package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	base  string
	token string
	http  *http.Client
	// via tells a manual deploy from a pipeline's in the audit trail. It is
	// inferred from the variables every CI exports.
	via string
	// agent is the AI agent driving this CLI, if any: see detectAgent.
	agent string
}

func newClient(c *Config) *Client {
	return &Client{
		base:  c.APIBaseURL,
		token: c.Token,
		http:  &http.Client{Timeout: 120 * time.Second},
		via:   detectVia(),
		agent: detectAgent(),
	}
}

// detectVia recognizes the common CI systems from the variable each exports.
func detectVia() string {
	for _, k := range []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "JENKINS_URL", "CIRCLECI"} {
		if v := os.Getenv(k); v != "" && v != "false" && v != "0" {
			return "ci"
		}
	}
	return "cli"
}

// detectAgent works out which coding agent launched this command.
//
// Agents run shell commands in a subprocess, and most leave a recognizable
// variable in its environment. That is enough to label each version with the
// agent that published it — which is how a team sees an artifact passing from
// Claude Code to Codex to Gemini CLI. The label is cosmetic: a wrong guess
// costs nothing, and FALCONIA_AGENT or --agent always wins.
func detectAgent() string {
	if v := env("AGENT"); v != "" {
		return v
	}
	checks := []struct{ variable, agent string }{
		{"CLAUDECODE", "claude-code"},
		{"CLAUDE_CODE_ENTRYPOINT", "claude-code"},
		{"GEMINI_CLI", "gemini-cli"},
		{"CODEX_SANDBOX", "codex"},
		{"CODEX_SANDBOX_NETWORK_DISABLED", "codex"},
		{"CURSOR_AGENT", "cursor"},
		{"OPENCODE", "opencode"},
		{"AIDER_MODEL", "aider"},
	}
	for _, c := range checks {
		if v := os.Getenv(c.variable); v != "" && v != "0" && v != "false" {
			return c.agent
		}
	}
	return ""
}

type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func (c *Client) do(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.decorate(req)
	return c.send(req, out)
}

// putRaw uploads a blob's bytes.
func (c *Client) putRaw(path string, r io.Reader, size int64, out any) error {
	req, err := http.NewRequest(http.MethodPut, c.base+path, r)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	c.decorate(req)
	return c.send(req, out)
}

func (c *Client) decorate(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("X-Falconia-Via", c.via)
	if c.agent != "" {
		req.Header.Set("X-Falconia-Agent", c.agent)
	}
	req.Header.Set("User-Agent", "falconia-cli/"+version)
}

func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	// The body is read in any case: it is needed both for the result and the
	// error message, and leaving it half-read would waste the connection.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode >= 400 {
		return errorFrom(resp, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("unreadable answer from %s: %w", req.URL.Path, err)
		}
	}
	return nil
}

func errorFrom(resp *http.Response, raw []byte) error {
	msg := strings.TrimSpace(string(raw))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	if msg == "" {
		msg = resp.Status
	}
	return &apiError{Status: resp.StatusCode, Msg: msg}
}

// getText downloads a text answer, like the comments digest.
func (c *Client) getText(path string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return "", err
	}
	c.decorate(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 400 {
		return "", errorFrom(resp, raw)
	}
	return string(raw), nil
}

// download streams a binary answer into w and returns its headers.
func (c *Client) download(path string, w io.Writer) (http.Header, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, errorFrom(resp, raw)
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return nil, err
	}
	return resp.Header, nil
}
