package handlers

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/clackes/falconia/backend/internal/storage"
)

// download is GET /api/artifacts/{id}/download: one version as a zip.
//
// This is the anti-lock-in endpoint. Whatever agent made an artifact, whoever
// can edit it can take every file out, edit it locally with any other agent,
// and publish it back — `falconia pull` is built on this. The archive contains
// exactly the bundle, no wrapper folder and no metadata, so unzipping it into
// a directory and running `falconia deploy` round-trips cleanly.
//
// It requires edit access, like reading the source does elsewhere.
func (a *api) download(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	n, _ := strconv.Atoi(r.URL.Query().Get("v"))
	v, err := a.deps.Store.GetVersion(r.Context(), art.ID, n)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	files, err := a.deps.Store.ListFiles(r.Context(), v.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}

	name := "artifact"
	if art.Slug != nil && *art.Slug != "" {
		name = *art.Slug
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-v%d.zip"`, name, v.VersionNumber))
	w.Header().Set("X-Falconia-Entry", v.EntryPath)
	w.Header().Set("X-Falconia-Version", strconv.Itoa(v.VersionNumber))

	zw := zip.NewWriter(w)
	defer zw.Close()
	mod := v.CreatedAt
	if mod.IsZero() {
		mod = time.Now()
	}

	// A single-document version has no file rows: its document is the
	// version's text payload.
	if len(files) == 0 {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: v.EntryPath, Method: zip.Deflate, Modified: mod})
		if err == nil {
			_, _ = io.WriteString(f, v.CodePayload)
		}
		return
	}
	for _, file := range files {
		rc, err := a.deps.Blobs.Get(r.Context(), file.SHA256)
		if err != nil {
			a.deps.Logger.Error("export blob", "sha", file.SHA256, "err", err)
			continue
		}
		method := zip.Deflate
		if alreadyCompressed(file.Path) {
			method = zip.Store
		}
		f, err := zw.CreateHeader(&zip.FileHeader{Name: file.Path, Method: method, Modified: mod})
		if err == nil {
			_, _ = io.Copy(f, rc)
		}
		rc.Close()
	}
	a.record(r, art.ID, storage.EventExport, v.VersionNumber, nil)
}

func alreadyCompressed(p string) bool {
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".zip", ".gz",
		".mp4", ".webm", ".mp3", ".woff", ".woff2", ".pdf"} {
		if strings.HasSuffix(strings.ToLower(p), ext) {
			return true
		}
	}
	return false
}
