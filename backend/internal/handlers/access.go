package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

// ---------------- Requests for edit access ----------------

func publicAccessRequest(q storage.AccessRequest) map[string]any {
	out := map[string]any{
		"id": q.ID, "artifact_id": q.ArtifactID,
		"user_id": q.UserID, "email": q.Email, "name": q.Name,
		"message": q.Message, "status": q.Status,
		"created_at": q.CreatedAt, "decided_at": q.DecidedAt,
	}
	if q.ArtifactTitle != "" {
		out["artifact_title"] = q.ArtifactTitle
		out["artifact_path"] = q.ArtifactPath
	}
	return out
}

// requestAccess is a viewer asking for editor access. Keys cannot ask: an
// agent asking for more power on its own is exactly what this must not be.
func (a *api) requestAccess(w http.ResponseWriter, r *http.Request, art *storage.Artifact, role storage.Role) {
	if role.CanDeploy() {
		writeErr(w, http.StatusConflict, "you already have edit access")
		return
	}
	var in struct {
		Message string `json:"message"`
	}
	_ = decode(r, &in)
	q, err := a.deps.Store.RequestAccess(r.Context(), art.ID, identityFrom(r).User.ID, in.Message)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"request": publicAccessRequest(*q)})
}

// listAccessRequests answers two different people: the owner sees what is
// pending on the artifact, anyone else sees only their own latest request.
func (a *api) listAccessRequests(w http.ResponseWriter, r *http.Request, art *storage.Artifact, role storage.Role) {
	if role == storage.RoleOwner {
		list, err := a.deps.Store.ListAccessRequests(r.Context(), art.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"requests": mapSlice(list, publicAccessRequest)})
		return
	}
	mine, err := a.deps.Store.MyAccessRequest(r.Context(), art.ID, identityFrom(r).User.ID)
	if errors.Is(err, storage.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"mine": nil})
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mine": publicAccessRequest(*mine)})
}

// accessInbox is every pending request on artifacts the caller owns.
func (a *api) accessInbox(w http.ResponseWriter, r *http.Request) {
	list, err := a.deps.Store.ListAccessInbox(r.Context(), identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": mapSlice(list, publicAccessRequest)})
}

func (a *api) decideAccessRequest(w http.ResponseWriter, r *http.Request) {
	rid, err := uuid.Parse(chi.URLParam(r, "rid"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		Approve bool `json:"approve"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	q, err := a.deps.Store.GetAccessRequest(r.Context(), rid)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// The decision belongs to whoever owns the artifact now, which may not be
	// who owned it when the request was made.
	if _, role, err := a.access(r, q.ArtifactID); err != nil || role != storage.RoleOwner {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	done, err := a.deps.Store.DecideAccessRequest(r.Context(), rid, identityFrom(r).User.ID, in.Approve)
	if errors.Is(err, storage.ErrConflict) {
		writeErr(w, http.StatusConflict, "this request was already answered")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if in.Approve {
		a.record(r, q.ArtifactID, storage.EventShare, 0, map[string]any{"email": q.Email, "role": "editor", "request": rid.String()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": publicAccessRequest(*done)})
}
