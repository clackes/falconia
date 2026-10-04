package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/clackes/falconia/backend/internal/agents"
	"github.com/clackes/falconia/backend/internal/storage"
	"github.com/clackes/falconia/backend/pkg/auth"
)

// securityHeaders protects the application origin.
//
// Artifacts never pass through here: they live on another origin, served by
// the content package. These headers only concern the interface, which is why
// they can be this strict.
func securityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			// credentialless rather than require-corp: the interface loads fonts
			// and images from third parties that do not send CORP, and
			// require-corp would block them without adding anything against the
			// real risk, which is the artifact, and that already lives on
			// another origin.
			h.Set("Cross-Origin-Embedder-Policy", "credentialless")
			h.Set("Cross-Origin-Resource-Policy", "same-site")
			next.ServeHTTP(w, r)
		})
	}
}

// corsMW opens the API to the interface origin.
//
// In development Vite runs on a different port from the backend; in production
// both sit behind the same nginx and this middleware does nothing. The content
// origin is deliberately absent: artifacts must not be able to call the API.
func corsMW(appOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			o := r.Header.Get("Origin")
			if o != "" && (o == appOrigin || isLocalhost(o)) {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", o)
				h.Add("Vary", "Origin")
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				h.Set("Access-Control-Allow-Headers",
					"Content-Type,Authorization,X-Provider-Key,X-Falconia-Via,X-Falconia-Agent,X-SFA-Via")
				h.Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isLocalhost(o string) bool {
	return strings.HasPrefix(o, "http://localhost:") || strings.HasPrefix(o, "http://127.0.0.1:")
}

// ---------------- Identity ----------------

// identity is who is making the request, and with what.
//
// The difference between a browser session and a key is not bureaucracy: a key
// lives in an agent's configuration and in pipeline secrets, places where far
// more people can see it than will ever see its owner's password. Hence the
// rule that runs through this whole file: a key can do less than the person
// who created it.
type identity struct {
	User *storage.User
	Key  *storage.APIKey // nil for a browser session
}

func (i *identity) isKey() bool { return i.Key != nil }

// via tells the audit trail where the action came from.
func (i *identity) via(r *http.Request) string {
	if i.Key == nil {
		return "web"
	}
	// An MCP client and a pipeline use keys of the same type, and only they
	// can tell themselves apart: the header is a hint, not a credential, and it
	// only ends up in the log.
	hint := r.Header.Get("X-Falconia-Via")
	if hint == "" {
		hint = r.Header.Get("X-SFA-Via") // CLIs released before the rename
	}
	switch hint {
	case "cli", "ci", "mcp", "api":
		return hint
	}
	switch i.Key.Kind {
	case "ci":
		return "ci"
	case "mcp":
		return "mcp"
	default:
		return "cli"
	}
}

// agentOf works out which AI agent is behind a request made with a key.
//
// The CLI sends X-Falconia-Agent after looking at its environment (Claude Code,
// Codex and Gemini CLI each leave a recognizable trace in the shell they
// spawn). Failing that, some agents identify themselves in the User-Agent. A
// browser session is a human and has no agent.
func agentOf(id *identity, r *http.Request) string {
	if id == nil || !id.isKey() {
		return ""
	}
	if a := agents.Normalize(r.Header.Get("X-Falconia-Agent")); a != "" {
		return a
	}
	return agents.FromUserAgent(r.Header.Get("User-Agent"))
}

type ctxKey int

const ctxIdentity ctxKey = 1

// auth accepts both a browser JWT and an API key, and puts the identity — and
// the agent, if any — in the context.
func (a *api) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := a.identify(r)
		if err != nil || id == nil {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := context.WithValue(r.Context(), ctxIdentity, id)
		ctx = agents.With(ctx, agentOf(id, r))
		next(w, r.WithContext(ctx))
	}
}

// requireScope demands a specific scope from keys. A browser session always
// has all of its user's permissions and passes.
func (a *api) requireScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		id := identityFrom(r)
		if id.isKey() && !id.Key.HasScope(scope) {
			writeErr(w, http.StatusForbidden, "this key lacks the scope "+scope)
			return
		}
		next(w, r)
	})
}

// humanOnly closes an operation to keys.
//
// It covers everything destructive (deleting an artifact) and everything that
// widens privileges (issuing keys, administering the instance). A stolen key
// must be able to do neither.
func (a *api) humanOnly(next http.HandlerFunc) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request) {
		if identityFrom(r).isKey() {
			writeErr(w, http.StatusForbidden,
				"this operation needs a signed-in session: API keys cannot perform it")
			return
		}
		next(w, r)
	})
}

func (a *api) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return a.humanOnly(func(w http.ResponseWriter, r *http.Request) {
		if !identityFrom(r).User.IsAdmin {
			writeErr(w, http.StatusForbidden, "reserved to instance administrators")
			return
		}
		next(w, r)
	})
}

// identify resolves the bearer token, JWT or API key alike.
func (a *api) identify(r *http.Request) (*identity, error) {
	tok := extractBearer(r)
	if tok == "" {
		return nil, nil
	}
	if storage.IsAPIKey(tok) {
		u, k, err := a.deps.Store.LookupAPIKey(r.Context(), tok)
		if err != nil {
			return nil, err
		}
		return &identity{User: u, Key: k}, nil
	}
	claims, err := auth.Parse(a.deps.JWTSecret, tok)
	if err != nil {
		return nil, err
	}
	u, err := a.deps.Store.GetUser(r.Context(), claims.UserID)
	if err != nil {
		return nil, err
	}
	return &identity{User: u}, nil
}

func identityFrom(r *http.Request) *identity {
	v, _ := r.Context().Value(ctxIdentity).(*identity)
	return v
}

// maybeIdentity reads the identity without requiring one, for endpoints that
// behave differently depending on whether the caller is signed in.
func (a *api) maybeIdentity(r *http.Request) *identity {
	if id := identityFrom(r); id != nil {
		return id
	}
	id, err := a.identify(r)
	if err != nil {
		return nil
	}
	return id
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func userIDOf(id *identity) uuid.UUID {
	if id == nil || id.User == nil {
		return uuid.Nil
	}
	return id.User.ID
}

func emailOf(id *identity) string {
	if id == nil || id.User == nil {
		return ""
	}
	return id.User.Email
}
