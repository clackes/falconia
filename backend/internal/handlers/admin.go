package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// The instance panel: who uses it, how much space it takes, what happened.
// Reserved to whoever set up the server.

func (a *api) adminStats(w http.ResponseWriter, r *http.Request) {
	s, err := a.deps.Store.Stats(r.Context())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users": s.Users, "orgs": s.Orgs, "artifacts": s.Artifacts,
		"versions": s.Versions, "files": s.Files, "blobs": s.Blobs,
		"bytes": s.Bytes, "open_threads": s.OpenThread,
		"blob_backend": a.deps.BlobBackend,
	})
}

func (a *api) adminUsers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	users, err := a.deps.Store.ListUsers(r.Context(), limit)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]userJSON, 0, len(users))
	for i := range users {
		out = append(out, publicUser(&users[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (a *api) adminSetRole(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Demoting yourself would leave the instance without admins when you are
	// the only one: better to prevent it than to explain later how to repair
	// it from the database.
	if uid == identityFrom(r).User.ID && !in.IsAdmin {
		writeErr(w, http.StatusConflict, "make someone else an admin before removing your own role")
		return
	}
	if err := a.deps.Store.SetAdmin(r.Context(), uid, in.IsAdmin); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) adminEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	evs, err := a.deps.Store.ListInstanceEvents(r.Context(), limit)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": mapSlice(evs, publicEvent)})
}

// adminGC frees the space of blobs no version references any more.
//
// It never runs by itself: deleting bytes is irreversible, and the decision
// stays with a person who looks at the numbers before pressing the button.
func (a *api) adminGC(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("older_than_hours"))
	orphans, err := a.deps.Store.OrphanBlobs(r.Context(), hours, 1000)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if r.URL.Query().Get("dry_run") != "false" {
		writeJSON(w, http.StatusOK, map[string]any{"orphans": len(orphans), "deleted": 0, "dry_run": true})
		return
	}
	deleted := 0
	for _, sha := range orphans {
		if err := a.deps.Blobs.Delete(r.Context(), sha); err != nil {
			a.deps.Logger.Warn("gc blob", "sha", sha, "err", err)
			continue
		}
		if err := a.deps.Store.ForgetBlob(r.Context(), sha); err != nil {
			a.deps.Logger.Warn("gc row", "sha", sha, "err", err)
			continue
		}
		deleted++
	}
	writeJSON(w, http.StatusOK, map[string]any{"orphans": len(orphans), "deleted": deleted, "dry_run": false})
}
