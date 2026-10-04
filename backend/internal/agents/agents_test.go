package agents

import (
	"context"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":                      "",
		"claude-code":           ClaudeCode,
		"Claude Code":           ClaudeCode,
		"claude-ai":             ClaudeDesktop,
		"codex-mcp-client":      Codex,
		"Codex":                 Codex,
		"gemini-cli-mcp-client": GeminiCLI,
		"cursor-vscode":         Cursor,
		"Visual Studio Code":    VSCode,
		"windsurf-client":       Windsurf,
		"opencode":              OpenCode,
		"chat:gpt-4o":           "chat:gpt-4o",
		"chat:<script>":         "chat:script",
		"My Custom Agent!!":     "my-custom-agent",
		"example-agent":         "example-agent", // must not match "amp"
		"amp-cli":               Amp,
		"GeminiCLI":             GeminiCLI,
		"<img src=x onerror=1>": "img-src-x-onerror-1",
		"a-very-long-agent-name-that-goes-on-and-on-forever": "a-very-long-agent-name-that-goes",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromUserAgent(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"node":                         "",
		"python-httpx/0.27":            "",
		"claude-code/2.1.0 (external)": ClaudeCode,
		"codex_cli_rs/0.40.0 (Mac OS)": Codex,
		"GeminiCLI/0.9.0 (linux; x64)": GeminiCLI,
		"Mozilla/5.0 zed-industries":   "", // "zed" is too short to trust in a UA
	}
	for in, want := range cases {
		if got := FromUserAgent(in); got != want {
			t.Errorf("FromUserAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContext(t *testing.T) {
	ctx := context.Background()
	if From(ctx) != "" {
		t.Fatal("empty context must have no agent")
	}
	if From(With(ctx, "")) != "" {
		t.Fatal("With(\"\") must not set an agent")
	}
	if got := From(With(ctx, Codex)); got != Codex {
		t.Fatalf("From = %q", got)
	}
}
