# Connecting agents

Falconia works with any AI agent, through three doors:

1. **MCP** — for agents that speak the Model Context Protocol (Claude Code,
   Codex, Gemini CLI, Cursor, VS Code, Windsurf, opencode, Claude Desktop…).
2. **The CLI** — for anything that can run a shell command, including agents in
   CI.
3. **The local bridge** — to drive your own Claude Code / Codex / Gemini CLI
   from the web interface, on your subscription. See
   [local-agent.md](local-agent.md).

The fastest way is the **Connect agents** page of your instance: it creates a
key and fills it into a ready-to-paste configuration for each agent. This page
is the same information, for reference.

Throughout, replace `https://falconia.example.com` with your instance and
`$FALCONIA_TOKEN` with a key (`falconia_pat_…`).

## MCP

Endpoint: `https://falconia.example.com/mcp` — Streamable HTTP, bearer auth.

Append `?agent=<name>` to the URL so everything the client publishes is
labeled correctly. Most clients also identify themselves, so it is a
belt-and-braces measure.

### Claude Code

```bash
claude mcp add --transport http --scope user falconia \
  "https://falconia.example.com/mcp?agent=claude-code" \
  --header "Authorization: Bearer $FALCONIA_TOKEN"
```

### Codex

`~/.codex/config.toml`:

```toml
[mcp_servers.falconia]
url = "https://falconia.example.com/mcp?agent=codex"
bearer_token_env_var = "FALCONIA_TOKEN"
```

(or `http_headers = { "Authorization" = "Bearer …" }` to keep the key in the file)

### Gemini CLI

```bash
gemini mcp add --scope user --transport http falconia \
  "https://falconia.example.com/mcp?agent=gemini-cli" \
  --header "Authorization: Bearer $FALCONIA_TOKEN"
```

### Cursor

`~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "falconia": {
      "url": "https://falconia.example.com/mcp?agent=cursor",
      "headers": { "Authorization": "Bearer falconia_pat_…" }
    }
  }
}
```

### VS Code (Copilot agent mode)

`.vscode/mcp.json` or your user `mcp.json`:

```json
{
  "servers": {
    "falconia": {
      "type": "http",
      "url": "https://falconia.example.com/mcp?agent=vscode",
      "headers": { "Authorization": "Bearer falconia_pat_…" }
    }
  }
}
```

### Windsurf

`~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "falconia": {
      "serverUrl": "https://falconia.example.com/mcp?agent=windsurf",
      "headers": { "Authorization": "Bearer falconia_pat_…" }
    }
  }
}
```

### opencode

`opencode.json`:

```json
{
  "mcp": {
    "falconia": {
      "type": "remote",
      "url": "https://falconia.example.com/mcp?agent=opencode",
      "headers": { "Authorization": "Bearer falconia_pat_…" }
    }
  }
}
```

### Claude Desktop

Through the `mcp-remote` bridge (needs Node.js), in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "falconia": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://falconia.example.com/mcp?agent=claude-desktop",
               "--header", "Authorization: Bearer falconia_pat_…"]
    }
  }
}
```

### Anything else

Any MCP client that supports Streamable HTTP and custom headers works the same
way. If your client only supports stdio, put `mcp-remote` in between as shown
for Claude Desktop.

## MCP tools

| Tool | What it does |
| --- | --- |
| `list_artifacts` | what you can open; filter by `query`, `space`, `category`, `agent` |
| `get_artifact` | metadata, entry document source, file list — read this before editing |
| `get_file` | one text file of a bundle |
| `list_versions` | history, with the agent of each version |
| `create_artifact` | from `code` (one HTML document) or `files` (a bundle); returns the URL |
| `deploy_artifact` | new version: whole `code`, or only the changed `files`, plus `delete` |
| `update_artifact` | title, description, `visibility` (`private`/`org`/`link`), `team_role`… |
| `rotate_link` | replace the secret share link |
| `share_artifact` / `unshare_artifact` / `list_shares` | people with direct access |
| `fork_artifact`, `move_artifact` | copy, or move to another space |
| `list_threads` | open comments with file, selector and quoted text |
| `comment_artifact`, `reply_thread`, `resolve_thread` | the review loop |
| `artifact_stats`, `list_spaces`, `list_teams`, `list_categories` | |

Every tool accepts an artifact as a UUID, `space/slug`, or any Falconia URL —
including a `/p/` share link, which grants the key's owner view access the same
way clicking it would.

## The CLI

```bash
curl -fsSL https://falconia.example.com/api/install | sh   # Windows: irm …/api/install.ps1 | iex
falconia login                                              # or export FALCONIA_TOKEN=…

falconia deploy ./dist --title "Q3 report" --visibility link -m "first draft"
falconia pull growth/q3-report        # continue an artifact someone else (or another agent) made
falconia comments growth/q3-report    # read what people asked
falconia resolve <thread-id>
falconia share growth/q3-report --add ana@example.com --role editor
```

The CLI labels what it publishes with the agent that runs it. It recognizes:

| Agent | Detected from |
| --- | --- |
| Claude Code | `CLAUDECODE` |
| Codex | `CODEX_SANDBOX`, `CODEX_SANDBOX_NETWORK_DISABLED` |
| Gemini CLI | `GEMINI_CLI` |
| Cursor | `CURSOR_AGENT` |
| opencode | `OPENCODE` |

`FALCONIA_AGENT=<name>` or `--agent <name>` always wins.

### Project instructions

Paste this into `AGENTS.md` (Codex, Cursor, opencode, Amp…), `CLAUDE.md` or
`GEMINI.md` so every agent working in a repository knows the workflow. For a
fuller version, install [`skill/SKILL.md`](../skill/SKILL.md) as a skill
(Claude Code: `~/.claude/skills/falconia/SKILL.md`).

```markdown
## Sharing artifacts with Falconia

Publish visual work (HTML pages, dashboards, reports, prototypes) to Falconia so
the team can open it with a link and comment on it.

- Publish: `falconia deploy ./dist --title "Q3 report" -m "what changed"`
  (the same folder always updates the same artifact; `--visibility link` for a public link)
- Continue someone else's artifact: `falconia pull <space/slug or URL>`, edit, `falconia deploy <folder>`
- Before fixing: `falconia comments <space/slug>`; after deploying the fix: `falconia resolve <thread-id>`

Artifacts must be self-contained: no CDN scripts or network calls; inline everything.
```

## Switching agents

Nothing in Falconia belongs to the agent that made it. To move an artifact from
one agent to another:

- **Over MCP**: ask the new agent to `get_artifact` the URL, change it, and
  `deploy_artifact` — the same URL, the next version, labeled with the new
  agent.
- **With the CLI**: `falconia pull <url>`, let the new agent edit the folder,
  `falconia deploy <folder>`.
- **By hand**: download the zip from *Continue with… → Download*, and publish
  it back however you like.

The open comments come along: they are attached to the artifact, not to a
conversation with any one model.
