package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/clackes/falconia/backend/internal/agentbridge"
	"github.com/clackes/falconia/backend/internal/blob"
	"github.com/clackes/falconia/backend/internal/categories"
	"github.com/clackes/falconia/backend/internal/mcp"
	"github.com/clackes/falconia/backend/internal/storage"
)

type Deps struct {
	Store  *storage.Postgres
	Blobs  blob.Store
	Agents *agentbridge.Hub

	AppBaseURL     string
	ContentBaseURL string
	APIBaseURL     string

	JWTSecret       []byte
	ContentSecret   []byte
	ContentGrantTTL time.Duration

	AllowRegistration bool
	BlobBackend       string
	MaxUploadBytes    int64
	MaxBundleFiles    int

	Logger *slog.Logger
}

// NewRouter builds the application API.
//
// Three access levels run through every route:
//
//	auth         — a session or a key
//	requireScope — a key must hold the right scope
//	humanOnly    — keys are refused: destruction and key management
//
// The only anonymous routes are health, configuration, sign-in, the CLI
// installer and /api/p/{token}, which serves artifacts whose owner chose
// "anyone with the link".
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders())
	r.Use(corsMW(d.AppBaseURL))

	a := &api{deps: d}
	const (
		read   = storage.ScopeArtifactsRead
		write  = storage.ScopeArtifactsWrite
		cread  = storage.ScopeCommentsRead
		cwrite = storage.ScopeCommentsWrite
	)

	r.Get("/api/health", a.health)
	r.Get("/api/config", a.config)
	r.Get("/api/categories", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"categories": categories.All})
	})

	// --- Authentication ---
	r.Post("/api/auth/register", a.register)
	r.Post("/api/auth/login", a.login)
	r.Get("/api/auth/me", a.auth(a.me))

	// CLI login. Start and poll are public by necessity: whoever calls them
	// has no token yet. Approving needs a real session, because that is where
	// access is granted.
	r.Post("/api/auth/device/start", a.startDeviceLogin)
	r.Post("/api/auth/device/poll", a.pollDeviceLogin)
	r.Get("/api/auth/device/{code}", a.humanOnly(a.describeDeviceLogin))
	r.Post("/api/auth/device/approve", a.humanOnly(a.approveDeviceLogin))

	// --- Anyone with the link ---
	r.Get("/api/p/{token}", a.openLink)

	// --- Spaces ---
	r.Get("/api/spaces", a.auth(a.listSpaces))
	r.Get("/api/spaces/{space}/artifacts", a.requireScope(read, a.listSpaceArtifacts))

	// --- Lists ---
	r.Get("/api/artifacts", a.requireScope(read, a.listFeed))
	r.Get("/api/me/artifacts", a.requireScope(read, a.myArtifacts))
	r.Get("/api/me/shared", a.requireScope(read, a.sharedWithMe))
	r.Get("/api/s/{space}/{slug}", a.requireScope(read, a.resolvePath))

	// --- One artifact ---
	r.Post("/api/artifacts", a.requireScope(write, a.createArtifact))
	r.Get("/api/artifacts/{id}", a.requireScope(read, a.withArtifact(storage.RoleViewer, a.getArtifact)))
	r.Patch("/api/artifacts/{id}", a.requireScope(write, a.withArtifact(storage.RoleOwner, a.updateMeta)))
	r.Delete("/api/artifacts/{id}", a.humanOnly(a.withArtifact(storage.RoleOwner, a.deleteArtifact)))
	r.Get("/api/artifacts/{id}/versions", a.requireScope(read, a.withArtifact(storage.RoleViewer, a.listVersions)))
	r.Get("/api/artifacts/{id}/content-url", a.requireScope(read, a.withArtifact(storage.RoleViewer, a.contentURL)))
	r.Get("/api/artifacts/{id}/download", a.requireScope(read, a.withArtifact(storage.RoleEditor, a.download)))
	r.Get("/api/artifacts/{id}/forks", a.requireScope(read, a.withArtifact(storage.RoleViewer, a.listForks)))
	r.Get("/api/artifacts/{id}/stats", a.requireScope(read, a.withArtifact(storage.RoleViewer, a.artifactStats)))
	r.Get("/api/artifacts/{id}/events", a.requireScope(read, a.withArtifact(storage.RoleOwner, a.artifactEvents)))
	r.Post("/api/artifacts/{id}/commit", a.requireScope(write, a.withArtifact(storage.RoleEditor, a.commitVersion)))
	r.Post("/api/artifacts/{id}/deploy", a.requireScope(write, a.withArtifact(storage.RoleEditor, a.deployToArtifact)))
	r.Post("/api/artifacts/{id}/fork", a.requireScope(write, a.withArtifact(storage.RoleViewer, a.forkArtifact)))
	r.Post("/api/artifacts/{id}/move", a.requireScope(write, a.withArtifact(storage.RoleOwner, a.moveArtifact)))
	r.Post("/api/artifacts/{id}/likes", a.auth(a.withArtifact(storage.RoleViewer, a.like)))
	r.Delete("/api/artifacts/{id}/likes", a.auth(a.withArtifact(storage.RoleViewer, a.unlike)))

	// --- Sharing ---
	r.Get("/api/artifacts/{id}/shares", a.requireScope(read, a.withArtifact(storage.RoleOwner, a.listShares)))
	r.Post("/api/artifacts/{id}/shares", a.requireScope(write, a.withArtifact(storage.RoleOwner, a.addShare)))
	r.Delete("/api/artifacts/{id}/shares/{email}", a.requireScope(write, a.withArtifact(storage.RoleOwner, a.removeShare)))
	r.Post("/api/artifacts/{id}/link/rotate", a.requireScope(write, a.withArtifact(storage.RoleOwner, a.rotateLink)))

	// --- Review loop ---
	r.Get("/api/artifacts/{id}/threads", a.requireScope(cread, a.withArtifact(storage.RoleViewer, a.listThreads)))
	r.Post("/api/artifacts/{id}/threads", a.requireScope(cwrite, a.withArtifact(storage.RoleViewer, a.createThread)))
	r.Get("/api/artifacts/{id}/review", a.requireScope(cread, a.withArtifact(storage.RoleViewer, a.reviewDigest)))
	r.Post("/api/threads/{tid}/replies", a.requireScope(cwrite, a.withThread(a.replyThread)))
	r.Post("/api/threads/{tid}/status", a.requireScope(cwrite, a.withThread(a.setThreadStatus)))
	r.Delete("/api/threads/{tid}", a.requireScope(cwrite, a.withThread(a.deleteThread)))
	r.Patch("/api/comments/{cid}", a.requireScope(cwrite, a.editComment))
	r.Delete("/api/comments/{cid}", a.requireScope(cwrite, a.deleteComment))

	// --- Deploy ---
	r.Post("/api/blobs/missing", a.requireScope(write, a.missingBlobs))
	r.Put("/api/blobs/{sha}", a.requireScope(write, a.uploadBlob))
	r.Post("/api/deploy", a.requireScope(write, a.deploy))

	// --- Teams (orgs in the database) ---
	r.Get("/api/orgs", a.auth(a.listMyOrgs))
	r.Post("/api/orgs", a.humanOnly(a.createOrg))
	r.Get("/api/orgs/{id}/members", a.auth(a.withOrg("member", a.listOrgMembers)))
	r.Post("/api/orgs/{id}/members", a.humanOnly(a.withOrg("admin", a.inviteToOrg)))
	r.Delete("/api/orgs/{id}/members/{uid}", a.humanOnly(a.withOrg("admin", a.removeOrgMember)))
	r.Get("/api/orgs/{id}/artifacts", a.requireScope(read, a.withOrg("member", a.listOrgArtifacts)))

	// --- Keys ---
	r.Get("/api/keys", a.humanOnly(a.listKeys))
	r.Post("/api/keys", a.humanOnly(a.createKey))
	r.Delete("/api/keys/{id}", a.humanOnly(a.revokeKey))

	// --- Administration ---
	r.Get("/api/admin/stats", a.adminOnly(a.adminStats))
	r.Get("/api/admin/users", a.adminOnly(a.adminUsers))
	r.Post("/api/admin/users/{id}/role", a.adminOnly(a.adminSetRole))
	r.Get("/api/admin/events", a.adminOnly(a.adminEvents))
	r.Post("/api/admin/gc", a.adminOnly(a.adminGC))

	// --- Built-in chat with the person's own model (BYOK) ---
	r.Post("/api/chat", a.auth(a.aiChat))

	// --- Local agent bridge ---
	//
	// The bridge runs on the person's computer and connects outward: only the
	// request text and what the agent prints pass through here. Model
	// credentials stay on their machine.
	r.Get("/api/agent/status", a.auth(a.agentStatus))
	r.Get("/api/agent/poll", a.requireScope(write, a.agentPoll))
	r.Post("/api/agent/disconnect", a.requireScope(write, a.agentDisconnect))
	r.Post("/api/agent/runs/{id}/events", a.requireScope(write, a.agentEvents))
	r.Post("/api/agent/runs", a.humanOnly(a.agentRun))
	r.Get("/api/agent/runs/{id}/stream", a.humanOnly(a.agentStream))

	// --- MCP ---
	mcpServer := &mcp.Server{
		Store:      d.Store,
		Publisher:  a.publisher(),
		AppBaseURL: d.AppBaseURL,
		Logger:     d.Logger,
	}
	mcp.ServerVersion = Version
	r.Handle("/mcp", mcpServer.Handler())
	r.Get("/mcp/info", a.mcpInfo)

	// --- CLI installer ---
	r.Get("/api/install", a.installScript)
	r.Get("/api/install.ps1", a.installScriptPowerShell)
	r.Get("/api/cli/{os}/{arch}", a.serveCLI)

	return r
}

// deployToArtifact is /api/artifacts/{id}/deploy: the same logic as
// /api/deploy, with the destination already resolved from the path.
func (a *api) deployToArtifact(w http.ResponseWriter, r *http.Request, art *storage.Artifact, _ storage.Role) {
	a.deployInto(w, r, art)
}

// mcpInfo describes how to connect an MCP client. The interface's "Connect
// agents" page has ready-made snippets for each agent; this is the
// machine-readable version.
func (a *api) mcpInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     "falconia",
		"endpoint": a.deps.APIBaseURL + "/mcp",
		"transport": map[string]any{
			"type": "streamable-http",
			"auth": "Authorization: Bearer <api key>",
		},
		"hint":          "append ?agent=<name> to the endpoint to label what this client publishes",
		"create_key_at": a.deps.AppBaseURL + "/connect",
	})
}
