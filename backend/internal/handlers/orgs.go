package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

// ---------------- Sharing an artifact with people ----------------

func (a *api) listShares(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	shares, err := a.deps.Store.ListShares(r.Context(), art.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"shares": mapSlice(shares, func(s storage.Share) map[string]any {
			return map[string]any{"email": s.Email, "role": s.Role, "created_at": s.CreatedAt}
		}),
	})
}

func (a *api) addShare(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// People added by email get access whatever the general access is, like
	// on a Google Doc: there is no visibility to flip.
	if err := a.deps.Store.AddShare(r.Context(), art.ID, identityFrom(r).User.ID, in.Email, in.Role); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a.record(r, art.ID, storage.EventShare, 0, map[string]any{"email": strings.ToLower(in.Email)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) removeShare(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	if err := a.deps.Store.RemoveShare(r.Context(), art.ID, chi.URLParam(r, "email")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------- Teams ----------------

func (a *api) listMyOrgs(w http.ResponseWriter, r *http.Request) {
	orgs, err := a.deps.Store.ListMyOrgs(r.Context(), identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": mapSlice(orgs, publicOrg)})
}

func publicOrg(o storage.Org) map[string]any {
	return map[string]any{
		"id": o.ID, "slug": o.Slug, "name": o.Name,
		"my_role": o.MyRole, "space_slug": o.SpaceSlug, "created_at": o.CreatedAt,
	}
}

func (a *api) createOrg(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a name is required")
		return
	}
	o, err := a.deps.Store.CreateOrg(r.Context(), in.Name, in.Slug, identityFrom(r).User.ID)
	if errors.Is(err, storage.ErrConflict) {
		writeErr(w, http.StatusConflict, "a team with this identifier already exists")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"org": publicOrg(*o)})
}

// withOrg resolves the team and the caller's role in it.
func (a *api) withOrg(minRole string, fn func(http.ResponseWriter, *http.Request, *storage.Org, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		oid, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid id")
			return
		}
		o, err := a.deps.Store.GetOrg(r.Context(), oid)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		role, err := a.deps.Store.OrgRole(r.Context(), oid, identityFrom(r).User.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if role == "" {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if minRole == "admin" && role != "owner" && role != "admin" {
			writeErr(w, http.StatusForbidden, "reserved to the team's admins")
			return
		}
		fn(w, r, o, role)
	}
}

func (a *api) listOrgMembers(w http.ResponseWriter, r *http.Request, o *storage.Org, _ string) {
	members, err := a.deps.Store.ListOrgMembers(r.Context(), o.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"members": mapSlice(members, func(m storage.Membership) map[string]any {
			return map[string]any{
				"user_id": m.UserID, "email": m.Email, "name": m.Name,
				"role": m.Role, "joined_at": m.JoinedAt, "pending": m.Pending,
			}
		}),
	})
}

func (a *api) inviteToOrg(w http.ResponseWriter, r *http.Request, o *storage.Org, _ string) {
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	outcome, err := a.deps.Store.InviteToOrg(r.Context(), o.ID, identityFrom(r).User.ID, in.Email, in.Role)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": outcome})
}

func (a *api) removeOrgMember(w http.ResponseWriter, r *http.Request, o *storage.Org, _ string) {
	uid, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	err = a.deps.Store.RemoveOrgMember(r.Context(), o.ID, uid)
	if errors.Is(err, storage.ErrForbidden) {
		writeErr(w, http.StatusConflict,
			"this is the last owner: make someone else owner before removing them")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listOrgArtifacts(w http.ResponseWriter, r *http.Request, o *storage.Org, _ string) {
	sp, err := a.deps.Store.GetSpaceByOrg(r.Context(), o.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	id := identityFrom(r)
	items, err := a.deps.Store.ListBySpace(r.Context(), sp.ID, id.User.ID, id.User.Email)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.writeArtifactList(w, r, items)
}
