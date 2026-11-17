package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

// Proving an email address.
//
// Access is granted by email (shares, team invitations), so an address must be
// proven before it unlocks anything, or anyone could sign up as someone else
// and inherit what was meant for them. Three ways to prove it: clicking a link
// sent to it, signing up through an invitation link sent to it, or an
// administrator vouching for it on an instance without a mail server.

const verificationTTL = 48 * time.Hour

// resendLimiter bounds verification emails per account: three an hour is
// plenty for a person, and keeps the instance from being used to flood an
// inbox with mail nobody asked for.
var resendLimiter = newFailureLimiter(3, time.Hour)

// sendVerification emails a fresh verification link, off the request path:
// a slow mail server must not make sign-up hang, and the result does not
// change what the response says.
func (a *api) sendVerification(u *storage.User) {
	if a.deps.Mailer == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		token, err := a.deps.Store.CreateEmailVerification(ctx, u, verificationTTL)
		if err != nil {
			a.deps.Logger.Error("verification token", "err", err)
			return
		}
		link := a.deps.AppBaseURL + "/verify?token=" + url.QueryEscape(token)
		body := "Hello " + u.DisplayName + ",\n\n" +
			"confirm that this address is yours to finish setting up your Falconia account:\n\n" +
			link + "\n\n" +
			"The link works once and expires in 48 hours. If you did not sign up, ignore this email:\n" +
			"nothing will happen.\n"
		if err := a.deps.Mailer.Send(ctx, u.Email, "Confirm your email for Falconia", body); err != nil {
			a.deps.Logger.Error("send verification", "err", err)
		}
	}()
}

// sendMail sends a notification without making the request wait for it.
func (a *api) sendMail(to, subject, body string) {
	if a.deps.Mailer == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.deps.Mailer.Send(ctx, to, subject, body); err != nil {
			a.deps.Logger.Error("send mail", "err", err)
		}
	}()
}

// verifyEmail is POST /api/auth/verify {token}. It needs no session: the
// link may be opened on a phone where the person is not signed in, and the
// token is the proof.
func (a *api) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u, err := a.deps.Store.ConsumeEmailVerification(r.Context(), in.Token)
	if errors.Is(err, storage.ErrNotFound) {
		writeErr(w, http.StatusBadRequest, "this link is invalid, already used or expired: request a new one")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// The address is proven: whatever was waiting for it applies now.
	if err := a.deps.Store.AcceptPendingInvites(r.Context(), u); err != nil {
		a.deps.Logger.Warn("accept invites", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": u.Email})
}

// resendVerification is POST /api/auth/verify/resend.
func (a *api) resendVerification(w http.ResponseWriter, r *http.Request) {
	u := identityFrom(r).User
	if u.EmailVerifiedAt != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "already_verified": true})
		return
	}
	if a.deps.Mailer == nil {
		writeErr(w, http.StatusConflict,
			"this instance cannot send email: ask an administrator to verify your address")
		return
	}
	key := u.ID.String()
	if !resendLimiter.allow(key) {
		writeErr(w, http.StatusTooManyRequests, "too many emails sent: try again in an hour")
		return
	}
	resendLimiter.fail(key)
	a.sendVerification(u)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// describeInvitation is GET /api/auth/invite/{token}: what the sign-up page
// shows before someone accepts.
func (a *api) describeInvitation(w http.ResponseWriter, r *http.Request) {
	inv, err := a.deps.Store.LookupInvitation(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": inv.Email, "team": inv.OrgName, "role": inv.Role})
}

// logoutEverywhere is POST /api/auth/logout-all: every session issued so far,
// this one included, stops working. It is the answer to "I signed in on a
// computer that is not mine" and to a stolen token.
func (a *api) logoutEverywhere(w http.ResponseWriter, r *http.Request) {
	if err := a.deps.Store.RevokeSessions(r.Context(), identityFrom(r).User.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminVerifyUser is POST /api/admin/users/{id}/verify.
func (a *api) adminVerifyUser(w http.ResponseWriter, r *http.Request) {
	uid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.deps.Store.MarkEmailVerified(r.Context(), uid); err != nil {
		writeStoreErr(w, err)
		return
	}
	if u, err := a.deps.Store.GetUser(r.Context(), uid); err == nil {
		_ = a.deps.Store.AcceptPendingInvites(r.Context(), u)
	}
	w.WriteHeader(http.StatusNoContent)
}
