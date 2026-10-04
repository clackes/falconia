import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Artifact } from '../../lib/api'
import { useSession } from '../../lib/session'
import { agentInfo } from '../../lib/agents'
import { ArtifactCard } from './ArtifactCard'

type Tab = 'all' | 'mine' | 'shared'

/**
 * The home: everything you can open, most recent first, whichever agent made
 * it.
 *
 * Access filtering happens in the backend query, not here: a list must never
 * show the title of something that would then answer "not found".
 */
export function Dashboard() {
  const { user } = useSession()
  const [tab, setTab] = useState<Tab>('all')
  const [query, setQuery] = useState('')
  const [agent, setAgent] = useState('')
  const [items, setItems] = useState<Artifact[] | null>(null)
  // The agent chips come from the unfiltered list, so picking one does not
  // make the others disappear.
  const [knownAgents, setKnownAgents] = useState<string[]>([])
  const [err, setErr] = useState('')

  useEffect(() => {
    let alive = true
    setItems(null)
    setErr('')
    const q = query.trim() || undefined
    const load = tab === 'mine' ? api.myArtifacts()
      : tab === 'shared' ? api.sharedWithMe()
      : api.feed({ query: q, agent: agent || undefined, limit: 100 })

    load.then((r) => {
      if (!alive) return
      let list = r.artifacts
      // "Mine" and "shared" are not filtered server-side by agent.
      if (agent && tab !== 'all') list = list.filter((a) => a.agents.includes(agent))
      setItems(list)
      if (!agent) {
        const seen = new Set<string>()
        r.artifacts.forEach((a) => a.agents.forEach((x) => seen.add(x)))
        setKnownAgents((prev) => Array.from(new Set([...prev, ...seen])))
      }
    }).catch((e) => { if (alive) { setErr(String(e.message || e)); setItems([]) } })
    return () => { alive = false }
  }, [tab, query, agent])

  const needsAttention = useMemo(
    () => (items ?? []).filter((a) => a.open_threads > 0 && a.my_role !== 'viewer'),
    [items])
  const firstRun = items !== null && items.length === 0 && tab === 'all' && !query && !agent

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-white">
            Hi, {user?.display_name?.split(' ')[0]}
          </h1>
          <p className="mt-1 text-sm text-ink-500">
            Everything you and your team made, with any AI agent.
          </p>
        </div>
        <div className="flex gap-2">
          <Link to="/connect" className="btn-outline">Connect an agent</Link>
          <Link to="/new" className="btn-primary">New artifact</Link>
        </div>
      </header>

      {firstRun ? <GetStarted /> : (
        <>
          {needsAttention.length > 0 && tab === 'all' && !agent && (
            <section className="mb-8">
              <h2 className="mb-3 text-sm font-medium text-amber-300">
                People left comments to address
              </h2>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {needsAttention.slice(0, 3).map((a) => <ArtifactCard key={a.id} a={a} />)}
              </div>
            </section>
          )}

          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Tabs tab={tab} onChange={setTab} />
            <input
              className="input ml-auto max-w-xs"
              placeholder="Search…"
              value={query}
              onChange={(e) => { setQuery(e.target.value); setTab('all') }}
            />
          </div>

          {knownAgents.length > 0 && (
            <div className="mb-5 flex flex-wrap items-center gap-2">
              <span className="text-xs text-ink-500">Made with</span>
              <button className={`cat-chip !py-1 text-xs ${agent === '' ? 'cat-chip-active' : ''}`}
                onClick={() => setAgent('')}>Any agent</button>
              {knownAgents.map((id) => {
                const a = agentInfo(id)
                return (
                  <button key={id}
                    className={`cat-chip !py-1 text-xs ${agent === id ? 'cat-chip-active' : ''}`}
                    onClick={() => setAgent(agent === id ? '' : id)}>
                    <span className="h-1.5 w-1.5 rounded-full" style={{ background: a.color }} />
                    {a.label}
                  </button>
                )
              })}
            </div>
          )}

          {err && <p className="mb-4 text-sm text-red-400">{err}</p>}

          {items === null ? (
            <p className="py-12 text-center text-ink-500">Loading…</p>
          ) : items.length === 0 ? (
            <Empty tab={tab} filtered={!!(query || agent)} />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {items.map((a) => <ArtifactCard key={a.id} a={a} />)}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function Tabs({ tab, onChange }: { tab: Tab; onChange: (t: Tab) => void }) {
  const opts: [Tab, string][] = [['all', 'All'], ['mine', 'Created by me'], ['shared', 'Shared with me']]
  return (
    <div className="flex gap-1 rounded-lg bg-ink-900 p-1">
      {opts.map(([id, label]) => (
        <button key={id} onClick={() => onChange(id)}
          className={`rounded-md px-3 py-1.5 text-sm transition-colors ${
            tab === id ? 'bg-ink-700 text-white' : 'text-ink-300 hover:text-white'}`}>
          {label}
        </button>
      ))}
    </div>
  )
}

function Empty({ tab, filtered }: { tab: Tab; filtered: boolean }) {
  const msg = filtered
    ? 'Nothing matches these filters.'
    : tab === 'shared'
      ? 'Nobody has shared anything with you yet.'
      : 'You have not published anything yet.'
  return (
    <div className="card px-6 py-16 text-center">
      <p className="text-ink-300">{msg}</p>
      {!filtered && (
        <p className="mx-auto mt-3 max-w-md text-sm text-ink-500">
          Ask your agent to publish something, start from{' '}
          <Link to="/new" className="text-accent-400">New artifact</Link>, or run{' '}
          <code className="text-ink-300">falconia deploy ./folder</code>.
        </p>
      )}
    </div>
  )
}

/** The first-run guide: what Falconia is for, in three steps. */
function GetStarted() {
  const steps = [
    {
      n: 1, title: 'Connect the agents you already use',
      body: 'Claude Code, Codex, Gemini CLI, Cursor, VS Code… one key, a copy-paste config for each.',
      cta: <Link to="/connect" className="btn-primary text-sm">Connect an agent</Link>,
    },
    {
      n: 2, title: 'Ask it to build something',
      body: '“Make a dashboard of these numbers and publish it to Falconia.” It gets a permanent URL, with every version kept.',
      cta: <Link to="/new" className="btn-outline text-sm">Or start in the browser</Link>,
    },
    {
      n: 3, title: 'Share it like a Google Doc',
      body: 'Restricted, your team, or anyone with the link. People pin comments on the page; any agent can read them and fix it.',
      cta: null,
    },
  ]
  return (
    <section className="card overflow-hidden">
      <div className="border-b border-ink-800 bg-gradient-to-r from-accent-500/10 to-transparent px-6 py-6">
        <h2 className="text-lg font-semibold text-white">Your artifacts, free from any one AI</h2>
        <p className="mt-1 max-w-2xl text-sm text-ink-300">
          Make it with Claude today, fix it with Codex tomorrow, let a teammate improve it with
          Gemini. Falconia keeps the artifact, its history and the conversation around it —
          not the subscription that made it.
        </p>
      </div>
      <ol className="grid gap-px bg-ink-800 md:grid-cols-3">
        {steps.map((s) => (
          <li key={s.n} className="flex flex-col gap-3 bg-ink-900 p-6">
            <span className="grid h-7 w-7 place-items-center rounded-full bg-accent-500 text-sm font-semibold text-ink-950">{s.n}</span>
            <h3 className="font-medium text-ink-100">{s.title}</h3>
            <p className="flex-1 text-sm text-ink-500">{s.body}</p>
            {s.cta}
          </li>
        ))}
      </ol>
    </section>
  )
}
