package mcp

import (
	"net/http/httptest"
	"testing"
)

func TestNegotiate(t *testing.T) {
	for _, v := range supportedVersions {
		if negotiate(v) != v {
			t.Errorf("supported version %s not echoed", v)
		}
	}
	if negotiate("1999-01-01") != supportedVersions[0] {
		t.Error("unknown version must get the newest")
	}
	if negotiate("") != supportedVersions[0] {
		t.Error("missing version must get the newest")
	}
}

func TestSessionsRememberTheClient(t *testing.T) {
	var st sessionStore
	id := st.open("codex", "user-1")
	if st.agent(id) != "codex" {
		t.Fatal("session lost its agent")
	}
	if st.agent("unknown") != "" || st.agent("") != "" {
		t.Fatal("unknown sessions must have no agent")
	}
	st.forget(id)
	if st.agent(id) != "" {
		t.Fatal("forgotten session still answers")
	}
}

func TestAgentForPrefersExplicitHints(t *testing.T) {
	s := &Server{}
	sid := s.sessions.open("claude-code", "u")

	r := httptest.NewRequest("POST", "/mcp?agent=codex", nil)
	r.Header.Set("Mcp-Session-Id", sid)
	if got := s.agentFor(r); got != "codex" {
		t.Fatalf("query hint ignored: %q", got)
	}
	r = httptest.NewRequest("POST", "/mcp", nil)
	r.Header.Set("Mcp-Session-Id", sid)
	if got := s.agentFor(r); got != "claude-code" {
		t.Fatalf("session hint ignored: %q", got)
	}
	r = httptest.NewRequest("POST", "/mcp", nil)
	r.Header.Set("User-Agent", "GeminiCLI/1.0")
	if got := s.agentFor(r); got != "gemini-cli" {
		t.Fatalf("user agent hint ignored: %q", got)
	}
}
