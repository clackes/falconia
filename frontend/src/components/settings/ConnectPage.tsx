import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiBase as clientApiBase, type ApiKey } from '../../lib/api'
import { useSession } from '../../lib/session'
import { ago, fullDate } from '../../lib/format'
import { SETUP_TARGETS, agentInfo, agentsMd } from '../../lib/agents'
import { CopyBlock } from '../ui/CopyBlock'

const SCOPES = [
  ['artifacts:read', 'Read artifacts'],
  ['artifacts:write', 'Publish versions'],
  ['comments:read', 'Read comments'],
  ['comments:write', 'Reply and resolve'],
] as const

const KEY_PLACEHOLDER = '<your-falconia-key>'

/**
 * Connect agents: one key, every agent.
 *
 * The page is built around the agents people actually use, with a
 * ready-to-paste configuration for each, because "MCP over streamable HTTP
 * with a bearer header" means nothing to most people and copy-paste means
 * everything. Whatever an agent publishes through here is labeled with its
 * name on the artifact's history.
 */
export function ConnectPage() {
  const { config } = useSession()
  const apiBase = config?.api_base_url ?? clientApiBase
  const mcpURL = config?.mcp_url ?? `${apiBase}/mcp`

  const [keys, setKeys] = useState<ApiKey[]>([])
  const [fresh, setFresh] = useState<string | null>(null)
  const [target, setTarget] = useState(SETUP_TARGETS[0].id)
  const [creating, setCreating] = useState(false)
  const [err, setErr] = useState('')

  const reload = () => api.keys().then((r) => setKeys(r.keys)).catch(() => setKeys([]))
  useEffect(() => { void reload() }, [])

  async function quickKey() {
    setCreating(true)
    setErr('')
    try {
      const label = target === 'cli' || target === 'agents-md' ? 'My agents' : `${agentLabel(target)} (MCP)`
      const r = await api.createKey({ label, kind: 'mcp' })
      setFresh(r.token)
      await reload()
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setCreating(false)
    }
  }

  const key = fresh ?? KEY_PLACEHOLDER
  const setup = SETUP_TARGETS.find((t) => t.id === target)

  return (
    <div className="mx-auto max-w-4xl space-y-10 px-4 py-8 sm:px-6">
      <header>
        <h1 className="text-2xl font-semibold text-white">Connect your agents</h1>
        <p className="mt-1 max-w-2xl text-sm text-ink-500">
          One key, every agent. Claude Code, Codex, Gemini CLI, Cursor and the others can then
          create, read and fix artifacts here — and each version remembers which agent made it.
          Switch subscriptions any time: the artifacts stay.
        </p>
      </header>

      <Step n={1} title="Create a key">
        {fresh ? (
          <div className="rounded-xl border border-accent-500/60 bg-accent-500/10 p-4">
            <p className="mb-2 text-sm text-ink-100">
              Your key is below and already filled into the snippets. Copy it now: it is shown only once.
            </p>
            <CopyBlock text={fresh} />
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            <button className="btn-primary" onClick={quickKey} disabled={creating}>
              {creating ? 'Creating…' : 'Create a key for my agents'}
            </button>
            <p className="text-xs text-ink-500">
              A key can read, publish and comment as you. It can never delete artifacts,
              invite people or create other keys.
            </p>
          </div>
        )}
        {err && <p className="mt-2 text-sm text-red-400">{err}</p>}
      </Step>

      <Step n={2} title="Add Falconia to your agent">
        <div className="mb-4 flex flex-wrap gap-2">
          {SETUP_TARGETS.map((t) => (
            <AgentTab key={t.id} id={t.id} label={t.label} active={target === t.id} onClick={() => setTarget(t.id)} />
          ))}
          <AgentTab id="cli" label="Any agent with a shell" active={target === 'cli'} onClick={() => setTarget('cli')} />
          <AgentTab id="agents-md" label="AGENTS.md" active={target === 'agents-md'} onClick={() => setTarget('agents-md')} />
        </div>

        {setup && (
          <div className="space-y-2">
            <p className="text-xs text-ink-500">{setup.where}</p>
            <CopyBlock text={setup.snippet(mcpURL, key)} />
            {setup.note && <p className="text-xs text-ink-500">{setup.note}</p>}
          </div>
        )}

        {target === 'cli' && (
          <div className="space-y-3">
            <p className="text-sm text-ink-300">
              Any agent that can run commands — including the ones above, Aider, Amp or your own
              scripts — can use the <code className="text-ink-100">falconia</code> CLI. It detects
              Claude Code, Codex and Gemini CLI by itself and labels what they publish.
            </p>
            <CopyBlock label="Install (macOS, Linux)" text={`curl -fsSL ${apiBase}/api/install | sh`} />
            <CopyBlock label="Install (Windows PowerShell)" text={`irm ${apiBase}/api/install.ps1 | iex`} />
            <CopyBlock label="Sign in with your browser, or use the key" text={`falconia login\n# or, non-interactive:\nexport FALCONIA_TOKEN=${key}`} />
            <CopyBlock label="Then" text={`falconia deploy ./dist --title "My report" --visibility link\nfalconia pull team/roadmap      # continue someone else's artifact\nfalconia comments team/roadmap  # read what people asked`} />
          </div>
        )}

        {target === 'agents-md' && (
          <div className="space-y-2">
            <p className="text-sm text-ink-300">
              Drop this into your project's <code className="text-ink-100">AGENTS.md</code>
              {' '}(or <code className="text-ink-100">CLAUDE.md</code> / <code className="text-ink-100">GEMINI.md</code>):
              every agent working in the repository will know how to publish and pick up artifacts.
            </p>
            <CopyBlock text={agentsMd(apiBase)} wrap />
          </div>
        )}
      </Step>

      <Step n={3} title="Try it">
        <p className="mb-2 text-sm text-ink-300">Ask your agent something like:</p>
        <CopyBlock mono={false} wrap text={'Build a one-page HTML dashboard of our last 6 months of signups (make up realistic numbers) and publish it to Falconia with link sharing. Give me the link.'} />
        <p className="mt-2 text-xs text-ink-500">
          It will appear in <Link to="/" className="text-accent-400">your dashboard</Link> with the agent's name on it.
        </p>
      </Step>

      <section className="card space-y-2 p-5">
        <h2 className="font-medium text-white">Prefer the browser? Use your subscription, no API key</h2>
        <p className="text-sm text-ink-500">
          Run <code className="text-ink-300">falconia agent</code> on your computer and chat with your
          own Claude Code, Codex or Gemini CLI from{' '}
          <Link to="/new" className="text-accent-400">New artifact</Link>. Model credentials never
          leave your machine.
        </p>
      </section>

      <section className="space-y-3">
        <h2 className="text-lg font-medium text-white">Continuous integration</h2>
        <p className="text-sm text-ink-500">A key in your pipeline's secrets, and every build publishes its own preview.</p>
        <CopyBlock text={`- name: Publish preview to Falconia
  env:
    FALCONIA_API_URL: \${{ secrets.FALCONIA_API_URL }}
    FALCONIA_TOKEN:   \${{ secrets.FALCONIA_TOKEN }}
  run: |
    curl -fsSL "$FALCONIA_API_URL/api/install" | sh
    ~/.local/bin/falconia deploy ./dist \\
      --space team --slug preview-pr-\${{ github.event.number }} \\
      --visibility org -m "\${{ github.event.pull_request.title }}"`} />
      </section>

      <KeysSection keys={keys} onChanged={reload} onCreated={(t) => setFresh(t)} />
    </div>
  )
}

function agentLabel(id: string) {
  return SETUP_TARGETS.find((t) => t.id === id)?.label ?? id
}

function AgentTab({ id, label, active, onClick }: { id: string; label: string; active: boolean; onClick(): void }) {
  const color = agentInfo(id).color
  return (
    <button onClick={onClick} className={`cat-chip !py-1 text-sm ${active ? 'cat-chip-active' : ''}`}>
      <span className="h-2 w-2 rounded-full" style={{ background: id === 'cli' || id === 'agents-md' ? '#7c8599' : color }} />
      {label}
    </button>
  )
}

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <section className="flex gap-4">
      <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-accent-500 text-sm font-semibold text-ink-950">{n}</span>
      <div className="min-w-0 flex-1">
        <h2 className="mb-3 text-lg font-medium text-white">{title}</h2>
        {children}
      </div>
    </section>
  )
}

function KeysSection({ keys, onChanged, onCreated }: {
  keys: ApiKey[]; onChanged(): void; onCreated(token: string): void
}) {
  const [label, setLabel] = useState('')
  const [kind, setKind] = useState('ci')
  const [scopes, setScopes] = useState<string[]>(SCOPES.map(([id]) => id))
  const [expires, setExpires] = useState(0)
  const [advanced, setAdvanced] = useState(false)
  const [err, setErr] = useState('')

  async function create(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    try {
      const r = await api.createKey({ label: label.trim() || 'Key', kind, scopes, expires_in_days: expires || undefined })
      onCreated(r.token)
      setLabel('')
      onChanged()
      window.scrollTo({ top: 0, behavior: 'smooth' })
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    }
  }

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-medium text-white">Your keys</h2>
        <button className="text-sm text-ink-500 hover:text-ink-300" onClick={() => setAdvanced((v) => !v)}>
          {advanced ? 'Hide' : 'New key with custom permissions'}
        </button>
      </div>

      {advanced && (
        <form onSubmit={create} className="card space-y-3 p-4">
          <div className="flex flex-wrap gap-2">
            <input className="input max-w-xs" placeholder="What is it for?"
              value={label} onChange={(e) => setLabel(e.target.value)} />
            <select className="input w-44" value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="ci">CI pipeline</option>
              <option value="mcp">Agent (MCP)</option>
              <option value="cli">Command line</option>
            </select>
            <select className="input w-40" value={expires} onChange={(e) => setExpires(Number(e.target.value))}>
              <option value={0}>Never expires</option>
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
              <option value={365}>1 year</option>
            </select>
            <button className="btn-primary">Create</button>
          </div>
          <div className="flex flex-wrap gap-3">
            {SCOPES.map(([id, text]) => (
              <label key={id} className="flex items-center gap-2 text-sm text-ink-300">
                <input type="checkbox" checked={scopes.includes(id)}
                  onChange={(e) => setScopes(e.target.checked ? [...scopes, id] : scopes.filter((s) => s !== id))} />
                {text}
              </label>
            ))}
          </div>
          {err && <p className="text-sm text-red-400">{err}</p>}
        </form>
      )}

      {keys.length === 0 ? (
        <p className="text-sm text-ink-500">No keys yet.</p>
      ) : (
        <ul className="divide-y divide-ink-800 rounded-xl border border-ink-800">
          {keys.map((k) => (
            <li key={k.id} className="flex items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm text-ink-100">{k.label}</div>
                <div className="text-xs text-ink-500">
                  <code>…{k.prefix}…</code> · {k.kind} · created {ago(k.created_at)}
                  {k.last_used_at ? ` · used ${ago(k.last_used_at)}` : ' · never used'}
                  {k.expires_at ? ` · expires ${fullDate(k.expires_at)}` : ''}
                </div>
              </div>
              <button className="text-xs text-ink-500 hover:text-red-400"
                onClick={async () => { await api.revokeKey(k.id); onChanged() }}>
                Revoke
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
