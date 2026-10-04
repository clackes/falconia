// Package publish turns "here is what changed" into a complete new version.
//
// Agents rarely want to resend a whole bundle. Claude Code fixing a typo in
// index.html, or Codex adding one chart script, should send that file and
// nothing else, and every other file of the previous version should carry
// over untouched. This package is where that happens, for every entry point
// alike: the MCP tools, the built-in chat and the Studio editor.
//
// It exists because the naive alternative silently destroys work: committing
// only the entry document of a multi-file bundle produces a version that has
// lost every stylesheet, script and image.
package publish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/clackes/falconia/backend/internal/blob"
	"github.com/clackes/falconia/backend/internal/storage"
)

// File is one file sent inline by an agent.
type File struct {
	Path string `json:"path"`
	// Content is the file body: UTF-8 text, or base64 when Encoding says so.
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"` // "" | "utf8" | "base64"
}

// Publisher writes inline files to the blob store and assembles versions.
type Publisher struct {
	Store    *storage.Postgres
	Blobs    blob.Store
	MaxBytes int64 // per file
	MaxFiles int   // per version
}

// Result is what a new version will be made of.
type Result struct {
	Files     []storage.FileSpec // empty for a single-document version
	EntryPath string
	// Code mirrors the entry document as text: it is what the Studio editor
	// opens and what get_artifact returns.
	Code string
}

// Put stores one file and returns its spec.
func (p *Publisher) Put(ctx context.Context, f File) (storage.FileSpec, []byte, error) {
	clean := storage.NormalizePath(f.Path)
	if clean == "" {
		return storage.FileSpec{}, nil, fmt.Errorf("invalid file path %q", f.Path)
	}
	var data []byte
	switch strings.ToLower(f.Encoding) {
	case "", "utf8", "utf-8", "text":
		data = []byte(f.Content)
	case "base64":
		b, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil {
			return storage.FileSpec{}, nil, fmt.Errorf("%s: invalid base64: %w", clean, err)
		}
		data = b
	default:
		return storage.FileSpec{}, nil, fmt.Errorf("%s: unknown encoding %q (use utf8 or base64)", clean, f.Encoding)
	}
	if p.MaxBytes > 0 && int64(len(data)) > p.MaxBytes {
		return storage.FileSpec{}, nil, fmt.Errorf("%s is larger than the %d MB limit", clean, p.MaxBytes>>20)
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if have, err := p.Blobs.Has(ctx, sha); err != nil || !have {
		if err := p.Blobs.Put(ctx, sha, bytes.NewReader(data), int64(len(data))); err != nil {
			return storage.FileSpec{}, nil, err
		}
	}
	// Bytes first, then the row: see storage.RegisterBlob.
	if err := p.Store.RegisterBlob(ctx, sha, int64(len(data))); err != nil {
		return storage.FileSpec{}, nil, err
	}
	return storage.FileSpec{Path: clean, SHA256: sha, Size: int64(len(data)), ContentType: ContentType(clean)}, data, nil
}

// Build assembles a version.
//
// prev is the version being replaced, or nil for a brand-new artifact. code,
// if not empty, replaces the entry document. files are added or replaced by
// path, and remove deletes paths; everything else carries over from prev.
//
// When the result has a single HTML or Markdown file and no other assets it
// stays a lightweight single-document version, the kind the Studio editor
// works on directly.
func (p *Publisher) Build(ctx context.Context, prev *storage.ArtifactVersion, entry, code string, files []File, remove []string) (*Result, error) {
	byPath := map[string]storage.FileSpec{}
	order := []string{}
	add := func(spec storage.FileSpec) {
		if _, ok := byPath[spec.Path]; !ok {
			order = append(order, spec.Path)
		}
		byPath[spec.Path] = spec
	}

	prevEntry := ""
	prevCode := ""
	if prev != nil {
		prevEntry = prev.EntryPath
		prevCode = prev.CodePayload
		prevFiles, err := p.Store.ListFiles(ctx, prev.ID)
		if err != nil {
			return nil, err
		}
		for _, f := range prevFiles {
			add(storage.FileSpec{Path: f.Path, SHA256: f.SHA256, Size: f.SizeBytes, ContentType: f.ContentType})
		}
	}

	entry = storage.NormalizePath(entry)
	if entry == "" {
		entry = prevEntry
	}

	// A previous single-document version has no file rows: materialize its
	// document so that adding a stylesheet to it does not lose the page.
	if prev != nil && len(order) == 0 && prevCode != "" && (len(files) > 0 || len(remove) > 0) {
		spec, _, err := p.Put(ctx, File{Path: firstNonEmpty(prevEntry, "index.html"), Content: prevCode})
		if err != nil {
			return nil, err
		}
		add(spec)
	}

	texts := map[string]string{}
	for _, f := range files {
		spec, data, err := p.Put(ctx, f)
		if err != nil {
			return nil, err
		}
		add(spec)
		if utf8.Valid(data) {
			texts[spec.Path] = string(data)
		}
	}

	if code != "" {
		target := firstNonEmpty(entry, "index.html")
		if len(order) > 0 {
			spec, _, err := p.Put(ctx, File{Path: target, Content: code})
			if err != nil {
				return nil, err
			}
			add(spec)
		}
		texts[target] = code
		entry = target
	}

	for _, r := range remove {
		clean := storage.NormalizePath(r)
		if _, ok := byPath[clean]; ok {
			delete(byPath, clean)
			delete(texts, clean)
		}
	}

	out := &Result{}
	specs := make([]storage.FileSpec, 0, len(byPath))
	paths := map[string]bool{}
	for _, pth := range order {
		if spec, ok := byPath[pth]; ok {
			specs = append(specs, spec)
			paths[pth] = true
		}
	}
	if p.MaxFiles > 0 && len(specs) > p.MaxFiles {
		return nil, fmt.Errorf("the bundle would contain %d files, more than the limit of %d", len(specs), p.MaxFiles)
	}

	// Single document: either code only, or exactly one document file.
	if len(specs) == 0 {
		if code == "" {
			return nil, errors.New("nothing to publish: send code or files")
		}
		out.EntryPath = firstNonEmpty(entry, "index.html")
		out.Code = code
		return out, nil
	}
	if len(specs) == 1 && isDocument(specs[0].Path) {
		if text, ok := texts[specs[0].Path]; ok {
			out.EntryPath = specs[0].Path
			out.Code = text
			return out, nil
		}
	}

	if entry == "" || !paths[entry] {
		entry = PickEntry(paths)
	}
	if entry == "" {
		return nil, errors.New("the bundle has no entry document (index.html or index.md)")
	}
	out.Files = specs
	out.EntryPath = entry
	if text, ok := texts[entry]; ok {
		out.Code = text
	} else if entry == prevEntry && code == "" {
		out.Code = prevCode
	}
	if out.Code == "" {
		if text, err := p.ReadText(ctx, byPath[entry].SHA256, 2<<20); err == nil {
			out.Code = text
		}
	}
	return out, nil
}

// ReadText returns a blob as text, or an error if it is binary or too large.
func (p *Publisher) ReadText(ctx context.Context, sha string, max int64) (string, error) {
	rc, err := p.Blobs.Get(ctx, sha)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > max {
		return "", fmt.Errorf("file larger than %d KB", max>>10)
	}
	if !utf8.Valid(data) {
		return "", errors.New("binary file")
	}
	return string(data), nil
}

// PickEntry chooses the document to open when the manifest does not say.
func PickEntry(paths map[string]bool) string {
	for _, candidate := range []string{"index.html", "index.htm", "index.md", "README.md", "readme.md"} {
		if paths[candidate] {
			return candidate
		}
	}
	// A bundle with a single document does not need to call it index.
	only := ""
	for p := range paths {
		if isDocument(p) {
			if only != "" {
				return ""
			}
			only = p
		}
	}
	return only
}

// ContentType guesses a MIME type from the extension.
func ContentType(p string) string {
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(p))); t != "" {
		return t
	}
	return "application/octet-stream"
}

func isDocument(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm", ".md", ".markdown":
		return true
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
