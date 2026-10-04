/**
 * The agents Falconia knows by name.
 *
 * The backend records an identifier on every version and comment
 * ("claude-code", "codex", "chat:gpt-5"...). This file turns it into a label
 * and a color, and knows how to configure each agent's MCP client. Unknown
 * identifiers still render — with their own name and a neutral color — so a
 * brand-new agent shows up on day one.
 *
 * Colors are generic hues, not vendor branding.
 */

export type AgentInfo = { id: string; label: string; color: string }

const KNOWN: Record<string, { label: string; color: string }> = {
  'claude-code':    { label: 'Claude Code',    color: '#d97757' },
  'claude-desktop': { label: 'Claude',         color: '#d97757' },
  codex:            { label: 'Codex',          color: '#10a37f' },
  chatgpt:          { label: 'ChatGPT',        color: '#10a37f' },
  'gemini-cli':     { label: 'Gemini CLI',     color: '#4c8df6' },
  cursor:           { label: 'Cursor',         color: '#a3a3a3' },
  vscode:           { label: 'VS Code',        color: '#3b9fe8' },
  copilot:          { label: 'Copilot',        color: '#8b5cf6' },
  windsurf:         { label: 'Windsurf',       color: '#14b8a6' },
  opencode:         { label: 'opencode',       color: '#f59e0b' },
  zed:              { label: 'Zed',            color: '#60a5fa' },
  cline:            { label: 'Cline',          color: '#e879f9' },
  goose:            { label: 'Goose',          color: '#facc15' },
  continue:         { label: 'Continue',       color: '#94a3b8' },
  aider:            { label: 'Aider',          color: '#22c55e' },
  amp:              { label: 'Amp',            color: '#f43f5e' },
}

export function agentInfo(id: string): AgentInfo {
  if (!id) return { id, label: 'By hand', color: '#5b6478' }
  if (id.startsWith('chat:')) return { id, label: `Chat · ${id.slice(5)}`, color: '#c9a227' }
  const k = KNOWN[id]
  return k ? { id, ...k } : { id, label: id, color: '#7c8599' }
}

// ---------------- Connecting an agent ----------------

export type SetupTarget = {
  id: string
  label: string
  /** Where the snippet goes, in a few words. */
  where: string
  /** Language for display. */
  lang: 'bash' | 'json' | 'toml'
  snippet(mcpURL: string, key: string): string
  note?: string
}

const withAgent = (url: string, agent: string) => `${url}${url.includes('?') ? '&' : '?'}agent=${agent}`

/**
 * One entry per client. Each puts ?agent=<id> on the URL, so what that client
 * publishes is labeled correctly even when it does not identify itself.
 */
export const SETUP_TARGETS: SetupTarget[] = [
  {
    id: 'claude-code', label: 'Claude Code', where: 'Run in your terminal', lang: 'bash',
    snippet: (u, k) =>
      `claude mcp add --transport http --scope user falconia \\\n  "${withAgent(u, 'claude-code')}" \\\n  --header "Authorization: Bearer ${k}"`,
  },
  {
    id: 'codex', label: 'Codex', where: '~/.codex/config.toml', lang: 'toml',
    snippet: (u, k) =>
      `[mcp_servers.falconia]\nurl = "${withAgent(u, 'codex')}"\nhttp_headers = { "Authorization" = "Bearer ${k}" }`,
    note: 'Prefer keeping the key out of the file? Use bearer_token_env_var = "FALCONIA_TOKEN" instead of http_headers and export the variable.',
  },
  {
    id: 'gemini-cli', label: 'Gemini CLI', where: 'Run in your terminal', lang: 'bash',
    snippet: (u, k) =>
      `gemini mcp add --scope user --transport http falconia \\\n  "${withAgent(u, 'gemini-cli')}" \\\n  --header "Authorization: Bearer ${k}"`,
  },
  {
    id: 'cursor', label: 'Cursor', where: '~/.cursor/mcp.json', lang: 'json',
    snippet: (u, k) => JSON.stringify({
      mcpServers: { falconia: { url: withAgent(u, 'cursor'), headers: { Authorization: `Bearer ${k}` } } },
    }, null, 2),
  },
  {
    id: 'vscode', label: 'VS Code / Copilot', where: '.vscode/mcp.json (or your user mcp.json)', lang: 'json',
    snippet: (u, k) => JSON.stringify({
      servers: { falconia: { type: 'http', url: withAgent(u, 'vscode'), headers: { Authorization: `Bearer ${k}` } } },
    }, null, 2),
  },
  {
    id: 'windsurf', label: 'Windsurf', where: '~/.codeium/windsurf/mcp_config.json', lang: 'json',
    snippet: (u, k) => JSON.stringify({
      mcpServers: { falconia: { serverUrl: withAgent(u, 'windsurf'), headers: { Authorization: `Bearer ${k}` } } },
    }, null, 2),
  },
  {
    id: 'opencode', label: 'opencode', where: 'opencode.json', lang: 'json',
    snippet: (u, k) => JSON.stringify({
      $schema: 'https://opencode.ai/config.json',
      mcp: { falconia: { type: 'remote', url: withAgent(u, 'opencode'), headers: { Authorization: `Bearer ${k}` } } },
    }, null, 2),
  },
  {
    id: 'claude-desktop', label: 'Claude Desktop', where: 'claude_desktop_config.json', lang: 'json',
    snippet: (u, k) => JSON.stringify({
      mcpServers: {
        falconia: {
          command: 'npx',
          args: ['-y', 'mcp-remote', withAgent(u, 'claude-desktop'), '--header', `Authorization: Bearer ${k}`],
        },
      },
    }, null, 2),
    note: 'Claude Desktop reaches remote servers through the mcp-remote bridge, which needs Node.js.',
  },
]

/**
 * AGENTS.md block: works for every agent that reads project instructions
 * (Codex, Claude Code via CLAUDE.md, Gemini CLI via GEMINI.md, Cursor, ...),
 * through the CLI rather than MCP.
 */
export function agentsMd(apiBase: string): string {
  return `## Sharing artifacts with Falconia

Publish visual work (HTML pages, dashboards, reports, prototypes) to Falconia so
the team can open it with a link and comment on it.

- Install once: \`curl -fsSL ${apiBase}/api/install | sh && falconia login\`
- Publish a folder or file: \`falconia deploy ./dist --title "Q3 report" -m "what changed"\`
  (the same folder always updates the same artifact; use \`--visibility link\` for a public link)
- Continue someone else's artifact: \`falconia pull <space/slug or URL>\`, edit, then \`falconia deploy <folder>\`
- Read feedback before fixing: \`falconia comments <space/slug>\`; after deploying the fix: \`falconia resolve <thread-id>\`

Artifacts must be self-contained: no CDN scripts or network calls; inline everything.`
}
