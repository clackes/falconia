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

	// Every blob is checked before the first byte goes out: once the zip has
	// started, the status code is already 200 and an error can no longer be
	// reported as one.
	for _, file := range files {
		if ok, err := a.deps.Blobs.Has(r.Context(), file.SHA256); err != nil || !ok {
			a.deps.Logger.Error("export: blob unavailable", "sha", file.SHA256, "err", err)
			writeErr(w, http.StatusInternalServerError, "a file of this version is unavailable")
			return
		}
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
	mod := v.CreatedAt
	if mod.IsZero() {
		mod = time.Now()
	}

	// A single-document version has no file rows: its document is the
	// version's text payload.
	if len(files) == 0 {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: v.EntryPath, Method: zip.Deflate, Modified: mod})
		if err == nil {
			_, err = io.WriteString(f, v.CodePayload)
		}
		if err == nil {
			err = zw.Close()
		}
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		a.record(r, art.ID, storage.EventExport, v.VersionNumber, nil)
		return
	}
	for _, file := range files {
		if err := addToZip(r, a, zw, file, mod); err != nil {
			// Skipping the file would hand `falconia pull` a well-formed zip
			// with pieces missing, which then gets deployed back as if it
			// were whole. Aborting the connection makes the client see a
			// broken download instead.
			a.deps.Logger.Error("export", "sha", file.SHA256, "path", file.Path, "err", err)
			panic(http.ErrAbortHandler)
		}
	}
	if err := zw.Close(); err != nil {
		panic(http.ErrAbortHandler)
	}
	a.record(r, art.ID, storage.EventExport, v.VersionNumber, nil)
}

func addToZip(r *http.Request, a *api, zw *zip.Writer, file storage.ArtifactFile, mod time.Time) error {
	rc, err := a.deps.Blobs.Get(r.Context(), file.SHA256)
	if err != nil {
		return err
	}
	defer rc.Close()
	method := zip.Deflate
	if alreadyCompressed(file.Path) {
		method = zip.Store
	}
	f, err := zw.CreateHeader(&zip.FileHeader{Name: file.Path, Method: method, Modified: mod})
	if err != nil {
		return err
	}
	_, err = io.Copy(f, rc)
	return err
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
