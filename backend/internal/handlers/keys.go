package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

// ---------------- API keys ----------------

func (a *api) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.deps.Store.ListAPIKeys(r.Context(), identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": mapSlice(keys, publicKey)})
}

// createKey issues a key and shows it exactly once.
//
// The endpoint is closed to keys themselves: if a key could issue another,
// revoking it would be pointless, because whoever stole it would already hold
// a second key nobody is watching.
func (a *api) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label     string   `json:"label"`
		Kind      string   `json:"kind"`
		Scopes    []string `json:"scopes"`
		ExpiresIn int      `json:"expires_in_days"`
	}
	_ = decode(r, &in)

	var ttl time.Duration
	if in.ExpiresIn > 0 {
		ttl = time.Duration(in.ExpiresIn) * 24 * time.Hour
	}
	k, token, err := a.deps.Store.CreateAPIKey(r.Context(), identityFrom(r).User.ID,
		in.Label, in.Kind, in.Scopes, ttl)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key": publicKey(*k),
		// Only chance: the database only holds the secret's bcrypt hash.
		"token": token,
		"note":  "Copy it now: it will not be shown again.",
	})
}

func (a *api) revokeKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.deps.Store.RevokeAPIKey(r.Context(), identityFrom(r).User.ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------- CLI login ----------------

// startDeviceLogin opens a login session. It needs no authentication: it is
// the starting point for whoever does not have any yet.
func (a *api) startDeviceLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label string `json:"label"`
	}
	_ = decode(r, &in)
	deviceCode, d, err := a.deps.Store.StartDeviceLogin(r.Context(), in.Label, 10*time.Minute)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"device_code":      deviceCode,
		"user_code":        d.UserCode,
		"verification_url": a.deps.AppBaseURL + "/cli-login",
		"expires_in":       int(time.Until(d.ExpiresAt).Seconds()),
		"interval":         2,
	})
}

// pollDeviceLogin is what the CLI polls while waiting for approval.
func (a *api) pollDeviceLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceCode string `json:"device_code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	token, u, err := a.deps.Store.PollDeviceLogin(r.Context(), strings.TrimSpace(in.DeviceCode))
	if errors.Is(err, storage.ErrNotFound) {
		writeJSON(w, http.StatusGone, map[string]any{"status": "expired"})
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if token == "" {
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "token": token, "user": publicUser(u),
		"api_base_url": a.deps.APIBaseURL,
	})
}

// describeDeviceLogin shows the person what they are about to authorize,
// before they do.
func (a *api) describeDeviceLogin(w http.ResponseWriter, r *http.Request) {
	d, err := a.deps.Store.FindDeviceByUserCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_code": d.UserCode, "label": d.Label,
		"approved": d.ApprovedBy != nil, "expires_at": d.ExpiresAt,
	})
}

// approveDeviceLogin is the moment a person, in their own browser, says yes.
func (a *api) approveDeviceLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserCode string `json:"user_code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	err := a.deps.Store.ApproveDeviceLogin(r.Context(), in.UserCode, identityFrom(r).User.ID)
	if errors.Is(err, storage.ErrConflict) {
		writeErr(w, http.StatusConflict, "this code has already been used")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
