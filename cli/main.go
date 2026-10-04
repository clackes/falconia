// Command falconia publishes artifacts to a Falconia instance.
//
// It is meant to be run by an agent as much as by a person — Claude Code,
// Codex, Gemini CLI, Cursor, or anything else with a shell. Two choices follow
// from that and run through the whole program: every command prints a short,
// plain answer when it succeeds, and every error goes to stderr with a
// non-zero exit code, so an agent or a pipeline notices without having to
// interpret prose.
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "login":
		err = cmdLogin(args)
	case "logout":
		err = cmdLogout()
	case "whoami":
		err = cmdWhoami()
	case "spaces":
		err = cmdSpaces()
	case "list", "ls":
		err = cmdList(args)
	case "deploy", "publish", "push":
		err = cmdDeploy(args)
	case "pull", "clone":
		err = cmdPull(args)
	case "share":
		err = cmdShare(args)
	case "agent":
		err = cmdAgent(args)
	case "comments", "review":
		err = cmdComments(args)
	case "reply":
		err = cmdReply(args)
	case "resolve":
		err = cmdResolve(args)
	case "open", "url":
		err = cmdOpen(args)
	case "mcp":
		err = cmdMCP(args)
	case "version", "--version", "-v":
		fmt.Println("falconia " + version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		if e, ok := err.(*apiError); ok && e.Status == 401 {
			fmt.Fprintln(os.Stderr, "hint: run `falconia login`")
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`falconia — share artifacts made by any AI agent

  falconia login                        sign in from this computer
  falconia logout                       forget the session
  falconia whoami                       who you are, on which instance
  falconia spaces                       where you can publish

  falconia deploy <path> [options]      publish a file or a folder
      --space   <slug>                  where to publish (default: your space)
      --slug    <slug>                  stable identifier in the URL
      --title   <text>                  displayed title
      -m, --message <text>              what changed
      --visibility private|org|link     general access (link = anyone with the link)
      --agent   <name>                  label the version (auto-detected)
      --open                            print only the URL

  falconia pull <ref> [folder]          download an artifact's files to edit them
                                        with any agent, then deploy them back
  falconia share <ref> [options]        who can open it, like a Google Doc
      --access private|org|link         general access
      --team-role viewer|editor         what the team can do with --access org
      --add <email> [--role editor]     give a person access
      --remove <email>                  revoke a person's access
      --rotate                          replace the secret link

  falconia list [--space s] [--agent a] list the artifacts you can open
  falconia comments <ref> [--all]       read the comments to address
  falconia reply <thread-id> <text>     reply to a thread
  falconia resolve <thread-id>          mark a thread as resolved
  falconia open <ref>                   print an artifact's URL
  falconia mcp                          how to connect an MCP client

  falconia agent [options]              connect YOUR agent to the web interface
      --runner claude|codex|gemini      which engine (default: the first found)
      --cmd '<command> {prompt}'        any other harness
      --dir <folder>                    where the agent works
      --full-access                     let it run commands without asking

<ref> is "space/slug", a UUID, or any Falconia URL (including share links).

Environment:
  FALCONIA_API_URL     instance to use, overrides the configuration
  FALCONIA_TOKEN       API key, for CI and agents
  FALCONIA_AGENT       agent name to record on what you publish
  FALCONIA_CONFIG_DIR  where to keep the configuration (default ~/.falconia)
`)
}

// ---------------- Commands ----------------

func cmdLogout() error {
	if err := clearToken(); err != nil {
		return err
	}
	fmt.Println("Signed out.")
	return nil
}

func cmdWhoami() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := cfg.requireToken(); err != nil {
		return err
	}
	var out struct {
		User struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
			IsAdmin     bool   `json:"is_admin"`
		} `json:"user"`
	}
	if err := newClient(cfg).do("GET", "/api/auth/me", nil, &out); err != nil {
		return err
	}
	role := ""
	if out.User.IsAdmin {
		role = "  (instance admin)"
	}
	fmt.Printf("%s <%s>%s\n%s\n", out.User.DisplayName, out.User.Email, role, cfg.APIBaseURL)
	if a := detectAgent(); a != "" {
		fmt.Printf("agent: %s\n", a)
	}
	return nil
}

func cmdSpaces() error {
	_, cl, err := authed()
	if err != nil {
		return err
	}
	var out struct {
		Spaces []struct {
			Slug   string `json:"slug"`
			Name   string `json:"name"`
			Kind   string `json:"kind"`
			MyRole string `json:"my_role"`
		} `json:"spaces"`
	}
	if err := cl.do("GET", "/api/spaces", nil, &out); err != nil {
		return err
	}
	for _, s := range out.Spaces {
		kind := "personal"
		if s.Kind == "org" {
			kind = "team"
		}
		fmt.Printf("%-24s %-10s %s\n", s.Slug, kind, s.Name)
	}
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	space := fs.String("space", "", "only this space")
	query := fs.String("q", "", "search title and description")
	agent := fs.String("agent", "", "only artifacts an agent published to (claude-code, codex, ...)")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	_, cl, err := authed()
	if err != nil {
		return err
	}
	q := url.Values{"limit": {"100"}}
	if *space != "" {
		q.Set("space", *space)
	}
	if *query != "" {
		q.Set("q", *query)
	}
	if *agent != "" {
		q.Set("agent", *agent)
	}
	var out struct {
		Artifacts []struct {
			Title       string   `json:"title"`
			Path        string   `json:"path"`
			Visibility  string   `json:"visibility"`
			OpenThreads int      `json:"open_threads"`
			Version     int      `json:"latest_version"`
			Agents      []string `json:"agents"`
		} `json:"artifacts"`
	}
	if err := cl.do("GET", "/api/artifacts?"+q.Encode(), nil, &out); err != nil {
		return err
	}
	if len(out.Artifacts) == 0 {
		fmt.Println("No artifacts.")
		return nil
	}
	for _, a := range out.Artifacts {
		extra := ""
		if len(a.Agents) > 0 {
			extra += "  [" + strings.Join(a.Agents, ", ") + "]"
		}
		if a.OpenThreads > 0 {
			extra += fmt.Sprintf("  %d to fix", a.OpenThreads)
		}
		fmt.Printf("%-40s v%-4d %-8s %s%s\n",
			strings.TrimPrefix(a.Path, "/s/"), a.Version, a.Visibility, a.Title, extra)
	}
	return nil
}

func cmdComments(args []string) error {
	fs := flag.NewFlagSet("comments", flag.ContinueOnError)
	all := fs.Bool("all", false, "include resolved threads too")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("which artifact? falconia comments <space/slug>")
	}
	cfg, cl, err := authed()
	if err != nil {
		return err
	}
	ref, err := resolveRef(cl, cfg, rest[0])
	if err != nil {
		return err
	}
	if *all {
		// The digest only shows open threads: to see everything, go through
		// the full list, formatted here.
		return printAllThreads(cl, ref)
	}
	text, err := cl.getText("/api/artifacts/" + ref + "/review")
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

type threadOut struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	PagePath string `json:"page_path"`
	Anchor   *struct {
		Selector string `json:"selector"`
		Quote    string `json:"quote"`
	} `json:"anchor"`
	Comments []struct {
		UserName string `json:"user_name"`
		Agent    string `json:"agent"`
		Body     string `json:"body"`
	} `json:"comments"`
}

func printAllThreads(cl *Client, ref string) error {
	var out struct {
		Threads []threadOut `json:"threads"`
	}
	if err := cl.do("GET", "/api/artifacts/"+ref+"/threads", nil, &out); err != nil {
		return err
	}
	if len(out.Threads) == 0 {
		fmt.Println("No comments.")
		return nil
	}
	for _, t := range out.Threads {
		fmt.Printf("[%s] %s  (%s)\n", t.Status, t.ID, t.PagePath)
		if t.Anchor != nil && t.Anchor.Quote != "" {
			fmt.Printf("    \"%s\"\n", t.Anchor.Quote)
		}
		for _, c := range t.Comments {
			who := c.UserName
			if c.Agent != "" {
				who += " (via " + c.Agent + ")"
			}
			fmt.Printf("    %s: %s\n", who, c.Body)
		}
		fmt.Println()
	}
	return nil
}

func cmdReply(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: falconia reply <thread-id> <text>")
	}
	_, cl, err := authed()
	if err != nil {
		return err
	}
	body := strings.Join(args[1:], " ")
	if err := cl.do("POST", "/api/threads/"+args[0]+"/replies",
		map[string]string{"body": body}, nil); err != nil {
		return err
	}
	fmt.Println("Reply sent.")
	return nil
}

func cmdResolve(args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	reopen := fs.Bool("reopen", false, "reopen instead of resolving")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: falconia resolve <thread-id>")
	}
	_, cl, err := authed()
	if err != nil {
		return err
	}
	for _, id := range rest {
		if err := cl.do("POST", "/api/threads/"+id+"/status",
			map[string]bool{"resolved": !*reopen}, nil); err != nil {
			return err
		}
	}
	if *reopen {
		fmt.Println("Reopened.")
	} else {
		fmt.Println("Resolved.")
	}
	return nil
}

func cmdOpen(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: falconia open <space/slug>")
	}
	cfg, cl, err := authed()
	if err != nil {
		return err
	}
	ref, err := resolveRef(cl, cfg, args[0])
	if err != nil {
		return err
	}
	var out struct {
		Artifact struct {
			Path     string `json:"path"`
			LinkPath string `json:"link_path"`
		} `json:"artifact"`
	}
	if err := cl.do("GET", "/api/artifacts/"+ref, nil, &out); err != nil {
		return err
	}
	if out.Artifact.LinkPath != "" {
		fmt.Println(cfg.appBase() + out.Artifact.LinkPath)
		return nil
	}
	fmt.Println(cfg.appBase() + out.Artifact.Path)
	return nil
}

// cmdMCP explains how to connect an MCP client. The web interface's "Connect
// agents" page generates ready-to-paste configuration for each agent; this is
// the terminal version.
func cmdMCP(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	endpoint := cfg.APIBaseURL + "/mcp"
	fmt.Printf(`MCP endpoint:  %s
Transport:     streamable HTTP
Auth header:   Authorization: Bearer <your key>

Create a key and copy a ready-made config for your agent at:
  %s/connect

Examples (replace $FALCONIA_TOKEN with your key):

  Claude Code   claude mcp add --transport http falconia "%s?agent=claude-code" \
                  --header "Authorization: Bearer $FALCONIA_TOKEN"

  Codex         # ~/.codex/config.toml
                [mcp_servers.falconia]
                url = "%s?agent=codex"
                bearer_token_env_var = "FALCONIA_TOKEN"

  Gemini CLI    gemini mcp add --transport http falconia "%s?agent=gemini-cli" \
                  --header "Authorization: Bearer $FALCONIA_TOKEN"
`, endpoint, cfg.appBase(), endpoint, endpoint, endpoint)
	return nil
}

// ---------------- Support ----------------

func authed() (*Config, *Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.requireToken(); err != nil {
		return nil, nil, err
	}
	return cfg, newClient(cfg), nil
}

// refToParam turns "space/slug" into the form the API expects in a path,
// where a slash would break the route.
func refToParam(ref string) string {
	ref = strings.Trim(strings.TrimPrefix(strings.TrimSpace(ref), "/s/"), "/")
	return strings.Replace(ref, "/", ":", 1)
}

// resolveRef accepts whatever an agent was handed — "space/slug", a UUID, an
// app URL, or a share link — and returns the API path parameter for it.
func resolveRef(cl *Client, cfg *Config, raw string) (string, error) {
	ref := strings.TrimSpace(raw)
	if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
		ref = u.Path
	}
	switch {
	case strings.HasPrefix(ref, "/p/"):
		token := strings.Trim(strings.TrimPrefix(ref, "/p/"), "/")
		var out struct {
			Artifact struct {
				ID string `json:"id"`
			} `json:"artifact"`
		}
		// Opening the link while signed in grants view access, like clicking
		// it in a browser would.
		if err := cl.do("GET", "/api/p/"+url.PathEscape(token), nil, &out); err != nil {
			return "", err
		}
		return out.Artifact.ID, nil
	case strings.HasPrefix(ref, "/a/"):
		return strings.Trim(strings.TrimPrefix(ref, "/a/"), "/"), nil
	}
	return refToParam(ref), nil
}
