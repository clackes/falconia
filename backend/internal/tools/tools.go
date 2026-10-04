// Package tools defines, in one place, what an agent can do on an instance.
//
// The same tools are exposed twice: as MCP tools for Claude Code, Codex,
// Gemini CLI, Cursor and anything else that speaks the protocol, and as
// tool-use for the built-in chat. One definition, because two hand-maintained
// lists drift apart, and when they do the user finds out, not the developer.
//
// The permission boundary is the same as the HTTP API's, for the same reason:
// here the caller acts with a key, and a key can do less than the person who
// issued it. Deleting an artifact, creating teams, inviting people to a team
// and managing keys are missing on purpose: those need a person in front of a
// screen.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/agents"
	"github.com/clackes/falconia/backend/internal/categories"
	"github.com/clackes/falconia/backend/internal/publish"
	"github.com/clackes/falconia/backend/internal/storage"
)

// Def describes one tool. InputSchema is JSON Schema.
type Def struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Runner runs tools on behalf of one specific user.
type Runner struct {
	Store      *storage.Postgres
	Publisher  *publish.Publisher
	AppBaseURL string
	User       *storage.User
	// Via ends up in the audit trail: "mcp" for an MCP client, "web" for the
	// chat. The agent's name travels separately, in the context.
	Via string
}

// Instructions is what an MCP client shows the model at startup, and the
// built-in chat's system prompt. It is written for any agent, not one in
// particular.
const Instructions = `Falconia is a shared home for artifacts — self-contained HTML pages, dashboards, documents and mini-apps — that any AI agent can create, and any other agent can later read, edit and publish again. Nothing here belongs to the agent that made it: an artifact made with Claude can be fixed with Codex or Gemini tomorrow.

Typical flow:
1. create_artifact with a complete, self-contained HTML document (or several files);
2. give the returned URL to the people who should see it (use visibility "link" for a link anyone can open);
3. list_threads to read the comments people pinned on the page;
4. deploy_artifact to publish the fix, then resolve_thread.

An artifact's URL never changes: versions pile up behind the same address.

Rules for the code you write:
- ONE complete HTML document (doctype, html, head, body), self-contained. Multi-file bundles are fine too: pass "files" with index.html at the root and relative paths.
- No network requests and no scripts from CDNs: inline everything.
- It runs in an opaque-origin iframe: no cookies, no window.parent, localStorage is per-artifact.
- To persist data use the injected global Falconia object (SFA is an alias):
    Falconia.setSchemaVersion(1)
    Falconia.onReady(({state, save}) => { ... })   // state is the saved JSON, or null
    Falconia.save(newState)
- To update, either send the WHOLE new document as "code", or send only the changed files in "files": every other file of the previous version is kept. Use get_artifact / get_file first to read the current source.

General access is "private" (only you and people added by email) by default. Use "org" to share with the whole team of the space, and "link" for anyone with the secret link, even without an account. Only change it when asked.`

// ---------------- Definitions ----------------

func obj(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
func str(d string) map[string]any  { return map[string]any{"type": "string", "description": d} }
func bln(d string) map[string]any  { return map[string]any{"type": "boolean", "description": d} }
func intg(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }

const idDesc = "the artifact: its UUID, 'space/slug', or its full URL"

// All is the complete tool list.
func All() []Def {
	visibility := map[string]any{
		"type": "string",
		"enum": []string{"private", "org", "link"},
		"description": "general access: private = you and the people added by email; " +
			"org = everyone in the space's team; link = anyone with the secret link, no account needed",
	}
	files := map[string]any{
		"type": "array",
		"description": "files to add or replace, relative to the bundle root (e.g. index.html, app.js, img/logo.svg). " +
			"Files of the previous version that you do not send are kept.",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     str("relative path inside the bundle"),
				"content":  str("file content: plain text, or base64 when encoding is base64"),
				"encoding": map[string]any{"type": "string", "enum": []string{"utf8", "base64"}, "description": "utf8 (default) or base64 for binary files"},
			},
			"required": []string{"path", "content"},
		},
	}
	return []Def{
		{"list_categories", "List the canonical artifact categories.", obj(nil)},
		{"list_spaces", "List the spaces you can publish to: your personal one and those of the teams you belong to.", obj(nil)},
		{"list_artifacts", "List the artifacts you can open, most recent first. Metadata only.", obj(map[string]any{
			"query":    str("case-insensitive substring of title or description"),
			"space":    str("slug of a space to restrict the search to"),
			"category": str("category"),
			"agent":    str("only artifacts that an agent published to, e.g. claude-code, codex, gemini-cli"),
			"limit":    intg("maximum number of results"),
		})},
		{"get_artifact", "Read an artifact: metadata, the source of its entry document and the list of its files. Read this before editing.", obj(map[string]any{
			"id":      str(idDesc),
			"version": intg("version number; latest when omitted"),
		}, "id")},
		{"get_file", "Read one text file of an artifact's bundle (e.g. app.js or styles.css).", obj(map[string]any{
			"id":      str(idDesc),
			"path":    str("path of the file inside the bundle"),
			"version": intg("version number; latest when omitted"),
		}, "id", "path")},
		{"list_versions", "List an artifact's version history, with the agent that published each version.", obj(map[string]any{
			"id": str(idDesc),
		}, "id")},
		{"create_artifact", "Create a new artifact from a complete, self-contained HTML document (code) or a set of files. Returns its stable URL.", obj(map[string]any{
			"title":       str("short title"),
			"code":        str("complete HTML document (single-file artifacts)"),
			"files":       files,
			"entry_path":  str("document to open for a bundle; defaults to index.html"),
			"description": str("short description"),
			"space":       str("space slug; your personal space when omitted"),
			"slug":        str("URL identifier; derived from the title when omitted"),
			"visibility":  visibility,
			"category":    str("one of: " + strings.Join(categories.Slugs(), ", ")),
			"icon":        str("fallback emoji"),
			"logo_url":    str("URL or data URL of a logo"),
			"theme_color": str("hex color"),
			"forkable":    bln("when false, nobody can fork it"),
		}, "title")},
		{"deploy_artifact", "Publish a new version of an existing artifact. The URL does not change. Send the whole new document as code, or only the changed files.", obj(map[string]any{
			"id":         str(idDesc),
			"code":       str("the complete updated entry document"),
			"files":      files,
			"delete":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "paths to remove from the bundle"},
			"entry_path": str("change the entry document"),
			"message":    str("what changed, in a few words"),
		}, "id")},
		{"update_artifact", "Update an artifact's metadata and general access: title, description, visibility, team role, category.", obj(map[string]any{
			"id":          str(idDesc),
			"title":       str("new title"),
			"description": str("new description"),
			"visibility":  visibility,
			"team_role":   map[string]any{"type": "string", "enum": []string{"viewer", "editor"}, "description": "what team members can do when visibility is org"},
			"category":    str("new category"),
			"slug":        str("new URL identifier"),
			"icon":        str("emoji"),
			"logo_url":    str("logo URL"),
			"theme_color": str("hex color"),
			"forkable":    bln("when false, nobody can fork it"),
		}, "id")},
		{"rotate_link", "Replace an artifact's secret share link with a new one. The old link stops working.", obj(map[string]any{
			"id": str(idDesc),
		}, "id")},
		{"fork_artifact", "Create an independent copy of an artifact. The copy starts private.", obj(map[string]any{
			"id":    str(idDesc),
			"title": str("title of the copy"),
			"space": str("destination space slug"),
		}, "id")},
		{"move_artifact", "Move an artifact to another space. Its /s/ address changes; its share link does not.", obj(map[string]any{
			"id":    str(idDesc),
			"space": str("destination space slug"),
		}, "id", "space")},
		{"share_artifact", "Give one email address access to an artifact. Works even if that person has no account yet.", obj(map[string]any{
			"id":    str(idDesc),
			"email": str("the invitee's email"),
			"role":  map[string]any{"type": "string", "enum": []string{"viewer", "editor"}, "description": "viewer reads and comments; editor can also publish"},
		}, "id", "email")},
		{"unshare_artifact", "Revoke an email address's access to an artifact.", obj(map[string]any{
			"id":    str(idDesc),
			"email": str("email to revoke"),
		}, "id", "email")},
		{"list_shares", "List the people with direct access to an artifact.", obj(map[string]any{
			"id": str(idDesc),
		}, "id")},
		{"list_threads", "Read the comments people pinned on an artifact. Use it before fixing: it says what to change and where (file, CSS selector, quoted text).", obj(map[string]any{
			"id":        str(idDesc),
			"only_open": bln("when true, only unresolved threads; default true"),
		}, "id")},
		{"comment_artifact", "Open a new comment thread on an artifact.", obj(map[string]any{
			"id":   str(idDesc),
			"body": str("comment text"),
		}, "id", "body")},
		{"reply_thread", "Reply to an existing comment thread.", obj(map[string]any{
			"thread_id": str("thread UUID"),
			"body":      str("reply text"),
		}, "thread_id", "body")},
		{"resolve_thread", "Mark a thread as resolved. Call it after publishing the fix that closes it.", obj(map[string]any{
			"thread_id": str("thread UUID"),
			"reopen":    bln("when true, reopen instead of resolving"),
		}, "thread_id")},
		{"artifact_stats", "An artifact's numbers: views, comments, versions.", obj(map[string]any{
			"id": str(idDesc),
		}, "id")},
		{"list_teams", "List the teams you belong to.", obj(nil)},
	}
}

// Scope maps each tool to the key scope it requires.
func Scope(name string) string {
	switch name {
	case "create_artifact", "deploy_artifact", "update_artifact", "rotate_link",
		"fork_artifact", "move_artifact", "share_artifact", "unshare_artifact":
		return storage.ScopeArtifactsWrite
	case "comment_artifact", "reply_thread", "resolve_thread":
		return storage.ScopeCommentsWrite
	case "list_threads":
		return storage.ScopeCommentsRead
	default:
		return storage.ScopeArtifactsRead
	}
}

// ---------------- Execution ----------------

var ErrUnknownTool = errors.New("unknown tool")

type input struct {
	ID          string         `json:"id"`
	ThreadID    string         `json:"thread_id"`
	Query       string         `json:"query"`
	Space       string         `json:"space"`
	Category    string         `json:"category"`
	Agent       string         `json:"agent"`
	Limit       int            `json:"limit"`
	Version     int            `json:"version"`
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Code        string         `json:"code"`
	Files       []publish.File `json:"files"`
	Delete      []string       `json:"delete"`
	EntryPath   string         `json:"entry_path"`
	Message     string         `json:"message"`
	Slug        string         `json:"slug"`
	Visibility  string         `json:"visibility"`
	TeamRole    string         `json:"team_role"`
	Icon        string         `json:"icon"`
	LogoURL     string         `json:"logo_url"`
	ThemeColor  string         `json:"theme_color"`
	Forkable    *bool          `json:"forkable"`
	Email       string         `json:"email"`
	Role        string         `json:"role"`
	Body        string         `json:"body"`
	OnlyOpen    *bool          `json:"only_open"`
	Reopen      bool           `json:"reopen"`
}

// Call runs a tool and returns the JSON to report back to the model.
func (r *Runner) Call(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	var in input
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}

	switch name {
	case "list_categories":
		return jsonOf(map[string]any{"categories": categories.All})

	case "list_spaces":
		spaces, err := r.Store.ListMySpaces(ctx, r.User.ID)
		if err != nil {
			return "", err
		}
		out := make([]map[string]any, 0, len(spaces))
		for _, s := range spaces {
			out = append(out, map[string]any{"slug": s.Slug, "name": s.Name, "kind": s.Kind, "my_role": s.MyRole})
		}
		return jsonOf(map[string]any{"spaces": out})

	case "list_teams", "list_orgs":
		orgs, err := r.Store.ListMyOrgs(ctx, r.User.ID)
		if err != nil {
			return "", err
		}
		out := make([]map[string]any, 0, len(orgs))
		for _, o := range orgs {
			out = append(out, map[string]any{"name": o.Name, "slug": o.Slug, "space": o.SpaceSlug, "my_role": o.MyRole})
		}
		return jsonOf(map[string]any{"teams": out})

	case "list_artifacts":
		opt := storage.ListFeedOptions{Query: in.Query, Category: in.Category, Agent: agents.Normalize(in.Agent), Limit: in.Limit}
		if in.Space != "" {
			sp, err := r.Store.GetSpaceBySlug(ctx, in.Space)
			if err != nil {
				return "", errors.New("unknown space " + in.Space)
			}
			opt.SpaceID = &sp.ID
		}
		items, err := r.Store.ListVisibleTo(ctx, r.User.ID, r.User.Email, opt)
		if err != nil {
			return "", err
		}
		return jsonOf(map[string]any{"artifacts": r.brief(items)})

	case "get_artifact":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		v, err := r.Store.GetVersion(ctx, art.ID, in.Version)
		if err != nil {
			return "", err
		}
		out := r.one(art)
		out["version"] = v.VersionNumber
		out["entry_path"] = v.EntryPath
		out["published_by_agent"] = v.Agent
		out["my_role"] = string(role)
		// Source goes only to those who could change it: read-only access
		// lets you watch the artifact run, not read its code.
		if role.CanDeploy() {
			out["code"] = v.CodePayload
		} else {
			out["note"] = "you have view access only: the source is not available"
		}
		if files, err := r.Store.ListFiles(ctx, v.ID); err == nil && len(files) > 0 {
			list := make([]map[string]any, 0, len(files))
			for _, f := range files {
				list = append(list, map[string]any{"path": f.Path, "size": f.SizeBytes, "type": f.ContentType})
			}
			out["files"] = list
		}
		return jsonOf(out)

	case "get_file":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if !role.CanDeploy() {
			return "", errors.New("you have view access only: the source is not available")
		}
		v, err := r.Store.GetVersion(ctx, art.ID, in.Version)
		if err != nil {
			return "", err
		}
		f, err := r.Store.GetFile(ctx, v.ID, in.Path)
		if errors.Is(err, storage.ErrNotFound) && storage.NormalizePath(in.Path) == v.EntryPath {
			return jsonOf(map[string]any{"path": v.EntryPath, "version": v.VersionNumber, "content": v.CodePayload})
		}
		if err != nil {
			return "", fmt.Errorf("no file %q in version %d", in.Path, v.VersionNumber)
		}
		text, err := r.Publisher.ReadText(ctx, f.SHA256, 1<<20)
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.Path, err)
		}
		return jsonOf(map[string]any{"path": f.Path, "version": v.VersionNumber, "content": text})

	case "list_versions":
		art, _, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		vs, err := r.Store.ListVersions(ctx, art.ID)
		if err != nil {
			return "", err
		}
		out := make([]map[string]any, 0, len(vs))
		for _, v := range vs {
			out = append(out, map[string]any{
				"version": v.VersionNumber, "message": v.CommitMessage, "author": v.AuthorName,
				"agent": v.Agent, "created_at": v.CreatedAt, "files": v.FileCount,
			})
		}
		return jsonOf(map[string]any{"versions": out})

	case "create_artifact":
		if strings.TrimSpace(in.Title) == "" {
			return "", errors.New("title is required")
		}
		if strings.TrimSpace(in.Code) == "" && len(in.Files) == 0 {
			return "", errors.New("send either code (a complete HTML document) or files")
		}
		sp, err := r.space(ctx, in.Space)
		if err != nil {
			return "", err
		}
		res, err := r.Publisher.Build(ctx, nil, in.EntryPath, in.Code, in.Files, nil)
		if err != nil {
			return "", err
		}
		art, v, err := r.Store.CreateArtifact(ctx, r.User.ID, storage.CreateArtifactInput{
			SpaceID: sp.ID, Title: in.Title, Description: in.Description, Code: res.Code,
			Icon: in.Icon, LogoURL: in.LogoURL, ThemeColor: in.ThemeColor,
			Slug: in.Slug, Category: in.Category, Visibility: in.Visibility, Forkable: in.Forkable,
			EntryPath: res.EntryPath, Files: res.Files, Message: in.Message,
		})
		if err != nil {
			return "", err
		}
		r.audit(ctx, art.ID, storage.EventCreate, v.VersionNumber, nil)
		art, _ = r.Store.GetArtifact(ctx, art.ID)
		out := r.one(art)
		out["version"] = v.VersionNumber
		return jsonOf(out)

	case "deploy_artifact":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if !role.CanDeploy() {
			return "", errors.New("you don't have permission to publish to this artifact")
		}
		if strings.TrimSpace(in.Code) == "" && len(in.Files) == 0 && len(in.Delete) == 0 {
			return "", errors.New("nothing to publish: send code, files or delete")
		}
		prev, err := r.Store.GetVersion(ctx, art.ID, 0)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return "", err
		}
		res, err := r.Publisher.Build(ctx, prev, in.EntryPath, in.Code, in.Files, in.Delete)
		if err != nil {
			return "", err
		}
		v, err := r.Store.CommitVersion(ctx, art.ID, r.User.ID,
			storage.NewVersionInput(res.Code, orDefault(in.Message, "deploy"), res.EntryPath, res.Files))
		if err != nil {
			return "", err
		}
		r.audit(ctx, art.ID, storage.EventDeploy, v.VersionNumber, map[string]any{"files": len(res.Files)})
		if fresh, err := r.Store.GetArtifact(ctx, art.ID); err == nil {
			art = fresh
		}
		out := r.one(art)
		out["version"] = v.VersionNumber
		return jsonOf(out)

	case "update_artifact":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if role != storage.RoleOwner {
			return "", errors.New("only the artifact's owner can change its metadata or access")
		}
		m := storage.MetaOf(art)
		m.Title = orDefault(in.Title, m.Title)
		m.Description = orDefault(in.Description, m.Description)
		m.Visibility = orDefault(in.Visibility, m.Visibility)
		m.TeamRole = orDefault(in.TeamRole, m.TeamRole)
		m.Icon = orDefault(in.Icon, m.Icon)
		m.ThemeColor = orDefault(in.ThemeColor, m.ThemeColor)
		m.LogoURL = orDefault(in.LogoURL, m.LogoURL)
		m.Slug = orDefault(in.Slug, m.Slug)
		m.Category = orDefault(in.Category, m.Category)
		if in.Forkable != nil {
			m.Forkable = *in.Forkable
		}
		if err := r.Store.UpdateMeta(ctx, art.ID, m); err != nil {
			return "", err
		}
		if in.Visibility != "" {
			r.audit(ctx, art.ID, storage.EventShare, 0, map[string]any{"general_access": storage.ValidVisibility(in.Visibility)})
		}
		updated, _ := r.Store.GetArtifact(ctx, art.ID)
		return jsonOf(r.one(updated))

	case "rotate_link":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if role != storage.RoleOwner {
			return "", errors.New("only the artifact's owner can rotate its link")
		}
		if _, err := r.Store.RotateLinkToken(ctx, art.ID); err != nil {
			return "", err
		}
		r.audit(ctx, art.ID, storage.EventShare, 0, map[string]any{"link": "rotated"})
		updated, _ := r.Store.GetArtifact(ctx, art.ID)
		return jsonOf(r.one(updated))

	case "fork_artifact":
		art, _, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		sp, err := r.space(ctx, in.Space)
		if err != nil {
			return "", err
		}
		fork, _, err := r.Store.Fork(ctx, art.ID, r.User.ID, sp.ID, in.Title)
		if errors.Is(err, storage.ErrForbidden) {
			return "", errors.New("this artifact cannot be forked")
		}
		if err != nil {
			return "", err
		}
		r.audit(ctx, art.ID, storage.EventFork, 0, map[string]any{"fork_id": fork.ID.String()})
		fork, _ = r.Store.GetArtifact(ctx, fork.ID)
		return jsonOf(r.one(fork))

	case "move_artifact":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if role != storage.RoleOwner {
			return "", errors.New("only the artifact's owner can move it")
		}
		sp, err := r.space(ctx, in.Space)
		if err != nil {
			return "", err
		}
		if err := r.Store.MoveToSpace(ctx, art.ID, sp.ID); err != nil {
			return "", err
		}
		moved, _ := r.Store.GetArtifact(ctx, art.ID)
		return jsonOf(r.one(moved))

	case "share_artifact":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if role != storage.RoleOwner {
			return "", errors.New("only the artifact's owner can share it")
		}
		if err := r.Store.AddShare(ctx, art.ID, r.User.ID, in.Email, in.Role); err != nil {
			return "", err
		}
		r.audit(ctx, art.ID, storage.EventShare, 0, map[string]any{"email": strings.ToLower(in.Email)})
		return jsonOf(map[string]any{"ok": true, "email": strings.ToLower(in.Email), "url": r.url(art)})

	case "unshare_artifact":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if role != storage.RoleOwner {
			return "", errors.New("only the artifact's owner can revoke access")
		}
		if err := r.Store.RemoveShare(ctx, art.ID, in.Email); err != nil {
			return "", err
		}
		return jsonOf(map[string]any{"ok": true})

	case "list_shares":
		art, role, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		if role != storage.RoleOwner {
			return "", errors.New("only the artifact's owner can see who it is shared with")
		}
		shares, err := r.Store.ListShares(ctx, art.ID)
		if err != nil {
			return "", err
		}
		out := make([]map[string]any, 0, len(shares))
		for _, s := range shares {
			out = append(out, map[string]any{"email": s.Email, "role": s.Role})
		}
		return jsonOf(map[string]any{"general_access": art.Visibility, "people": out})

	case "list_threads":
		art, _, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		onlyOpen := true
		if in.OnlyOpen != nil {
			onlyOpen = *in.OnlyOpen
		}
		threads, err := r.Store.ListThreads(ctx, art.ID, storage.ThreadFilter{OnlyOpen: onlyOpen})
		if err != nil {
			return "", err
		}
		return jsonOf(map[string]any{
			"artifact": r.url(art),
			"threads":  briefThreads(threads),
		})

	case "comment_artifact":
		art, _, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		n := 1
		if art.LatestVersion != nil {
			n = *art.LatestVersion
		}
		t, err := r.Store.CreateThread(ctx, art.ID, r.User.ID, n, "index.html", in.Body, nil, r.Via)
		if err != nil {
			return "", err
		}
		r.audit(ctx, art.ID, storage.EventComment, n, map[string]any{"thread_id": t.ID.String()})
		return jsonOf(map[string]any{"thread_id": t.ID, "status": t.Status})

	case "reply_thread", "resolve_thread":
		tid, err := uuid.Parse(strings.TrimSpace(in.ThreadID))
		if err != nil {
			return "", errors.New("invalid thread_id")
		}
		t, err := r.Store.GetThread(ctx, tid)
		if err != nil {
			return "", errors.New("thread not found")
		}
		// Permission is checked on the artifact, never on the thread: a
		// guessed id must not open a closed door.
		role, err := r.Store.ResolveAccess(ctx, t.ArtifactID, r.User.ID, r.User.Email)
		if err != nil || !role.CanRead() {
			return "", errors.New("thread not found")
		}
		if name == "reply_thread" {
			if _, err := r.Store.Reply(ctx, tid, r.User.ID, in.Body, r.Via); err != nil {
				return "", err
			}
			r.audit(ctx, t.ArtifactID, storage.EventComment, t.VersionNumber, map[string]any{"thread_id": tid.String()})
			return jsonOf(map[string]any{"ok": true, "thread_id": tid})
		}
		if !role.CanDeploy() && t.CreatedBy != r.User.ID {
			return "", errors.New("only the thread's author or someone who can edit the artifact can close it")
		}
		if err := r.Store.SetThreadStatus(ctx, tid, r.User.ID, !in.Reopen); err != nil {
			return "", err
		}
		if !in.Reopen {
			r.audit(ctx, t.ArtifactID, storage.EventResolve, t.VersionNumber, map[string]any{"thread_id": tid.String()})
		}
		return jsonOf(map[string]any{"ok": true, "thread_id": tid, "status": statusOf(!in.Reopen)})

	case "artifact_stats":
		art, _, err := r.resolve(ctx, in.ID)
		if err != nil {
			return "", err
		}
		s, err := r.Store.ArtifactStats(ctx, art.ID)
		if err != nil {
			return "", err
		}
		return jsonOf(s)
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownTool, name)
}

// ---------------- Support ----------------

// resolve accepts a UUID, "space/slug", "/s/space/slug", a full app URL or a
// share link, and checks access in one go. Agents paste whatever they were
// given, and refusing a perfectly good URL would be pedantic.
func (r *Runner) resolve(ctx context.Context, ref string) (*storage.Artifact, storage.Role, error) {
	ref = strings.TrimSpace(ref)
	if base := strings.TrimRight(r.AppBaseURL, "/"); base != "" {
		ref = strings.TrimPrefix(ref, base)
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	var art *storage.Artifact
	var err error
	switch {
	case strings.HasPrefix(ref, "/p/"):
		art, err = r.Store.GetArtifactByLinkToken(ctx, strings.TrimPrefix(ref, "/p/"))
		if err == nil {
			_ = r.Store.RecordLinkVisit(ctx, art.ID, r.User.ID)
		}
	case strings.HasPrefix(ref, "/a/"):
		id, perr := uuid.Parse(strings.TrimPrefix(ref, "/a/"))
		if perr != nil {
			return nil, storage.RoleNone, errors.New("invalid artifact id")
		}
		art, err = r.Store.GetArtifact(ctx, id)
	default:
		if id, perr := uuid.Parse(ref); perr == nil {
			art, err = r.Store.GetArtifact(ctx, id)
		} else if space, slug, ok := strings.Cut(strings.Trim(strings.TrimPrefix(ref, "/s/"), "/"), "/"); ok {
			art, err = r.Store.GetArtifactByPath(ctx, space, slug)
		} else {
			return nil, storage.RoleNone, errors.New("invalid identifier: use a UUID, 'space/slug' or the artifact's URL")
		}
	}
	if err != nil {
		return nil, storage.RoleNone, errors.New("artifact not found")
	}
	role, err := r.Store.ResolveAccess(ctx, art.ID, r.User.ID, r.User.Email)
	if err != nil || !role.CanRead() {
		return nil, storage.RoleNone, errors.New("artifact not found")
	}
	return art, role, nil
}

func (r *Runner) space(ctx context.Context, slug string) (*storage.Space, error) {
	if strings.TrimSpace(slug) == "" {
		return r.Store.GetSpaceByOwner(ctx, r.User.ID)
	}
	sp, err := r.Store.GetSpaceBySlug(ctx, slug)
	if err != nil {
		return nil, errors.New("unknown space " + slug)
	}
	role, err := r.Store.SpaceRole(ctx, sp, r.User.ID)
	if err != nil || role == "" {
		return nil, errors.New("you are not a member of the space " + slug)
	}
	return sp, nil
}

func (r *Runner) url(a *storage.Artifact) string { return r.AppBaseURL + a.Path() }

func (r *Runner) one(a *storage.Artifact) map[string]any {
	out := map[string]any{
		"id": a.ID, "title": a.Title, "description": a.Description,
		"url": r.url(a), "space": a.SpaceSlug, "visibility": a.Visibility,
		"category": a.Category, "open_threads": a.OpenThreads,
		"author": a.AuthorName, "agents": a.Agents, "updated_at": a.UpdatedAt,
	}
	if lp := a.LinkPath(); lp != "" {
		out["share_url"] = r.AppBaseURL + lp
	}
	if a.Visibility == "org" {
		out["team_role"] = a.TeamRole
	}
	if a.Slug != nil {
		out["slug"] = *a.Slug
	}
	if a.LatestVersion != nil {
		out["latest_version"] = *a.LatestVersion
	}
	return out
}

func (r *Runner) brief(items []storage.Artifact) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, r.one(&items[i]))
	}
	return out
}

// briefThreads reduces a thread to what is needed for a fix: where, what and
// who. The rest is noise that would eat context.
func briefThreads(ts []storage.Thread) []map[string]any {
	out := make([]map[string]any, 0, len(ts))
	for _, t := range ts {
		msgs := make([]string, 0, len(t.Comments))
		for _, c := range t.Comments {
			who := c.UserName
			if c.Agent != "" {
				who += " (via " + c.Agent + ")"
			}
			msgs = append(msgs, who+": "+c.Body)
		}
		row := map[string]any{
			"thread_id": t.ID, "status": t.Status, "page": t.PagePath,
			"opened_on_version": t.VersionNumber, "messages": msgs,
		}
		if t.Anchor != nil {
			if t.Anchor.Selector != "" {
				row["element"] = t.Anchor.Selector
			}
			if t.Anchor.Quote != "" {
				row["quoted_text"] = t.Anchor.Quote
			}
		}
		out = append(out, row)
	}
	return out
}

func (r *Runner) audit(ctx context.Context, artifactID uuid.UUID, kind string, version int, meta map[string]any) {
	_ = r.Store.RecordEvent(ctx, storage.RecordEventInput{
		ArtifactID: artifactID, ActorID: r.User.ID, Kind: kind,
		VersionNumber: version, Via: r.Via, Meta: meta,
		VisitorHash: "u:" + r.User.ID.String(),
	})
}

func jsonOf(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func statusOf(resolved bool) string {
	if resolved {
		return "resolved"
	}
	return "open"
}
