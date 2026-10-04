package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/clackes/falconia/backend/internal/agents"
	"github.com/clackes/falconia/backend/internal/content"
	"github.com/clackes/falconia/backend/internal/storage"
	"github.com/clackes/falconia/backend/pkg/auth"
)

// Version is the Falconia release, overridden at build time with
// -ldflags "-X github.com/clackes/falconia/backend/internal/handlers.Version=v1.2.3".
var Version = "dev"

type api struct{ deps Deps }

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version, "ts": time.Now().UTC()})
}

// config tells the interface what it needs to know before sign-in.
func (a *api) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":               "Falconia",
		"version":            Version,
		"app_base_url":       a.deps.AppBaseURL,
		"content_base_url":   a.deps.ContentBaseURL,
		"api_base_url":       a.deps.APIBaseURL,
		"mcp_url":            a.deps.APIBaseURL + "/mcp",
		"allow_registration": a.deps.AllowRegistration || a.noUsersYet(r),
		"max_upload_bytes":   a.deps.MaxUploadBytes,
		"max_bundle_files":   a.deps.MaxBundleFiles,
	})
}

// noUsersYet keeps sign-up open while the instance is empty, even when the
// configuration forbids it: otherwise a fresh install with
// ALLOW_REGISTRATION=false would have no way to get a first administrator.
func (a *api) noUsersYet(r *http.Request) bool {
	users, err := a.deps.Store.ListUsers(r.Context(), 1)
	return err == nil && len(users) == 0
}

// ---------------- Authentication ----------------

func (a *api) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !a.deps.AllowRegistration && !a.noUsersYet(r) {
		writeErr(w, http.StatusForbidden, "sign-up is closed on this instance: ask for an invitation")
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.Email == "" || !strings.Contains(in.Email, "@") {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	if len(in.Password) < 10 {
		writeErr(w, http.StatusBadRequest, "the password must be at least 10 characters long")
		return
	}
	if in.DisplayName == "" {
		in.DisplayName = strings.SplitN(in.Email, "@", 2)[0]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := a.deps.Store.CreateUser(r.Context(), in.Email, in.DisplayName, string(hash))
	if errors.Is(err, storage.ErrConflict) {
		writeErr(w, http.StatusConflict, "an account with this email already exists")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	_ = a.deps.Store.AcceptPendingInvites(r.Context(), u.ID, u.Email)
	a.issueSession(w, u, http.StatusCreated)
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u, err := a.deps.Store.GetUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(in.Email)))
	if err != nil || u.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		// One message for both "unknown email" and "wrong password": telling
		// them apart would let anyone find out who has an account.
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	_ = a.deps.Store.AcceptPendingInvites(r.Context(), u.ID, u.Email)
	a.issueSession(w, u, http.StatusOK)
}

func (a *api) issueSession(w http.ResponseWriter, u *storage.User, status int) {
	tok, err := auth.Issue(a.deps.JWTSecret, u.ID, 30*24*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, status, map[string]any{"token": tok, "user": publicUser(u)})
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r)
	spaces, err := a.deps.Store.ListMySpaces(r.Context(), id.User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":   publicUser(id.User),
		"spaces": mapSlice(spaces, publicSpace),
	})
}

// ---------------- Spaces ----------------

func (a *api) listSpaces(w http.ResponseWriter, r *http.Request) {
	spaces, err := a.deps.Store.ListMySpaces(r.Context(), identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": mapSlice(spaces, publicSpace)})
}

// resolveSpace accepts either a UUID or a slug, because the CLI passes slugs
// ("--space marketing") and the interface passes ids.
func (a *api) resolveSpace(r *http.Request, ref string) (*storage.Space, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return a.deps.Store.GetSpaceByOwner(r.Context(), identityFrom(r).User.ID)
	}
	if id, err := uuid.Parse(ref); err == nil {
		return a.deps.Store.GetSpace(r.Context(), id)
	}
	return a.deps.Store.GetSpaceBySlug(r.Context(), ref)
}

// canWriteSpace checks that the caller may publish into a space.
func (a *api) canWriteSpace(r *http.Request, sp *storage.Space) bool {
	role, err := a.deps.Store.SpaceRole(r.Context(), sp, identityFrom(r).User.ID)
	return err == nil && role != ""
}

func (a *api) listSpaceArtifacts(w http.ResponseWriter, r *http.Request) {
	sp, err := a.resolveSpace(r, chi.URLParam(r, "space"))
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

// ---------------- Artifacts ----------------

// access resolves artifact and role in one go.
func (a *api) access(r *http.Request, artifactID uuid.UUID) (*storage.Artifact, storage.Role, error) {
	id := identityFrom(r)
	role, err := a.deps.Store.ResolveAccess(r.Context(), artifactID, userIDOf(id), emailOf(id))
	if err != nil {
		return nil, storage.RoleNone, err
	}
	art, err := a.deps.Store.GetArtifact(r.Context(), artifactID)
	if err != nil {
		return nil, storage.RoleNone, err
	}
	return art, role, nil
}

// artifactParam resolves the {id} parameter, which may be a UUID or
// "<space>:<slug>".
func (a *api) artifactParam(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "id")
	if id, err := uuid.Parse(raw); err == nil {
		return id, nil
	}
	space, slug, ok := strings.Cut(raw, ":")
	if !ok {
		return uuid.Nil, storage.ErrNotFound
	}
	art, err := a.deps.Store.GetArtifactByPath(r.Context(), space, slug)
	if err != nil {
		return uuid.Nil, err
	}
	return art.ID, nil
}

// withArtifact is the preamble shared by almost every endpoint: resolve the
// artifact, check the minimum permission, hand over.
func (a *api) withArtifact(min storage.Role, fn func(http.ResponseWriter, *http.Request, *storage.Artifact, storage.Role)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		artID, err := a.artifactParam(r)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		art, role, err := a.access(r, artID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		switch min {
		case storage.RoleOwner:
			if role != storage.RoleOwner {
				writeErr(w, http.StatusForbidden, "only the artifact's owner can do this")
				return
			}
		case storage.RoleEditor:
			if !role.CanDeploy() {
				writeErr(w, http.StatusForbidden, "you need edit access to publish")
				return
			}
		}
		fn(w, r, art, role)
	}
}

// listFeed is the dashboard: everything I can open, most recent first.
func (a *api) listFeed(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r)
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	opt := storage.ListFeedOptions{
		Query:    strings.TrimSpace(q.Get("q")),
		Category: strings.TrimSpace(q.Get("category")),
		Agent:    agents.Normalize(q.Get("agent")),
		Limit:    limit,
	}
	if ref := q.Get("space"); ref != "" {
		sp, err := a.resolveSpace(r, ref)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		opt.SpaceID = &sp.ID
	}
	items, err := a.deps.Store.ListVisibleTo(r.Context(), id.User.ID, id.User.Email, opt)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.writeArtifactList(w, r, items)
}

func (a *api) myArtifacts(w http.ResponseWriter, r *http.Request) {
	items, err := a.deps.Store.ListByAuthor(r.Context(), identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.writeArtifactList(w, r, items)
}

func (a *api) sharedWithMe(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r)
	items, err := a.deps.Store.ListSharedWithMe(r.Context(), id.User.ID, id.User.Email)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.writeArtifactList(w, r, items)
}

// writeArtifactList enriches a list with likes, in two queries instead of two
// per row.
func (a *api) writeArtifactList(w http.ResponseWriter, r *http.Request, items []storage.Artifact) {
	ids := make([]uuid.UUID, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	likes, _ := a.deps.Store.LikeCountsBatch(r.Context(), ids)
	mine, _ := a.deps.Store.MyLikedSet(r.Context(), identityFrom(r).User.ID, ids)

	out := make([]artifactJSON, 0, len(items))
	for i := range items {
		j := publicArtifact(&items[i])
		j.Likes = likes[items[i].ID]
		j.Liked = mine[items[i].ID]
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": out})
}

func (a *api) getArtifact(w http.ResponseWriter, r *http.Request, art *storage.Artifact, role storage.Role) {
	n, _ := strconv.Atoi(r.URL.Query().Get("v"))
	v, err := a.deps.Store.GetVersion(r.Context(), art.ID, n)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		writeStoreErr(w, err)
		return
	}
	j := publicArtifact(art)
	j.MyRole = string(role)
	j.Likes, _ = a.deps.Store.CountLikes(r.Context(), art.ID)
	j.Liked, _ = a.deps.Store.HasLiked(r.Context(), art.ID, identityFrom(r).User.ID)

	resp := map[string]any{"artifact": j}
	if v != nil {
		// Source goes only to people who can change it: viewers watch the
		// artifact run, they do not read its code.
		resp["version"] = publicVersion(v, role.CanDeploy())
		if files, err := a.deps.Store.ListFiles(r.Context(), v.ID); err == nil && len(files) > 0 {
			resp["files"] = mapSlice(files, func(f storage.ArtifactFile) map[string]any {
				return map[string]any{"path": f.Path, "content_type": f.ContentType, "size": f.SizeBytes}
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolvePath resolves /s/<space>/<slug> to an artifact.
func (a *api) resolvePath(w http.ResponseWriter, r *http.Request) {
	art, err := a.deps.Store.GetArtifactByPath(r.Context(), chi.URLParam(r, "space"), chi.URLParam(r, "slug"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	_, role, err := a.access(r, art.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.getArtifact(w, r, art, role)
}

func (a *api) listVersions(w http.ResponseWriter, r *http.Request, art *storage.Artifact, role storage.Role) {
	vs, err := a.deps.Store.ListVersions(r.Context(), art.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"versions": mapSlice(vs, func(v storage.ArtifactVersion) versionJSON { return publicVersion(&v, false) }),
	})
}

// contentURL mints the signed link the iframe loads the artifact from.
//
// Access is decided here, not in the content server: the content server checks
// a signature and knows nothing about roles. Keeping the two apart means the
// process that touches hostile bytes does not need to know who the users are.
func (a *api) contentURL(w http.ResponseWriter, r *http.Request, art *storage.Artifact, role storage.Role) {
	n, _ := strconv.Atoi(r.URL.Query().Get("v"))
	out, err := a.signContent(r, art, n, identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) signContent(r *http.Request, art *storage.Artifact, n int, userID uuid.UUID) (map[string]any, error) {
	v, err := a.deps.Store.GetVersion(r.Context(), art.ID, n)
	if err != nil {
		return nil, err
	}
	grant, err := content.Sign(a.deps.ContentSecret, content.Grant{
		ArtifactID: art.ID,
		Version:    v.VersionNumber,
		UserID:     userID,
		ExpiresAt:  time.Now().Add(a.deps.ContentGrantTTL).Unix(),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"url":        a.deps.ContentBaseURL + "/c/" + grant + "/",
		"version":    v.VersionNumber,
		"entry_path": v.EntryPath,
		"expires_in": int(a.deps.ContentGrantTTL.Seconds()),
	}, nil
}

// ---------------- Links ----------------

// openLink is GET /api/p/{token}: the one artifact endpoint that answers
// without a session.
//
// It is how "anyone with the link" works. An anonymous visitor receives a
// minimal description and a signed content URL, so the artifact renders
// without an account. A signed-in visitor is also remembered, which puts the
// artifact in their dashboard and lets them comment through the normal viewer.
func (a *api) openLink(w http.ResponseWriter, r *http.Request) {
	art, err := a.deps.Store.GetArtifactByLinkToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("v"))
	id := a.maybeIdentity(r)
	uid := userIDOf(id)

	resp := map[string]any{"artifact": publicLinkArtifact(art), "signed_in": id != nil}
	if id != nil {
		// A key counts too: an agent handed the link should be able to read
		// the artifact and comment on it, like its owner clicking the link.
		if err := a.deps.Store.RecordLinkVisit(r.Context(), art.ID, uid); err != nil {
			a.deps.Logger.Warn("link visit", "err", err)
		}
		if role, err := a.deps.Store.ResolveAccess(r.Context(), art.ID, uid, emailOf(id)); err == nil {
			resp["my_role"] = string(role)
		}
	}
	c, err := a.signContent(r, art, n, uid)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	resp["content"] = c
	writeJSON(w, http.StatusOK, resp)
}

// rotateLink issues a new secret link and invalidates the old one.
func (a *api) rotateLink(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	if _, err := a.deps.Store.RotateLinkToken(r.Context(), art.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, art.ID, storage.EventShare, 0, map[string]any{"link": "rotated"})
	updated, _ := a.deps.Store.GetArtifact(r.Context(), art.ID)
	writeJSON(w, http.StatusOK, map[string]any{"artifact": publicArtifact(updated)})
}

// ---------------- Create and edit ----------------

type createArtifactReq struct {
	Space       string `json:"space"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Code        string `json:"code"`
	Slug        string `json:"slug"`
	Icon        string `json:"icon"`
	LogoURL     string `json:"logo_url"`
	ThemeColor  string `json:"theme_color"`
	Category    string `json:"category"`
	Visibility  string `json:"visibility"`
	Forkable    *bool  `json:"forkable"`
}

func (a *api) createArtifact(w http.ResponseWriter, r *http.Request) {
	var in createArtifactReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeErr(w, http.StatusBadRequest, "a title is required")
		return
	}
	sp, err := a.resolveSpace(r, in.Space)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !a.canWriteSpace(r, sp) {
		writeErr(w, http.StatusForbidden, "you are not a member of this space")
		return
	}
	id := identityFrom(r)
	art, v, err := a.deps.Store.CreateArtifact(r.Context(), id.User.ID, storage.CreateArtifactInput{
		SpaceID: sp.ID, Title: in.Title, Description: in.Description, Code: in.Code,
		Icon: in.Icon, LogoURL: in.LogoURL, ThemeColor: in.ThemeColor,
		Slug: in.Slug, Category: in.Category, Visibility: in.Visibility, Forkable: in.Forkable,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, art.ID, storage.EventCreate, v.VersionNumber, nil)
	art, _ = a.deps.Store.GetArtifact(r.Context(), art.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"artifact": publicArtifact(art), "version": publicVersionAs(v, false, identityFrom(r).User.DisplayName),
	})
}

func (a *api) updateMeta(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	in := storage.MetaOf(art)
	// Decode on top of the current values, so a partial PATCH does not wipe
	// the fields it does not mention.
	var patch struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		Visibility  *string `json:"visibility"`
		TeamRole    *string `json:"team_role"`
		Icon        *string `json:"icon"`
		ThemeColor  *string `json:"theme_color"`
		LogoURL     *string `json:"logo_url"`
		Slug        *string `json:"slug"`
		Forkable    *bool   `json:"forkable"`
		Category    *string `json:"category"`
	}
	if err := decode(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	assign(&in.Title, patch.Title)
	assign(&in.Description, patch.Description)
	assign(&in.Visibility, patch.Visibility)
	assign(&in.TeamRole, patch.TeamRole)
	assign(&in.Icon, patch.Icon)
	assign(&in.ThemeColor, patch.ThemeColor)
	assign(&in.LogoURL, patch.LogoURL)
	assign(&in.Slug, patch.Slug)
	assign(&in.Category, patch.Category)
	assign(&in.Forkable, patch.Forkable)
	if err := a.deps.Store.UpdateMeta(r.Context(), art.ID, in); err != nil {
		writeStoreErr(w, err)
		return
	}
	if patch.Visibility != nil && storage.ValidVisibility(*patch.Visibility) != art.Visibility {
		a.record(r, art.ID, storage.EventShare, 0,
			map[string]any{"general_access": storage.ValidVisibility(*patch.Visibility)})
	}
	updated, _ := a.deps.Store.GetArtifact(r.Context(), art.ID)
	writeJSON(w, http.StatusOK, map[string]any{"artifact": publicArtifact(updated)})
}

func assign[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

func (a *api) deleteArtifact(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	if err := a.deps.Store.DeleteArtifact(r.Context(), art.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, uuid.Nil, storage.EventDelete, 0, map[string]any{"title": art.Title, "id": art.ID.String()})
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) forkArtifact(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	var in struct {
		Title string `json:"title"`
		Space string `json:"space"`
	}
	_ = decode(r, &in)
	sp, err := a.resolveSpace(r, in.Space)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !a.canWriteSpace(r, sp) {
		writeErr(w, http.StatusForbidden, "you are not a member of this space")
		return
	}
	fork, v, err := a.deps.Store.Fork(r.Context(), art.ID, identityFrom(r).User.ID, sp.ID, in.Title)
	if errors.Is(err, storage.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "this artifact cannot be forked")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, art.ID, storage.EventFork, 0, map[string]any{"fork_id": fork.ID.String()})
	fork, _ = a.deps.Store.GetArtifact(r.Context(), fork.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"artifact": publicArtifact(fork), "version": publicVersionAs(v, false, identityFrom(r).User.DisplayName),
	})
}

func (a *api) moveArtifact(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	var in struct {
		Space string `json:"space"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	sp, err := a.resolveSpace(r, in.Space)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !a.canWriteSpace(r, sp) {
		writeErr(w, http.StatusForbidden, "you are not a member of the destination space")
		return
	}
	if err := a.deps.Store.MoveToSpace(r.Context(), art.ID, sp.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	updated, _ := a.deps.Store.GetArtifact(r.Context(), art.ID)
	writeJSON(w, http.StatusOK, map[string]any{"artifact": publicArtifact(updated)})
}

func (a *api) listForks(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	id := identityFrom(r)
	items, err := a.deps.Store.ForkChildren(r.Context(), art.ID, id.User.ID, id.User.Email)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.writeArtifactList(w, r, items)
}

// ---------------- Stats, activity, likes ----------------

func (a *api) artifactStats(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	s, err := a.deps.Store.ArtifactStats(r.Context(), art.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (a *api) artifactEvents(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	evs, err := a.deps.Store.ListEvents(r.Context(), art.ID, limit)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": mapSlice(evs, publicEvent)})
}

func (a *api) like(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	n, err := a.deps.Store.LikeArtifact(r.Context(), art.ID, identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": n, "liked": true})
}

func (a *api) unlike(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	n, err := a.deps.Store.UnlikeArtifact(r.Context(), art.ID, identityFrom(r).User.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": n, "liked": false})
}

// record writes to the audit trail without failing the request if it cannot:
// a log matters, but not as much as the operation it logs.
func (a *api) record(r *http.Request, artifactID uuid.UUID, kind string, version int, meta map[string]any) {
	id := identityFrom(r)
	err := a.deps.Store.RecordEvent(r.Context(), storage.RecordEventInput{
		ArtifactID: artifactID, ActorID: userIDOf(id), Kind: kind,
		VersionNumber: version, Via: id.via(r), Meta: meta,
		VisitorHash: "u:" + userIDOf(id).String(),
	})
	if err != nil {
		a.deps.Logger.Warn("audit", "err", err, "kind", kind)
	}
}
