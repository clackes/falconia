// Package mcp exposes the instance's tools over the Model Context Protocol.
//
// Streamable HTTP transport, JSON responses, JSON-RPC 2.0. The implemented
// subset is what real clients need: initialize, ping, tools/list, tools/call,
// plus notifications and batches. It is tested against the clients people
// actually use — Claude Code, Codex, Gemini CLI, Cursor, VS Code, Windsurf,
// opencode — and the guiding rule is to be lenient in what it accepts: an
// agent that cannot connect is an agent locked out of its own artifacts.
//
// Authentication is an API key in the Authorization header. From there on
// every operation happens on behalf of whoever issued it, with the same limits
// that key has on the HTTP API: the tools live in internal/tools, and that,
// not this file, is where what an agent can do gets decided.
package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/clackes/falconia/backend/internal/agents"
	"github.com/clackes/falconia/backend/internal/publish"
	"github.com/clackes/falconia/backend/internal/storage"
	"github.com/clackes/falconia/backend/internal/tools"
)

// supportedVersions lists the protocol revisions this server speaks, newest
// first. A client asking for one of them gets it back; anything else gets the
// newest, and the client decides whether it can live with that.
var supportedVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// ServerVersion is reported in serverInfo.
var ServerVersion = "dev"

type Server struct {
	Store      *storage.Postgres
	Publisher  *publish.Publisher
	AppBaseURL string
	Logger     *slog.Logger

	sessions sessionStore
}

func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
		case http.MethodDelete:
			// A client ending its session. Nothing to clean up beyond the
			// session hint itself.
			s.sessions.forget(r.Header.Get("Mcp-Session-Id"))
			w.WriteHeader(http.StatusNoContent)
			return
		default:
			// GET would open a server-to-client SSE stream; this server never
			// initiates messages, and the spec says to answer 405.
			w.Header().Set("Allow", "POST, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		user, key, err := s.authenticate(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="falconia", error="invalid_token"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(rpcError(nil, -32001, "unauthorized: "+err.Error()+
				". Create a key in Falconia under Connect agents and send it as 'Authorization: Bearer <key>'."))
			return
		}

		body, err := readBody(w, r)
		if err != nil {
			writeJSON(w, http.StatusOK, rpcError(nil, -32700, "parse error"))
			return
		}

		ctx := agents.With(r.Context(), s.agentFor(r))
		trimmed := bytes.TrimSpace(body)

		// A batch (allowed by revision 2025-03-26).
		if len(trimmed) > 0 && trimmed[0] == '[' {
			var reqs []rpcReq
			if err := json.Unmarshal(trimmed, &reqs); err != nil {
				writeJSON(w, http.StatusOK, rpcError(nil, -32700, "parse error"))
				return
			}
			out := []any{}
			for _, req := range reqs {
				if resp, ok := s.handle(ctx, w, r, user, key, req); ok {
					out = append(out, resp)
				}
			}
			if len(out) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			writeJSON(w, http.StatusOK, out)
			return
		}

		var req rpcReq
		if err := json.Unmarshal(trimmed, &req); err != nil {
			writeJSON(w, http.StatusOK, rpcError(nil, -32700, "parse error"))
			return
		}
		resp, ok := s.handle(ctx, w, r, user, key, req)
		if !ok {
			// Notifications and responses get 202 and no body.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handle processes one JSON-RPC message. It returns false when the message
// expects no response (a notification).
func (s *Server) handle(ctx context.Context, w http.ResponseWriter, r *http.Request, user *storage.User, key *storage.APIKey, req rpcReq) (any, bool) {
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil, false
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"clientInfo"`
		}
		_ = json.Unmarshal(req.Params, &p)
		// The client's self-reported name is the most reliable agent hint
		// there is: remember it for the rest of the session.
		sid := s.sessions.open(agents.Normalize(p.ClientInfo.Name), user.ID.String())
		w.Header().Set("Mcp-Session-Id", sid)
		return rpcResult(req.ID, map[string]any{
			"protocolVersion": negotiate(p.ProtocolVersion),
			"serverInfo":      map[string]any{"name": "falconia", "title": "Falconia", "version": ServerVersion},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"instructions":    tools.Instructions,
		}), true
	case "ping":
		return rpcResult(req.ID, map[string]any{}), true
	case "tools/list":
		return rpcResult(req.ID, map[string]any{"tools": toolSchemas(key)}), true
	case "tools/call":
		return s.call(ctx, user, key, req), true
	case "resources/list":
		return rpcResult(req.ID, map[string]any{"resources": []any{}}), true
	case "prompts/list":
		return rpcResult(req.ID, map[string]any{"prompts": []any{}}), true
	default:
		return rpcError(req.ID, -32601, "unknown method: "+req.Method), true
	}
}

// agentFor works out which agent is calling, from the strongest hint to the
// weakest: an explicit ?agent= in the configured URL, the name the client gave
// at initialize, its User-Agent.
func (s *Server) agentFor(r *http.Request) string {
	if a := agents.Normalize(r.URL.Query().Get("agent")); a != "" {
		return a
	}
	if a := agents.Normalize(r.Header.Get("X-Falconia-Agent")); a != "" {
		return a
	}
	if a := s.sessions.agent(r.Header.Get("Mcp-Session-Id")); a != "" {
		return a
	}
	return agents.FromUserAgent(r.Header.Get("User-Agent"))
}

func negotiate(requested string) string {
	for _, v := range supportedVersions {
		if v == requested {
			return v
		}
	}
	return supportedVersions[0]
}

func (s *Server) authenticate(r *http.Request) (*storage.User, *storage.APIKey, error) {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return nil, nil, errors.New("missing API key")
	}
	u, k, err := s.Store.LookupAPIKey(r.Context(), strings.TrimSpace(h[7:]))
	if err != nil {
		return nil, nil, errors.New("invalid or expired API key")
	}
	return u, k, nil
}

// toolSchemas only shows the tools the key can actually use.
//
// Hiding them instead of letting them fail keeps the model from trying: a tool
// that is listed and then refuses every call wastes the agent's turns and
// protects nothing more.
func toolSchemas(key *storage.APIKey) []map[string]any {
	out := []map[string]any{}
	for _, d := range tools.All() {
		if !key.HasScope(tools.Scope(d.Name)) {
			continue
		}
		out = append(out, map[string]any{
			"name": d.Name, "description": d.Description, "inputSchema": d.InputSchema,
		})
	}
	return out
}

func (s *Server) call(ctx context.Context, user *storage.User, key *storage.APIKey, req rpcReq) any {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return rpcError(req.ID, -32602, "invalid params")
	}
	if !key.HasScope(tools.Scope(params.Name)) {
		return toolResult(req.ID, "this key lacks the scope "+tools.Scope(params.Name), true)
	}

	runner := &tools.Runner{Store: s.Store, Publisher: s.Publisher, AppBaseURL: s.AppBaseURL, User: user, Via: "mcp"}
	out, err := runner.Call(ctx, params.Name, params.Arguments)
	if err != nil {
		// A tool error goes back to the model as content with isError, not as
		// a JSON-RPC error: the model can read it and correct itself instead
		// of seeing the conversation break.
		s.Logger.Info("mcp tool error", "tool", params.Name, "agent", agents.From(ctx), "err", err)
		return toolResult(req.ID, err.Error(), true)
	}
	return toolResult(req.ID, out, false)
}

// ---------- Sessions ----------

// sessionStore remembers which client opened which session. It is a hint for
// attribution only — losing it on restart costs nothing but a label — so it
// lives in memory, bounded in size and age.
type sessionStore struct {
	mu   sync.Mutex
	byID map[string]session
}

type session struct {
	agent    string
	userID   string
	lastSeen time.Time
}

const (
	sessionTTL = 24 * time.Hour
	maxSession = 20000
)

func (st *sessionStore) open(agent, userID string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.byID == nil {
		st.byID = map[string]session{}
	}
	if len(st.byID) >= maxSession {
		cutoff := time.Now().Add(-sessionTTL)
		for k, v := range st.byID {
			if v.lastSeen.Before(cutoff) {
				delete(st.byID, k)
			}
		}
		// Still full: drop an arbitrary half rather than grow without bound.
		if len(st.byID) >= maxSession {
			n := 0
			for k := range st.byID {
				delete(st.byID, k)
				if n++; n >= maxSession/2 {
					break
				}
			}
		}
	}
	st.byID[id] = session{agent: agent, userID: userID, lastSeen: time.Now()}
	return id
}

func (st *sessionStore) agent(id string) string {
	if id == "" {
		return ""
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	sess, ok := st.byID[id]
	if !ok || time.Since(sess.lastSeen) > sessionTTL {
		return ""
	}
	sess.lastSeen = time.Now()
	st.byID[id] = sess
	return sess.agent
}

func (st *sessionStore) forget(id string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.byID, id)
}

// ---------- JSON-RPC ----------

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(http.MaxBytesReader(w, r.Body, 32<<20))
	return buf.Bytes(), err
}

func rpcResult(id json.RawMessage, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcError(id json.RawMessage, code int, msg string) map[string]any {
	var rid any = id
	if len(id) == 0 {
		rid = nil
	}
	return map[string]any{
		"jsonrpc": "2.0", "id": rid,
		"error": map[string]any{"code": code, "message": msg},
	}
}

func toolResult(id json.RawMessage, text string, isError bool) map[string]any {
	return rpcResult(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
