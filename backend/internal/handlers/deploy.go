package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/publish"
	"github.com/clackes/falconia/backend/internal/storage"
)

// Deploying a bundle takes three steps, a protocol designed for whoever
// publishes the same thing often with one line changed:
//
//	1. POST /api/blobs/missing  — "of these hashes, which don't you have?"
//	2. PUT  /api/blobs/<sha256> — upload only those
//	3. POST /api/deploy         — declare the version's manifest
//
// The first step is the one that matters. A fifty megabyte bundle where only
// index.html changed transfers a few kilobytes, because the instance already
// has everything else under the same hash. The third step is atomic: either a
// whole version is born, or nothing is.

var sha256Re = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (a *api) publisher() *publish.Publisher {
	return &publish.Publisher{
		Store: a.deps.Store, Blobs: a.deps.Blobs,
		MaxBytes: a.deps.MaxUploadBytes, MaxFiles: a.deps.MaxBundleFiles,
	}
}

// missingBlobs answers "which of these files do I need to upload?".
func (a *api) missingBlobs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SHAs []string `json:"shas"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(in.SHAs) > 5000 {
		writeErr(w, http.StatusBadRequest, "too many hashes in a single request")
		return
	}
	clean := make([]string, 0, len(in.SHAs))
	for _, s := range in.SHAs {
		s = strings.ToLower(strings.TrimSpace(s))
		if sha256Re.MatchString(s) {
			clean = append(clean, s)
		}
	}
	missing, err := a.deps.Store.MissingBlobs(r.Context(), clean)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"missing": missing})
}

// uploadBlob receives the bytes of one file.
//
// The hash in the URL is a claim, not a fact: it is recomputed while reading
// the bytes and, if it does not match, the request is rejected and nothing
// stays written. Without this check anyone could overwrite the content of a
// known hash and swap, under everyone's nose, a file that ten artifacts
// believe they share.
func (a *api) uploadBlob(w http.ResponseWriter, r *http.Request) {
	want := strings.ToLower(chi.URLParam(r, "sha"))
	if !sha256Re.MatchString(want) {
		writeErr(w, http.StatusBadRequest, "invalid sha256")
		return
	}
	if have, err := a.deps.Blobs.Has(r.Context(), want); err == nil && have {
		// Already there: the upload is idempotent and there is nothing to do.
		writeJSON(w, http.StatusOK, map[string]any{"sha256": want, "stored": false})
		return
	}

	// Bytes go through a temporary file rather than memory: a deploy with
	// twenty ten-megabyte files in parallel must not bring the process down.
	tmp, err := os.CreateTemp("", "falconia-upload-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	limited := io.LimitReader(r.Body, a.deps.MaxUploadBytes+1)
	size, err := io.Copy(io.MultiWriter(tmp, h), limited)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "upload interrupted")
		return
	}
	if size > a.deps.MaxUploadBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		writeErr(w, http.StatusBadRequest, "the declared sha256 does not match the bytes received")
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.deps.Blobs.Put(r.Context(), got, tmp, size); err != nil {
		a.deps.Logger.Error("blob put", "err", err)
		writeErr(w, http.StatusInternalServerError, "write failed")
		return
	}
	// Bytes first, then the row: a blob without a row is collectable garbage,
	// a row without bytes is a broken artifact.
	if err := a.deps.Store.RegisterBlob(r.Context(), got, size); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"sha256": got, "size": size, "stored": true})
}

// deployFile is one line of the manifest.
type deployFile struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

type deployReq struct {
	// Destination. When ArtifactID is empty the Space+Slug pair is used, and
	// if nothing exists there yet, it is created.
	ArtifactID string `json:"artifact_id"`
	Space      string `json:"space"`
	Slug       string `json:"slug"`

	Title       string `json:"title"`
	Description string `json:"description"`
	Message     string `json:"message"`
	Visibility  string `json:"visibility"`
	Category    string `json:"category"`
	Icon        string `json:"icon"`
	LogoURL     string `json:"logo_url"`

	EntryPath string       `json:"entry_path"`
	Files     []deployFile `json:"files"`
	// Code covers the single-document case, the Studio's.
	Code string `json:"code"`
}

// deploy publishes a new version, creating the artifact if it does not exist.
//
// One endpoint for both cases because that is what automation needs: an agent
// or a pipeline running on every change should not have to know whether it is
// the first time, nor remember an identifier between runs. Space and slug are
// enough, and they are stable.
func (a *api) deploy(w http.ResponseWriter, r *http.Request) {
	var in deployReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	files, entry, err := a.validateManifest(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	art, err := a.deployTarget(r, &in)
	if err != nil {
		writeStoreErr(w, err)
		return
	}

	id := identityFrom(r)

	// New artifact: created together with its first version.
	if art == nil {
		sp, err := a.resolveSpace(r, in.Space)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if !a.canWriteSpace(r, sp) {
			writeErr(w, http.StatusForbidden, "you are not a member of this space")
			return
		}
		title := firstNonBlank(in.Title, in.Slug, "Untitled artifact")
		created, v, err := a.deps.Store.CreateArtifact(r.Context(), id.User.ID, storage.CreateArtifactInput{
			SpaceID: sp.ID, Title: title, Description: in.Description,
			Code: a.entryText(r, in.Code, entry, files), Icon: in.Icon, LogoURL: in.LogoURL,
			Slug: firstNonBlank(in.Slug, title), Category: in.Category,
			Visibility: in.Visibility, EntryPath: entry, Files: files, Message: in.Message,
		})
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		a.record(r, created.ID, storage.EventDeploy, v.VersionNumber,
			map[string]any{"files": len(files), "created": true})
		created, _ = a.deps.Store.GetArtifact(r.Context(), created.ID)
		a.writeDeployResult(w, http.StatusCreated, created, v, id.User.DisplayName)
		return
	}

	// Existing artifact: publishing needs edit access.
	role, err := a.deps.Store.ResolveAccess(r.Context(), art.ID, id.User.ID, id.User.Email)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !role.CanDeploy() {
		writeErr(w, http.StatusForbidden, "you don't have permission to publish to this artifact")
		return
	}
	a.commitManifest(w, r, art, &in, files, entry)
}

// commitManifest publishes a validated manifest on an existing artifact.
func (a *api) commitManifest(w http.ResponseWriter, r *http.Request, art *storage.Artifact, in *deployReq, files []storage.FileSpec, entry string) {
	id := identityFrom(r)
	v, err := a.deps.Store.CommitVersion(r.Context(), art.ID, id.User.ID, storage.NewVersionInput(
		a.entryText(r, in.Code, entry, files), firstNonBlank(in.Message, "deploy"), entry, files))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// Optional metadata sent with a deploy updates the artifact only when set:
	// a pipeline that does not send it must not wipe the title.
	a.applyOptionalMeta(r, art, in)
	a.record(r, art.ID, storage.EventDeploy, v.VersionNumber, map[string]any{"files": len(files)})

	updated, _ := a.deps.Store.GetArtifact(r.Context(), art.ID)
	a.writeDeployResult(w, http.StatusCreated, updated, v, id.User.DisplayName)
}

// entryText returns the text mirror of the entry document. For a bundle it is
// read back from the blob store, so that any agent asking for the source later
// gets the real page and not an empty string.
func (a *api) entryText(r *http.Request, code, entry string, files []storage.FileSpec) string {
	if code != "" || len(files) == 0 {
		return code
	}
	for _, f := range files {
		if f.Path == entry {
			if text, err := a.publisher().ReadText(r.Context(), f.SHA256, 2<<20); err == nil {
				return text
			}
		}
	}
	return ""
}

// deployTarget finds the destination artifact, or nil if it must be created.
func (a *api) deployTarget(r *http.Request, in *deployReq) (*storage.Artifact, error) {
	if in.ArtifactID != "" {
		id, err := uuid.Parse(in.ArtifactID)
		if err != nil {
			return nil, storage.ErrNotFound
		}
		return a.deps.Store.GetArtifact(r.Context(), id)
	}
	if strings.TrimSpace(in.Slug) == "" {
		return nil, nil
	}
	sp, err := a.resolveSpace(r, in.Space)
	if err != nil {
		return nil, err
	}
	art, err := a.deps.Store.GetArtifactByPath(r.Context(), sp.Slug, in.Slug)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	return art, err
}

func (a *api) applyOptionalMeta(r *http.Request, art *storage.Artifact, in *deployReq) {
	m := storage.MetaOf(art)
	m.Title = firstNonBlank(in.Title, art.Title)
	m.Description = firstNonBlank(in.Description, art.Description)
	m.Visibility = firstNonBlank(in.Visibility, art.Visibility)
	m.Icon = firstNonBlank(in.Icon, art.Icon)
	m.LogoURL = firstNonBlank(in.LogoURL, art.LogoURL)
	m.Category = firstNonBlank(in.Category, art.Category)
	if err := a.deps.Store.UpdateMeta(r.Context(), art.ID, m); err != nil {
		a.deps.Logger.Warn("deploy meta", "err", err)
	}
}

func (a *api) writeDeployResult(w http.ResponseWriter, status int, art *storage.Artifact, v *storage.ArtifactVersion, authorName string) {
	out := map[string]any{
		"artifact": publicArtifact(art),
		"version":  publicVersionAs(v, false, authorName),
		"url":      a.deps.AppBaseURL + art.Path(),
	}
	if lp := art.LinkPath(); lp != "" {
		out["share_url"] = a.deps.AppBaseURL + lp
	}
	writeJSON(w, status, out)
}

// validateManifest cleans and checks the file list.
//
// Every path is normalized, and paths trying to climb out of the bundle root
// are rejected, not silently fixed: a manifest containing "../../etc/passwd" is
// not a typo, and treating it as one would mean accepting a cleaned-up version
// of something meant to do harm.
func (a *api) validateManifest(r *http.Request, in *deployReq) ([]storage.FileSpec, string, error) {
	entry := storage.NormalizePath(in.EntryPath)
	if len(in.Files) == 0 {
		if strings.TrimSpace(in.Code) == "" {
			return nil, "", errors.New("send either files or code")
		}
		if entry == "" {
			entry = "index.html"
		}
		return nil, entry, nil
	}
	if len(in.Files) > a.deps.MaxBundleFiles {
		return nil, "", errors.New("the bundle contains too many files")
	}

	out := make([]storage.FileSpec, 0, len(in.Files))
	seen := map[string]bool{}
	shas := make([]string, 0, len(in.Files))
	for _, f := range in.Files {
		clean := storage.NormalizePath(f.Path)
		if clean == "" {
			return nil, "", errors.New("invalid file path: " + f.Path)
		}
		if seen[clean] {
			return nil, "", errors.New("duplicate path in the bundle: " + clean)
		}
		seen[clean] = true
		sha := strings.ToLower(strings.TrimSpace(f.SHA256))
		if !sha256Re.MatchString(sha) {
			return nil, "", errors.New("invalid sha256 for " + clean)
		}
		shas = append(shas, sha)
		out = append(out, storage.FileSpec{
			Path: clean, SHA256: sha, Size: f.Size,
			ContentType: guessContentType(clean, f.ContentType),
		})
	}

	// No version can be born with a file that does not exist in the object
	// store: it would be an artifact that 404s on itself.
	missing, err := a.deps.Store.MissingBlobs(r.Context(), shas)
	if err != nil {
		return nil, "", errors.New("internal error")
	}
	if len(missing) > 0 {
		return nil, "", errors.New(strconv.Itoa(len(missing)) + " files are still missing: upload them before deploying")
	}

	if entry == "" || !seen[entry] {
		entry = publish.PickEntry(seen)
	}
	if entry == "" {
		return nil, "", errors.New("the bundle has no entry document (index.html or index.md)")
	}
	return out, entry, nil
}

func guessContentType(p, declared string) string {
	if t := mime.TypeByExtension(strings.ToLower(path.Ext(p))); t != "" {
		return t
	}
	if declared != "" {
		return declared
	}
	return "application/octet-stream"
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }

// commitVersion is the Studio's save: the edited entry document.
//
// On a multi-file bundle only the entry document is replaced and every other
// file carries over; before this, saving from the editor turned a bundle into
// a lone HTML page and silently dropped its assets.
func (a *api) commitVersion(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	var in struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Code) == "" {
		writeErr(w, http.StatusBadRequest, "the code is empty")
		return
	}
	var prev *storage.ArtifactVersion
	if v, err := a.deps.Store.GetVersion(r.Context(), art.ID, 0); err == nil {
		prev = v
	}
	res, err := a.publisher().Build(r.Context(), prev, "", in.Code, nil, nil)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	v, err := a.deps.Store.CommitVersion(r.Context(), art.ID, identityFrom(r).User.ID,
		storage.NewVersionInput(res.Code, firstNonBlank(in.Message, "edit"), res.EntryPath, res.Files))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, art.ID, storage.EventDeploy, v.VersionNumber, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"version": publicVersionAs(v, false, identityFrom(r).User.DisplayName)})
}

// deployInto publishes on an artifact already identified by the request path.
// It is the variant for callers who know the id, while /api/deploy serves
// those who think in terms of space and slug.
func (a *api) deployInto(w http.ResponseWriter, r *http.Request, art *storage.Artifact) {
	var in deployReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	files, entry, err := a.validateManifest(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a.commitManifest(w, r, art, &in, files, entry)
}
