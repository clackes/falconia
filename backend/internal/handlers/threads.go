package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/storage"
)

// The review loop.
//
// Anyone who can open an artifact can comment on it, without being given the
// source: that is the point of the whole product. A person pins a comment on
// the page; whichever agent the author uses today reads it, fixes the page,
// publishes and resolves the thread. The artifact's owner and the thread's
// author can close it.

func (a *api) listThreads(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	f := storage.ThreadFilter{
		OnlyOpen: r.URL.Query().Get("status") == "open",
		PagePath: r.URL.Query().Get("page"),
	}
	f.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	threads, err := a.deps.Store.ListThreads(r.Context(), art.ID, f)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": mapSlice(threads, publicThread)})
}

func (a *api) createThread(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	var in struct {
		Body          string          `json:"body"`
		PagePath      string          `json:"page_path"`
		VersionNumber int             `json:"version_number"`
		Anchor        *storage.Anchor `json:"anchor"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Body) == "" {
		writeErr(w, http.StatusBadRequest, "the comment is empty")
		return
	}
	if in.VersionNumber <= 0 && art.LatestVersion != nil {
		in.VersionNumber = *art.LatestVersion
	}
	id := identityFrom(r)
	t, err := a.deps.Store.CreateThread(r.Context(), art.ID, id.User.ID,
		in.VersionNumber, in.PagePath, in.Body, in.Anchor, id.via(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, art.ID, storage.EventComment, in.VersionNumber, map[string]any{"thread_id": t.ID.String()})
	writeJSON(w, http.StatusCreated, map[string]any{"thread": publicThread(*t)})
}

// withThread resolves a thread and checks that the caller can see the
// artifact it belongs to.
//
// The check always goes through the artifact, never the thread: a guessed
// thread id must not open a door the artifact keeps shut.
func (a *api) withThread(fn func(http.ResponseWriter, *http.Request, *storage.Thread, *storage.Artifact, storage.Role)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tid, err := uuid.Parse(chi.URLParam(r, "tid"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid id")
			return
		}
		t, err := a.deps.Store.GetThread(r.Context(), tid)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		art, role, err := a.access(r, t.ArtifactID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		fn(w, r, t, art, role)
	}
}

func (a *api) replyThread(w http.ResponseWriter, r *http.Request, t *storage.Thread, art *storage.Artifact, _ storage.Role) {
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	id := identityFrom(r)
	c, err := a.deps.Store.Reply(r.Context(), t.ID, id.User.ID, in.Body, id.via(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	a.record(r, art.ID, storage.EventComment, t.VersionNumber, map[string]any{"thread_id": t.ID.String()})
	c.UserName = id.User.DisplayName
	writeJSON(w, http.StatusCreated, map[string]any{"comment": publicComment(*c)})
}

func (a *api) setThreadStatus(w http.ResponseWriter, r *http.Request, t *storage.Thread, art *storage.Artifact, role storage.Role) {
	var in struct {
		Resolved bool `json:"resolved"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	uid := identityFrom(r).User.ID
	// Editors may resolve too: the agent fixing a comment works with the key
	// of someone who can publish, and must be able to close what it fixed.
	if role != storage.RoleOwner && !role.CanDeploy() && t.CreatedBy != uid {
		writeErr(w, http.StatusForbidden,
			"only the thread's author or someone who can edit the artifact can close it")
		return
	}
	if err := a.deps.Store.SetThreadStatus(r.Context(), t.ID, uid, in.Resolved); err != nil {
		writeStoreErr(w, err)
		return
	}
	if in.Resolved {
		a.record(r, art.ID, storage.EventResolve, t.VersionNumber, map[string]any{"thread_id": t.ID.String()})
	}
	updated, err := a.deps.Store.GetThread(r.Context(), t.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": publicThread(*updated)})
}

func (a *api) deleteThread(w http.ResponseWriter, r *http.Request, t *storage.Thread, _ *storage.Artifact, role storage.Role) {
	if role != storage.RoleOwner && t.CreatedBy != identityFrom(r).User.ID {
		writeErr(w, http.StatusForbidden, "you cannot delete this thread")
		return
	}
	if err := a.deps.Store.DeleteThread(r.Context(), t.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) editComment(w http.ResponseWriter, r *http.Request) {
	cid, err := uuid.Parse(chi.URLParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := a.deps.Store.EditComment(r.Context(), cid, identityFrom(r).User.ID, in.Body); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) deleteComment(w http.ResponseWriter, r *http.Request) {
	cid, err := uuid.Parse(chi.URLParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := a.deps.Store.DeleteComment(r.Context(), cid, identityFrom(r).User.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reviewDigest is the summary an agent reads.
//
// Plain text, not JSON, in a format that can be pasted into a prompt as is:
// it is what `falconia comments` prints inside a Claude Code, Codex or Gemini
// session. It includes the file path and the selector, because the agent needs
// to know WHERE to act, not just that someone complained.
func (a *api) reviewDigest(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	threads, err := a.deps.Store.ListThreads(r.Context(), art.ID, storage.ThreadFilter{OnlyOpen: true})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var b strings.Builder
	b.WriteString("# Open comments on \"" + art.Title + "\"\n")
	b.WriteString(a.deps.AppBaseURL + art.Path() + "\n\n")
	if len(threads) == 0 {
		b.WriteString("No open comments.\n")
	}
	for i, t := range threads {
		b.WriteString("## Thread " + itoa(i+1) + "  [" + t.ID.String() + "]\n")
		b.WriteString("File: " + t.PagePath + "  ·  opened on version " + itoa(t.VersionNumber) + "\n")
		if t.Anchor != nil {
			if t.Anchor.Selector != "" {
				b.WriteString("Element: " + t.Anchor.Selector + "\n")
			}
			if t.Anchor.Quote != "" {
				b.WriteString("Text: \"" + t.Anchor.Quote + "\"\n")
			}
		}
		for _, c := range t.Comments {
			who := c.UserName
			if c.Agent != "" {
				who += " (via " + c.Agent + ")"
			}
			b.WriteString("- " + who + ": " + c.Body + "\n")
		}
		b.WriteString("\n")
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
