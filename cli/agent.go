package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// `falconia agent` connects YOUR agent to the web interface.
//
// The point is to take API keys out of the picture. People who code all day
// already pay for Claude Code, Codex or Gemini CLI, and that subscription
// covers running the official binary on their own machine. It does not cover —
// and the providers' terms say so in writing — passing those credentials
// through someone else's server.
//
// So credentials do not move. The official binary, installed and signed in by
// you, runs here. The server sends text requests, and what the agent prints
// goes back. For the person in the browser it feels like "sign in with Claude
// Code" (or Codex, or Gemini), but it does not break and gets nobody's account
// suspended. And because every teammate can connect whichever agent they pay
// for, the same artifacts are edited by all of them.

// runner describes how to launch an agent non-interactively.
type runner struct {
	Name string
	Bin  string
	// args builds the command line for one prompt.
	args func(prompt string, fullAccess bool) []string
	// newParser creates a line reader for a single run.
	//
	// It is a factory rather than a function because the reader must remember
	// what it already showed: Claude Code repeats the whole answer in its final
	// message, and without memory it would be printed twice.
	newParser func() lineParser
}

// lineParser turns one line of output into the event to send to the browser.
// It returns false when the line should not be shown.
type lineParser func(line string) (event, bool)

var runners = []runner{
	{
		Name: "claude",
		Bin:  "claude",
		args: func(prompt string, fullAccess bool) []string {
			mode := "acceptEdits"
			if fullAccess {
				mode = "bypassPermissions"
			}
			// stream-json gives one event per line instead of a wall of text
			// at the end: that is what lets the interface show the answer as
			// it forms.
			return []string{
				"-p", prompt,
				"--output-format", "stream-json",
				"--verbose",
				"--permission-mode", mode,
			}
		},
		newParser: claudeParser,
	},
	{
		Name: "codex",
		Bin:  "codex",
		args: func(prompt string, fullAccess bool) []string {
			a := []string{"exec", "--json", "--skip-git-repo-check"}
			if fullAccess {
				a = append(a, "--dangerously-bypass-approvals-and-sandbox")
			} else {
				a = append(a, "-s", "workspace-write")
			}
			return append(a, prompt)
		},
		newParser: codexParser,
	},
	{
		Name: "gemini",
		Bin:  "gemini",
		args: func(prompt string, fullAccess bool) []string {
			mode := "auto_edit"
			if fullAccess {
				mode = "yolo"
			}
			return []string{"-p", prompt, "--output-format", "stream-json", "--approval-mode", mode}
		},
		newParser: geminiParser,
	},
}

// agentIDs maps runner names to the identifiers Falconia records on versions.
var agentIDs = map[string]string{"claude": "claude-code", "codex": "codex", "gemini": "gemini-cli"}

type event struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Tool     string `json:"tool,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	// fromResult marks Claude Code's closing message, which repeats what was
	// already said. It stays internal: the server does not need to know.
	fromResult bool   `json:"-"`
	URL        string `json:"url,omitempty"`
}

func cmdAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	runnerName := fs.String("runner", "", "claude | codex | gemini")
	custom := fs.String("cmd", "", "command to run, with {prompt} where the request goes")
	dir := fs.String("dir", "", "the agent's working directory (default: this one)")
	fullAccess := fs.Bool("full-access", false, "let the agent run commands without asking")
	once := fs.Bool("once", false, "run a single request, then exit")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, cl, err := authed()
	if err != nil {
		return err
	}

	work := *dir
	if work == "" {
		if work, err = os.Getwd(); err != nil {
			return err
		}
	}
	if err := os.Chdir(work); err != nil {
		return fmt.Errorf("cannot enter %s: %w", work, err)
	}

	r, err := pickRunner(*runnerName, *custom)
	if err != nil {
		return err
	}

	host, _ := os.Hostname()
	fmt.Printf(`
  Agent connected to %s

  Engine       %s
  Directory    %s
  Permissions  %s

  Model credentials stay on this computer: the server only receives the
  request text and what the agent prints.

  Open the web interface and write in the chat. Ctrl-C to disconnect.

`, cfg.APIBaseURL, r.Name, work, permissionsLabel(*fullAccess))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Disconnecting cleanly keeps the interface from showing "connected" for
	// the minute and a half the expiry would take.
	defer func() {
		_ = cl.do("POST", "/api/agent/disconnect", map[string]string{}, nil)
	}()

	idle := 0
	for ctx.Err() == nil {
		job, err := pollOnce(ctx, cl, r.Name, host, work)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			idle++
			// The server may be restarting: retry with growing patience rather
			// than hammering it and flooding the terminal with errors.
			backoff := time.Duration(min(idle, 10)) * time.Second
			fmt.Fprintf(os.Stderr, "  connection lost (%v), retrying in %v\n", err, backoff)
			sleepCtx(ctx, backoff)
			continue
		}
		idle = 0
		if job == nil {
			continue // no request this round
		}

		fmt.Printf("  → %s\n", firstLine(job.Prompt, 72))
		if err := runJob(ctx, cl, r, job, *fullAccess); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		}
		if *once {
			break
		}
	}
	fmt.Println("\n  Agent disconnected.")
	return nil
}

func permissionsLabel(full bool) string {
	if full {
		return "full access (the agent runs commands without asking)"
	}
	return "file edits allowed, commands sandboxed — use --full-access to widen"
}

func pickRunner(name, custom string) (runner, error) {
	if custom != "" {
		if !strings.Contains(custom, "{prompt}") {
			return runner{}, fmt.Errorf("--cmd must contain {prompt}")
		}
		return customRunner(custom), nil
	}
	if name != "" {
		for _, r := range runners {
			if r.Name == name {
				if _, err := exec.LookPath(r.Bin); err != nil {
					return runner{}, fmt.Errorf("%s is not installed on this computer", r.Bin)
				}
				return r, nil
			}
		}
		return runner{}, fmt.Errorf("unknown engine: %s (available: claude, codex, gemini, or --cmd)", name)
	}
	for _, r := range runners {
		if _, err := exec.LookPath(r.Bin); err == nil {
			return r, nil
		}
	}
	return runner{}, fmt.Errorf(
		"none of claude, codex or gemini is installed.\n" +
			"Install one and sign in with your subscription, or pass --cmd 'your-agent {prompt}'")
}

// customRunner adapts any command: whoever uses another harness should not
// have to wait for it to be added here.
func customRunner(tpl string) runner {
	return runner{
		Name: "custom",
		Bin:  strings.Fields(tpl)[0],
		args: func(prompt string, _ bool) []string {
			parts := strings.Fields(tpl)
			out := make([]string, 0, len(parts)-1)
			for _, p := range parts[1:] {
				out = append(out, strings.ReplaceAll(p, "{prompt}", prompt))
			}
			return out
		},
		newParser: unknownParser,
	}
}

// unknownParser reads the output of a harness we do not know.
//
// Many print JSONL, and the formats we can read are different enough not to be
// confused. All are tried, falling back to raw text: whoever connects their own
// harness sees a readable conversation instead of a wall of JSON, without
// anyone having to add a case here.
func unknownParser() lineParser { return parseUnknownLine }

func parseUnknownLine(line string) (event, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return event{}, false
	}
	if strings.HasPrefix(trimmed, "{") {
		for _, parse := range []func(string) (event, bool){parseClaudeLine, parseCodexLine, parseGeminiLine} {
			if e, ok := parse(trimmed); ok && (e.Type != "text" || e.Text != trimmed+"\n") {
				return withURL(e), true
			}
		}
	}
	return withURL(event{Type: "text", Text: line + "\n"}), true
}

type jobPayload struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

func pollOnce(ctx context.Context, cl *Client, runnerName, host, dir string) (*jobPayload, error) {
	path := fmt.Sprintf("/api/agent/poll?wait=25&runner=%s&host=%s&dir=%s",
		urlValue(runnerName), urlValue(host), urlValue(dir))
	var job jobPayload
	err := cl.do("GET", path, nil, &job)
	if err != nil {
		return nil, err
	}
	if job.ID == "" {
		return nil, nil // 204: nothing to do
	}
	return &job, nil
}

// runJob runs the agent and sends back what it prints.
func runJob(ctx context.Context, cl *Client, r runner, job *jobPayload, fullAccess bool) error {
	out := newOutbox(cl, job.ID)
	defer out.close()

	out.push(event{Type: "status", Text: "starting " + r.Name})

	// A request cannot last forever: an agent stuck waiting for an approval
	// nobody can give would hold the queue indefinitely.
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(runCtx, r.Bin, r.args(job.Prompt, fullAccess)...)
	cmd.Stdin = nil
	// Whatever the agent publishes with `falconia deploy` during this run is
	// attributed to it, even if its own environment gives no other clue.
	agentID := agentIDs[r.Name]
	if agentID == "" {
		agentID = r.Bin
	}
	cmd.Env = append(os.Environ(), "FALCONIA_AGENT="+agentID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		out.push(event{Type: "error", Text: "cannot start " + r.Bin + ": " + err.Error()})
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scan := bufio.NewScanner(stdout)
		// A stream-json line carrying a whole file quickly exceeds the default
		// 64 KB buffer, and the scanner would stop mid-answer without a word.
		scan.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		parse := r.newParser()
		for scan.Scan() {
			if e, ok := parse(scan.Text()); ok {
				out.push(e)
			}
		}
	}()
	go func() {
		defer wg.Done()
		scan := bufio.NewScanner(stderr)
		scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if line != "" {
				fmt.Fprintln(os.Stderr, "    "+line)
			}
		}
	}()
	wg.Wait()

	err = cmd.Wait()
	code := cmd.ProcessState.ExitCode()
	if err != nil && code == 0 {
		code = 1
	}
	if runCtx.Err() == context.DeadlineExceeded {
		out.push(event{Type: "error", Text: "the agent did not finish within twenty minutes"})
		return nil
	}
	out.push(event{Type: "done", ExitCode: &code})
	fmt.Printf("  ✓ done (exit %d)\n", code)
	return nil
}

// outbox accumulates events and ships them in small batches.
//
// Without it, a long answer would become one HTTP request per line: the
// terminal would cope, the network in between much less so.
type outbox struct {
	cl    *Client
	jobID string

	mu      sync.Mutex
	pending []event
	timer   *time.Timer
	closed  bool
}

func newOutbox(cl *Client, jobID string) *outbox {
	return &outbox{cl: cl, jobID: jobID}
}

func (o *outbox) push(e event) {
	o.mu.Lock()
	o.pending = append(o.pending, e)
	n := len(o.pending)
	final := e.Type == "done" || e.Type == "error"
	if o.timer == nil && !final {
		o.timer = time.AfterFunc(150*time.Millisecond, o.flush)
	}
	o.mu.Unlock()

	if final || n >= 64 {
		o.flush()
	}
}

func (o *outbox) flush() {
	o.mu.Lock()
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
	batch := o.pending
	o.pending = nil
	o.mu.Unlock()

	if len(batch) == 0 {
		return
	}
	if err := o.cl.do("POST", "/api/agent/runs/"+o.jobID+"/events",
		map[string]any{"events": batch}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "    cannot send the output back: %v\n", err)
	}
}

func (o *outbox) close() {
	o.mu.Lock()
	o.closed = true
	o.mu.Unlock()
	o.flush()
}

// ---------------- Reading the agents' output ----------------

// urlRe spots an artifact address in the output, so the interface can offer a
// button instead of making the person hunt for the link in the text.
var urlRe = regexp.MustCompile(`https?://[^\s"'<>)]+/[sp]/[^\s"'<>)]+`)

// claudeParser reads Claude Code's stream-json format.
func claudeParser() lineParser {
	sawText := false
	return func(line string) (event, bool) {
		e, ok := parseClaudeLine(line)
		if !ok {
			return e, false
		}
		if e.Type == "text" {
			if e.fromResult {
				// The final message repeats everything already shown. It only
				// counts when the agent said nothing else.
				if sawText {
					return event{}, false
				}
			} else {
				sawText = true
			}
		}
		return e, true
	}
}

func parseClaudeLine(line string) (event, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return event{}, false
	}
	var msg struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil {
		// Not JSON: a log line, shown as is.
		return event{Type: "text", Text: line + "\n"}, true
	}

	switch msg.Type {
	case "assistant":
		var b strings.Builder
		tool := ""
		for _, c := range msg.Message.Content {
			switch c.Type {
			case "text":
				b.WriteString(c.Text)
			case "tool_use":
				tool = c.Name
			}
		}
		if tool != "" {
			return event{Type: "tool", Tool: tool}, true
		}
		if b.Len() == 0 {
			return event{}, false
		}
		return withURL(event{Type: "text", Text: b.String()}), true
	case "result":
		if msg.IsError {
			// An agent failure is not the model's answer: shown as text,
			// "session expired" would look like something the model said.
			return event{Type: "error", Text: firstNonEmptyStr(msg.Result, "the agent exited with an error")}, true
		}
		if msg.Result != "" {
			e := withURL(event{Type: "text", Text: msg.Result})
			e.fromResult = true
			return e, true
		}
	}
	return event{}, false
}

func codexParser() lineParser { return parseCodexLine }

// parseCodexLine reads `codex exec --json`. Two generations of the format are
// understood: the older {"msg": {...}} envelope and the newer item events
// ({"type": "item.completed", "item": {...}}).
func parseCodexLine(line string) (event, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return event{}, false
	}
	var msg struct {
		Type string `json:"type"`
		Msg  struct {
			Type    string   `json:"type"`
			Message string   `json:"message"`
			Text    string   `json:"text"`
			Command []string `json:"command"`
		} `json:"msg"`
		Item struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Command string `json:"command"`
			Name    string `json:"name"`
			Tool    string `json:"tool"`
		} `json:"item"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil {
		return event{Type: "text", Text: line + "\n"}, true
	}

	if msg.Item.Type != "" {
		switch msg.Item.Type {
		case "agent_message":
			if msg.Type != "item.completed" || strings.TrimSpace(msg.Item.Text) == "" {
				return event{}, false
			}
			return withURL(event{Type: "text", Text: msg.Item.Text + "\n"}), true
		case "command_execution":
			if msg.Type != "item.started" {
				return event{}, false
			}
			name := strings.Fields(msg.Item.Command)
			if len(name) == 0 {
				return event{Type: "tool", Tool: "command"}, true
			}
			return event{Type: "tool", Tool: name[0]}, true
		case "mcp_tool_call", "web_search", "file_change":
			if msg.Type != "item.started" {
				return event{}, false
			}
			return event{Type: "tool", Tool: firstNonEmptyStr(msg.Item.Tool, msg.Item.Name, msg.Item.Type)}, true
		}
		return event{}, false
	}
	if msg.Type == "error" || msg.Type == "turn.failed" {
		return event{Type: "error", Text: firstNonEmptyStr(msg.Message, "codex reported an error")}, true
	}

	kind := msg.Msg.Type
	if kind == "" {
		kind = msg.Type
	}
	switch {
	case strings.Contains(kind, "agent_message"), strings.Contains(kind, "agent_reasoning"):
		text := firstNonEmptyStr(msg.Msg.Message, msg.Msg.Text)
		if text == "" {
			return event{}, false
		}
		return withURL(event{Type: "text", Text: text}), true
	case strings.Contains(kind, "exec_command"), strings.Contains(kind, "tool"):
		name := "command"
		if len(msg.Msg.Command) > 0 {
			name = msg.Msg.Command[0]
		}
		return event{Type: "tool", Tool: name}, true
	}
	return event{}, false
}

func geminiParser() lineParser { return parseGeminiLine }

// parseGeminiLine reads Gemini CLI's stream-json output. Older releases only
// print plain text, which falls through as text.
func parseGeminiLine(line string) (event, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return event{}, false
	}
	var msg struct {
		Type     string `json:"type"`
		Role     string `json:"role"`
		Content  string `json:"content"`
		ToolName string `json:"tool_name"`
		Status   string `json:"status"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil {
		return withURL(event{Type: "text", Text: line + "\n"}), true
	}
	switch msg.Type {
	case "message":
		if msg.Role != "assistant" || msg.Content == "" {
			return event{}, false
		}
		return withURL(event{Type: "text", Text: msg.Content}), true
	case "tool_use":
		return event{Type: "tool", Tool: firstNonEmptyStr(msg.ToolName, "tool")}, true
	case "error":
		text := "gemini reported an error"
		if msg.Error != nil && msg.Error.Message != "" {
			text = msg.Error.Message
		}
		return event{Type: "error", Text: text}, true
	case "result":
		if msg.Status == "error" && msg.Error != nil {
			return event{Type: "error", Text: msg.Error.Message}, true
		}
	}
	return event{}, false
}

func withURL(e event) event {
	if m := urlRe.FindString(e.Text); m != "" {
		e.URL = m
	}
	return e
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func urlValue(s string) string {
	r := strings.NewReplacer(" ", "%20", "&", "%26", "?", "%3F", "#", "%23", "/", "%2F")
	return r.Replace(s)
}
