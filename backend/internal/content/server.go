package content

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/clackes/falconia/backend/internal/blob"
	"github.com/clackes/falconia/backend/internal/storage"
)

type Server struct {
	Store      *storage.Postgres
	Blobs      blob.Store
	Secret     []byte
	AppOrigin  string // the only origin allowed to frame artifacts
	Logger     *slog.Logger
	MaxInlineB int64
}

// Handler builds the content server's router.
//
// It is deliberately tiny, and knows nothing about sessions or users: all it
// can do is check a signature and return bytes. The less surface this process
// has, the less it matters that the bytes it serves are hostile.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/__sfa/sdk.js", s.serveSDK)
	mux.HandleFunc("/c/", s.serveArtifact)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	return s.baseHeaders(mux)
}

// baseHeaders puts on every response the headers that make serving untrusted
// content safe.
func (s *Server) baseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The application sends COEP: without this the browser would refuse
		// to frame or load anything coming from here.
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Cross-Origin-Embedder-Policy", "credentialless")
		// The artifact runs in an opaque origin: every fetch of its own files
		// is technically cross-origin and would fail without this. It opens
		// nothing, because without a valid grant in the path there is nothing
		// to read.
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Vary", "Origin")
		next.ServeHTTP(w, r)
	})
}

// serveArtifact is the /c/<grant>/<path...> route.
func (s *Server) serveArtifact(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/c/")
	grantTok, filePath, _ := strings.Cut(rest, "/")
	if grantTok == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	g, err := Verify(s.Secret, grantTok)
	if err != nil {
		// 403, not 404: the link is well-formed, it just expired, and whoever
		// opens it should understand that reloading the page is enough.
		http.Error(w, "link expired: reload the artifact page", http.StatusForbidden)
		return
	}

	// A request for /c/<grant> without a trailing slash would break every
	// relative URL inside the artifact, which would resolve one level too high.
	if filePath == "" && !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
		return
	}

	ctx := r.Context()
	version, err := s.Store.GetVersion(ctx, g.ArtifactID, g.Version)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	isEntry := filePath == "" || filePath == version.EntryPath
	if filePath == "" {
		filePath = version.EntryPath
	}

	// frame-ancestors is only narrowed on the document: assets are not framed
	// and the header would be noise.
	if isEntry {
		w.Header().Set("Content-Security-Policy", "frame-ancestors "+s.AppOrigin)
		s.recordView(ctx, g, r)
	}

	// Versions created in the Studio have no files: their single document
	// lives in the version's text payload.
	if version.Kind == "doc" || version.FileCount == 0 {
		if !isEntry {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		s.writeDocument(w, r, version.EntryPath, []byte(version.CodePayload))
		return
	}

	file, err := s.Store.GetFile(ctx, version.ID, filePath)
	if errors.Is(err, storage.ErrNotFound) {
		// A single-page app uses paths that do not exist on disk: if the
		// bundle has an index.html and the request has no extension, serve it
		// and let the artifact's router decide.
		if path.Ext(filePath) == "" {
			if entry, err2 := s.Store.GetFile(ctx, version.ID, version.EntryPath); err2 == nil {
				file = entry
			}
		}
		if file == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	} else if err != nil {
		s.Logger.Error("content lookup", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	rc, err := s.Blobs.Get(ctx, file.SHA256)
	if err != nil {
		s.Logger.Error("blob read", "err", err, "sha", file.SHA256)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	// Blobs are content-addressed: the same URL can never change bytes, so the
	// cache can be aggressive and immutable.
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if match := r.Header.Get("If-None-Match"); strings.Contains(match, file.SHA256) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if isDocument(file.Path) {
		body, err := io.ReadAll(io.LimitReader(rc, s.maxInline()))
		if err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		s.writeDocument(w, r, file.Path, body)
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(file.Path, file.ContentType))
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		s.Logger.Debug("content copy interrupted", "err", err)
	}
}

// writeDocument serves an entry document, injecting the shim into it.
func (s *Server) writeDocument(w http.ResponseWriter, r *http.Request, name string, body []byte) {
	if strings.EqualFold(path.Ext(name), ".md") || strings.EqualFold(path.Ext(name), ".markdown") {
		body = renderMarkdown(body)
	}
	out := injectSDK(body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(out)
}

// injectSDK inserts the bridge script into the document.
//
// It must come first, before any of the artifact's scripts: the shim replaces
// localStorage with an asynchronous proxy, and a write made before the proxy
// exists would go nowhere.
func injectSDK(body []byte) []byte {
	const tag = `<script src="/__sfa/sdk.js"></script>`
	s := string(body)
	lower := strings.ToLower(s)
	if i := strings.Index(lower, "<head>"); i >= 0 {
		return []byte(s[:i+len("<head>")] + tag + s[i+len("<head>"):])
	}
	if i := strings.Index(lower, "<html"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j >= 0 {
			at := i + j + 1
			return []byte(s[:at] + tag + s[at:])
		}
	}
	return []byte(tag + s)
}

var markdownRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

// renderMarkdown turns a .md file into a page.
//
// Raw HTML inside Markdown stays disabled (goldmark's default): a text
// document published by an agent has no reason to carry scripts, and allowing
// it would turn the most harmless format in the system into the handiest one
// to smuggle something in.
func renderMarkdown(src []byte) []byte {
	var out strings.Builder
	if err := markdownRenderer.Convert(src, &out); err != nil {
		return []byte("<pre>" + htmlEscape(string(src)) + "</pre>")
	}
	return []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root { color-scheme: light dark; --fg:#111827; --bg:#ffffff; --muted:#6b7280; --line:#e5e7eb; --code:#f6f7f9; }
  @media (prefers-color-scheme: dark) {
    :root { --fg:#e5e7eb; --bg:#0b0d12; --muted:#9ca3af; --line:#242a36; --code:#151a23; }
  }
  body { margin:0; padding:48px 16px; background:var(--bg); color:var(--fg);
         font:16px/1.7 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif; }
  main { max-width:46rem; margin:0 auto; }
  h1,h2,h3 { line-height:1.25; margin:2em 0 .6em; }
  h1 { font-size:2rem; } h2 { font-size:1.5rem; } h3 { font-size:1.2rem; }
  a { color:inherit; text-underline-offset:3px; }
  pre { background:var(--code); padding:16px; border-radius:10px; overflow:auto; }
  code { background:var(--code); padding:.15em .35em; border-radius:4px; font-size:.9em; }
  pre code { background:none; padding:0; }
  blockquote { margin:1.5em 0; padding-left:1em; border-left:3px solid var(--line); color:var(--muted); }
  table { border-collapse:collapse; width:100%; margin:1.5em 0; }
  th,td { border:1px solid var(--line); padding:8px 12px; text-align:left; }
  img { max-width:100%; height:auto; }
  hr { border:0; border-top:1px solid var(--line); margin:2.5em 0; }
</style></head><body><main>` + out.String() + `</main></body></html>`)
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func isDocument(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm", ".md", ".markdown":
		return true
	}
	return false
}

// contentTypeFor always prefers a type inferred from the extension over the
// one declared by the uploader.
//
// The declared type comes from the CLI, that is ultimately from the agent, and
// letting it choose would allow it to have a file the rest of the system
// treats as an image interpreted as HTML.
func contentTypeFor(p, declared string) string {
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(p))); t != "" {
		return t
	}
	if declared != "" && !strings.Contains(strings.ToLower(declared), "html") {
		return declared
	}
	return "application/octet-stream"
}

func (s *Server) maxInline() int64 {
	if s.MaxInlineB > 0 {
		return s.MaxInlineB
	}
	return 16 << 20
}

// recordView logs the opening in the audit trail without making the reader
// wait. Anonymous link visitors are recorded too, as "link" views.
func (s *Server) recordView(ctx context.Context, g *Grant, r *http.Request) {
	via := "web"
	if g.UserID == uuid.Nil {
		via = "link"
	}
	in := storage.RecordEventInput{
		ArtifactID: g.ArtifactID, ActorID: g.UserID, Kind: storage.EventView,
		VersionNumber: g.Version, Via: via, VisitorHash: s.visitorHash(g.UserID, r),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.Store.RecordEvent(ctx, in); err != nil {
			s.Logger.Debug("record view", "err", err)
		}
	}()
}

// visitorHash identifies a visitor without storing who they are.
//
// For a signed-in person it is their user id. For anyone else, a truncated
// HMAC of the IP address: enough not to count the same person ten times, not
// enough to trace them, because without the instance's key the hash cannot be
// reversed and with it one would only get an IP anyway.
func (s *Server) visitorHash(userID uuid.UUID, r *http.Request) string {
	if userID != uuid.Nil {
		return "u:" + userID.String()
	}
	ip := r.Header.Get("X-Forwarded-For")
	if i := strings.IndexByte(ip, ','); i >= 0 {
		ip = ip[:i]
	}
	if ip == "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		}
	}
	h := hmac.New(sha256.New, s.Secret)
	fmt.Fprintf(h, "visitor:%s", strings.TrimSpace(ip))
	return "a:" + hex.EncodeToString(h.Sum(nil))[:16]
}

//go:embed assets/sdk.js
var sdkJS []byte

// serveSDK delivers the shim. It lives on the content origin rather than the
// application's because an opaque-origin iframe cannot load anything from the
// app origin without CORS, and because there is no reason to have an artifact
// touch a file served from where the session lives.
//
// The /__sfa/ path predates the rename and stays: artifacts that hard-coded it
// keep working.
func (s *Server) serveSDK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(sdkJS)
}
