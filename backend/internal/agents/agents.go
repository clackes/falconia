// Package agents identifies which AI agent performed an action.
//
// Falconia is deliberately agent-agnostic: an artifact may be created by
// Claude Code, iterated on by Codex a week later, and fixed by Gemini CLI after
// that. Recording *which* agent produced each version (and wrote each comment)
// is what lets people see that history, and it is what makes switching
// subscriptions painless: nothing about an artifact belongs to the agent that
// created it.
//
// The agent is a hint, never a credential. It comes from places the caller
// controls — an MCP client's self-reported name, a query parameter, a header
// set by the CLI — so it is used for display and audit only, never for an
// authorization decision.
package agents

import (
	"context"
	"strings"
)

// Canonical identifiers. Anything else is kept as a sanitized free-form id so
// that a new agent shows up with its own name instead of disappearing.
const (
	ClaudeCode    = "claude-code"
	ClaudeDesktop = "claude-desktop"
	Codex         = "codex"
	GeminiCLI     = "gemini-cli"
	Cursor        = "cursor"
	VSCode        = "vscode"
	Windsurf      = "windsurf"
	OpenCode      = "opencode"
	Zed           = "zed"
	Cline         = "cline"
	Goose         = "goose"
	ChatGPT       = "chatgpt"
	Continue      = "continue"
	Copilot       = "copilot"
	Aider         = "aider"
	Amp           = "amp"
)

// aliases maps lower-cased substrings of client names, user agents and
// environment hints to canonical ids. Order matters: the first match wins, so
// more specific patterns come before generic ones ("claude-code" before
// "claude").
var aliases = []struct{ needle, id string }{
	{"claude-code", ClaudeCode},
	{"claude code", ClaudeCode},
	{"claudecode", ClaudeCode},
	{"claude-desktop", ClaudeDesktop},
	{"claude desktop", ClaudeDesktop},
	{"claude-ai", ClaudeDesktop},
	{"claude.ai", ClaudeDesktop},
	{"codex", Codex},
	{"gemini", GeminiCLI},
	{"cursor", Cursor},
	{"windsurf", Windsurf},
	{"codeium", Windsurf},
	{"opencode", OpenCode},
	{"zed", Zed},
	{"cline", Cline},
	{"roo-code", Cline},
	{"goose", Goose},
	{"chatgpt", ChatGPT},
	{"openai-mcp", ChatGPT},
	{"continue", Continue},
	{"copilot", Copilot},
	{"visual studio code", VSCode},
	{"vscode", VSCode},
	{"aider", Aider},
	{"amp", Amp},
	{"claude", ClaudeDesktop},
}

// Normalize turns whatever an agent calls itself into a stable identifier.
//
// It returns "" for empty input. Unknown names are reduced to [a-z0-9-] and
// capped in length, so a client cannot inject markup or an endless string into
// every page that shows a version history.
func Normalize(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	// A prefixed id ("chat:gpt-4o") is produced by Falconia itself and is kept
	// as is, apart from sanitizing.
	if strings.HasPrefix(s, "chat:") {
		return "chat:" + sanitize(strings.TrimPrefix(s, "chat:"), 48)
	}
	if id := match(s, 0); id != "" {
		return id
	}
	return sanitize(s, 32)
}

// match finds the first alias in s. Multi-word aliases ("claude-code") match
// as substrings; single-word aliases must start a token, so "amp" matches
// "amp-cli" but not "example".
func match(s string, minLen int) string {
	tokens := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	for _, a := range aliases {
		if len(a.needle) < minLen {
			continue
		}
		if strings.ContainsAny(a.needle, " -.") {
			if strings.Contains(s, a.needle) {
				return a.id
			}
			continue
		}
		for _, t := range tokens {
			if strings.HasPrefix(t, a.needle) {
				return a.id
			}
		}
	}
	return ""
}

// FromUserAgent recognizes an agent from an HTTP User-Agent header. It only
// returns known agents: a generic "node" or "python-requests" says nothing
// about who is driving it.
func FromUserAgent(ua string) string {
	// "zed" and "amp" are too short to trust inside an arbitrary user agent
	// string, which often carries a whole browser or runtime description.
	return match(strings.ToLower(ua), 5)
}

func sanitize(s string, max int) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
		if b.Len() >= max {
			break
		}
	}
	return strings.Trim(b.String(), "-.")
}

type ctxKey struct{}

// With stores the agent in the context. Storage reads it back when it writes
// versions, comments and events, so the identifier does not have to be
// threaded through every function signature in between.
func With(ctx context.Context, agent string) context.Context {
	if agent == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, agent)
}

// From returns the agent stored in the context, or "" for a human.
func From(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
