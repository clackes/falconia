package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreErr maps storage errors to HTTP codes.
//
// ErrForbidden becomes 404, not 403, on read paths: telling someone without
// access "it exists but you can't" reveals that the artifact exists, which is
// already information. Whoever is entitled to know it exists can see it.
func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, storage.ErrForbidden):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, storage.ErrConflict):
		writeErr(w, http.StatusConflict, "already exists")
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	return dec.Decode(v)
}

// ---------------- JSON shapes ----------------

type userJSON struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	CreatedAt   time.Time `json:"created_at"`
}

func publicUser(u *storage.User) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName,
		IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt}
}

type spaceJSON struct {
	ID     uuid.UUID `json:"id"`
	Slug   string    `json:"slug"`
	Name   string    `json:"name"`
	Kind   string    `json:"kind"`
	MyRole string    `json:"my_role,omitempty"`
}

func publicSpace(s storage.Space) spaceJSON {
	return spaceJSON{ID: s.ID, Slug: s.Slug, Name: s.Name, Kind: s.Kind, MyRole: s.MyRole}
}

type artifactJSON struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	TeamRole    string    `json:"team_role"`
	Slug        string    `json:"slug,omitempty"`
	Path        string    `json:"path"`
	// LinkPath is /p/<token> while general access is "anyone with the link".
	LinkPath   string `json:"link_path,omitempty"`
	Icon       string `json:"icon"`
	LogoURL    string `json:"logo_url"`
	ThemeColor string `json:"theme_color"`
	Category   string `json:"category"`
	Forkable   bool   `json:"forkable"`

	SpaceID   uuid.UUID `json:"space_id"`
	SpaceSlug string    `json:"space_slug"`
	SpaceName string    `json:"space_name"`
	SpaceKind string    `json:"space_kind"`

	AuthorID   uuid.UUID `json:"author_id"`
	AuthorName string    `json:"author_name"`

	LatestVersion int        `json:"latest_version"`
	ForkParentID  *uuid.UUID `json:"fork_parent_id,omitempty"`
	OpenThreads   int        `json:"open_threads"`
	// Agents that published versions of this artifact, most recent first.
	Agents []string `json:"agents"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Only filled in when the caller is known and the endpoint computes them.
	MyRole string `json:"my_role,omitempty"`
	Likes  int    `json:"likes"`
	Liked  bool   `json:"liked"`
}

func publicArtifact(a *storage.Artifact) artifactJSON {
	out := artifactJSON{
		ID: a.ID, Title: a.Title, Description: a.Description, Visibility: a.Visibility,
		TeamRole: a.TeamRole, Path: a.Path(), LinkPath: a.LinkPath(),
		Icon: a.Icon, LogoURL: a.LogoURL, ThemeColor: a.ThemeColor,
		Category: a.Category, Forkable: a.Forkable,
		SpaceID: a.SpaceID, SpaceSlug: a.SpaceSlug, SpaceName: a.SpaceName, SpaceKind: a.SpaceKind,
		AuthorID: a.AuthorID, AuthorName: a.AuthorName,
		ForkParentID: a.ForkParentID, OpenThreads: a.OpenThreads, Agents: a.Agents,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if out.Agents == nil {
		out.Agents = []string{}
	}
	if a.Slug != nil {
		out.Slug = *a.Slug
	}
	if a.LatestVersion != nil {
		out.LatestVersion = *a.LatestVersion
	}
	return out
}

// linkArtifactJSON is what an anonymous visitor of a shared link learns about
// an artifact: enough to show a header, nothing about who else can see it or
// where it lives.
type linkArtifactJSON struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	AuthorName    string    `json:"author_name"`
	LatestVersion int       `json:"latest_version"`
	Agents        []string  `json:"agents"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func publicLinkArtifact(a *storage.Artifact) linkArtifactJSON {
	out := linkArtifactJSON{
		ID: a.ID, Title: a.Title, Description: a.Description,
		AuthorName: a.AuthorName, Agents: a.Agents, UpdatedAt: a.UpdatedAt,
	}
	if out.Agents == nil {
		out.Agents = []string{}
	}
	if a.LatestVersion != nil {
		out.LatestVersion = *a.LatestVersion
	}
	return out
}

type versionJSON struct {
	ID            int64     `json:"id"`
	VersionNumber int       `json:"version_number"`
	CommitMessage string    `json:"commit_message"`
	AuthorID      uuid.UUID `json:"author_id"`
	AuthorName    string    `json:"author_name"`
	Agent         string    `json:"agent"`
	EntryPath     string    `json:"entry_path"`
	Kind          string    `json:"kind"`
	FileCount     int       `json:"file_count"`
	TotalBytes    int64     `json:"total_bytes"`
	CodeHash      string    `json:"code_hash"`
	CreatedAt     time.Time `json:"created_at"`
	Code          string    `json:"code,omitempty"`
}

// publicVersionAs takes the author's name from outside when the version comes
// from an INSERT that just happened: storage returns the id, not the name, and
// a response with an empty author would force the frontend into a second call
// just to print who published.
func publicVersionAs(v *storage.ArtifactVersion, withCode bool, authorName string) versionJSON {
	out := publicVersion(v, withCode)
	if out.AuthorName == "" {
		out.AuthorName = authorName
	}
	return out
}

func publicVersion(v *storage.ArtifactVersion, withCode bool) versionJSON {
	out := versionJSON{
		ID: v.ID, VersionNumber: v.VersionNumber, CommitMessage: v.CommitMessage,
		AuthorID: v.AuthorID, AuthorName: v.AuthorName, Agent: v.Agent, EntryPath: v.EntryPath,
		Kind: v.Kind, FileCount: v.FileCount, TotalBytes: v.TotalBytes,
		CodeHash: v.CodeHash, CreatedAt: v.CreatedAt,
	}
	if withCode {
		out.Code = v.CodePayload
	}
	return out
}

type commentJSON struct {
	ID        uuid.UUID `json:"id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	UserID    uuid.UUID `json:"user_id"`
	UserName  string    `json:"user_name"`
	Body      string    `json:"body"`
	Via       string    `json:"via"`
	Agent     string    `json:"agent"`
	Edited    bool      `json:"edited"`
	CreatedAt time.Time `json:"created_at"`
}

type threadJSON struct {
	ID            uuid.UUID       `json:"id"`
	ArtifactID    uuid.UUID       `json:"artifact_id"`
	VersionNumber int             `json:"version_number"`
	PagePath      string          `json:"page_path"`
	Anchor        *storage.Anchor `json:"anchor,omitempty"`
	Status        string          `json:"status"`
	CreatedBy     uuid.UUID       `json:"created_by"`
	CreatedByName string          `json:"created_by_name"`
	ResolvedAt    *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	Comments      []commentJSON   `json:"comments"`
}

func publicComment(c storage.Comment) commentJSON {
	return commentJSON{
		ID: c.ID, ThreadID: c.ThreadID, UserID: c.UserID, UserName: c.UserName,
		Body: c.Body, Via: c.Via, Agent: c.Agent, Edited: c.Edited, CreatedAt: c.CreatedAt,
	}
}

func publicThread(t storage.Thread) threadJSON {
	out := threadJSON{
		ID: t.ID, ArtifactID: t.ArtifactID, VersionNumber: t.VersionNumber,
		PagePath: t.PagePath, Anchor: t.Anchor, Status: t.Status,
		CreatedBy: t.CreatedBy, CreatedByName: t.CreatedByName,
		ResolvedAt: t.ResolvedAt, CreatedAt: t.CreatedAt,
		Comments: make([]commentJSON, 0, len(t.Comments)),
	}
	for _, c := range t.Comments {
		out.Comments = append(out.Comments, publicComment(c))
	}
	return out
}

type keyJSON struct {
	ID         uuid.UUID  `json:"id"`
	Label      string     `json:"label"`
	Prefix     string     `json:"prefix"`
	Kind       string     `json:"kind"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func publicKey(k storage.APIKey) keyJSON {
	return keyJSON{ID: k.ID, Label: k.Label, Prefix: k.Prefix, Kind: k.Kind,
		Scopes: k.Scopes, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt}
}

type eventJSON struct {
	ID            int64          `json:"id"`
	ArtifactID    *uuid.UUID     `json:"artifact_id,omitempty"`
	ArtifactTitle string         `json:"artifact_title,omitempty"`
	ActorName     string         `json:"actor_name"`
	Kind          string         `json:"kind"`
	VersionNumber *int           `json:"version_number,omitempty"`
	Via           string         `json:"via"`
	Agent         string         `json:"agent"`
	Meta          map[string]any `json:"meta,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

func publicEvent(e storage.Event) eventJSON {
	return eventJSON{ID: e.ID, ArtifactID: e.ArtifactID, ArtifactTitle: e.ArtifactTitle,
		ActorName: e.ActorName, Kind: e.Kind, VersionNumber: e.VersionNumber,
		Via: e.Via, Agent: e.Agent, Meta: e.Meta, CreatedAt: e.CreatedAt}
}

func mapSlice[T any, R any](in []T, f func(T) R) []R {
	out := make([]R, 0, len(in))
	for _, v := range in {
		out = append(out, f(v))
	}
	return out
}
