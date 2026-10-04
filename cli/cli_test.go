package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAgent(t *testing.T) {
	for _, k := range []string{"FALCONIA_AGENT", "SFA_AGENT", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "GEMINI_CLI",
		"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "CURSOR_AGENT", "OPENCODE", "AIDER_MODEL"} {
		t.Setenv(k, "")
	}
	if got := detectAgent(); got != "" {
		t.Fatalf("no agent expected, got %q", got)
	}
	t.Setenv("CODEX_SANDBOX", "seatbelt")
	if got := detectAgent(); got != "codex" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("FALCONIA_AGENT", "my-bot")
	if got := detectAgent(); got != "my-bot" {
		t.Fatalf("explicit agent must win, got %q", got)
	}
}

func TestParseCodexLine(t *testing.T) {
	cases := []struct {
		line, typ, text, tool string
		ok                    bool
	}{
		{`{"type":"item.completed","item":{"type":"agent_message","text":"Published!"}}`, "text", "Published!\n", "", true},
		{`{"type":"item.started","item":{"type":"command_execution","command":"falconia deploy ."}}`, "tool", "", "falconia", true},
		{`{"type":"item.started","item":{"type":"agent_message","text":"partial"}}`, "", "", "", false},
		{`{"msg":{"type":"agent_message","message":"old format"}}`, "text", "old format", "", true},
		{`{"type":"turn.failed","message":"boom"}`, "error", "boom", "", true},
	}
	for _, c := range cases {
		e, ok := parseCodexLine(c.line)
		if ok != c.ok || (ok && (e.Type != c.typ || e.Text != c.text || e.Tool != c.tool)) {
			t.Errorf("parseCodexLine(%s) = %+v, %v", c.line, e, ok)
		}
	}
}

func TestParseGeminiLine(t *testing.T) {
	e, ok := parseGeminiLine(`{"type":"message","role":"assistant","content":"see https://x.example/p/abc"}`)
	if !ok || e.Type != "text" || e.URL != "https://x.example/p/abc" {
		t.Fatalf("assistant message: %+v %v", e, ok)
	}
	if _, ok := parseGeminiLine(`{"type":"message","role":"user","content":"hi"}`); ok {
		t.Fatal("user echo must be hidden")
	}
	e, _ = parseGeminiLine(`{"type":"tool_use","tool_name":"run_shell_command"}`)
	if e.Type != "tool" || e.Tool != "run_shell_command" {
		t.Fatalf("tool: %+v", e)
	}
	e, _ = parseGeminiLine("plain text output")
	if e.Type != "text" {
		t.Fatalf("plain text: %+v", e)
	}
}

func TestExtractZipRefusesZipSlip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../evil.txt")
	_, _ = w.Write([]byte("nope"))
	_ = zw.Close()

	dir := t.TempDir()
	if _, err := extractZip(buf.Bytes(), filepath.Join(dir, "out")); err == nil {
		t.Fatal("zip slip not refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Fatal("file written outside the target folder")
	}
}

func TestExtractZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"index.html": "<h1>hi</h1>", "assets/app.js": "1"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	dir := t.TempDir()
	n, err := extractZip(buf.Bytes(), dir)
	if err != nil || n != 2 {
		t.Fatalf("extract: %d %v", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "assets", "app.js")); string(b) != "1" {
		t.Fatal("nested file not extracted")
	}
}

func TestDeriveSlug(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "Q3 Report")
	_ = os.MkdirAll(filepath.Join(proj, "dist"), 0o755)
	if got := deriveSlug(filepath.Join(proj, "dist")); got != "q3-report" {
		t.Fatalf("dist should use the project folder, got %q", got)
	}
	if got := deriveSlug(filepath.Join(proj, "chart.html")); got != "chart" {
		t.Fatalf("file slug: %q", got)
	}
}

func TestRefToParam(t *testing.T) {
	cases := map[string]string{"team/roadmap": "team:roadmap", "/s/team/roadmap/": "team:roadmap", "uuid-x": "uuid-x"}
	for in, want := range cases {
		if got := refToParam(in); got != want {
			t.Errorf("refToParam(%q) = %q, want %q", in, got, want)
		}
	}
}
