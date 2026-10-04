import { useEffect, useState } from 'react'
import { api, type Event, type User } from '../../lib/api'
import { ago, humanBytes } from '../../lib/format'
import { useSession } from '../../lib/session'
import { AgentBadge } from '../ui/AgentBadge'

/**
 * The instance panel.
 *
 * How many people, how much space, what happened recently. Collecting orphan
 * blobs lives here and is manual: deleting bytes is irreversible, and the
 * decision stays with a person who looks at the numbers first.
 */
export function AdminPage() {
  const { user, config } = useSession()
  const [stats, setStats] = useState<Record<string, number | string> | null>(null)
  const [users, setUsers] = useState<User[]>([])
  const [events, setEvents] = useState<Event[]>([])
  const [gc, setGc] = useState('')

  const reload = () => {
    api.adminStats().then(setStats).catch(() => setStats(null))
    api.adminUsers().then((r) => setUsers(r.users)).catch(() => setUsers([]))
    api.adminEvents().then((r) => setEvents(r.events)).catch(() => setEvents([]))
  }
  useEffect(reload, [])

  return (
    <div className="mx-auto max-w-4xl space-y-10 px-4 py-8 sm:px-6">
      <header>
        <h1 className="text-2xl font-semibold text-white">Instance</h1>
        <p className="mt-1 text-sm text-ink-500">
          The state of this installation · Falconia {config?.version ?? ''}
        </p>
      </header>

      {stats && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Stat label="People" value={stats.users} />
          <Stat label="Teams" value={stats.orgs} />
          <Stat label="Artifacts" value={stats.artifacts} />
          <Stat label="Versions" value={stats.versions} />
          <Stat label="Files" value={stats.files} />
          <Stat label="Blobs" value={stats.blobs} />
          <Stat label="Storage" value={humanBytes(Number(stats.bytes) || 0)} />
          <Stat label="Open comments" value={stats.open_threads} />
        </div>
      )}

      <section>
        <h2 className="mb-3 text-lg font-medium text-white">Disk space</h2>
        <p className="mb-3 text-sm text-ink-500">
          A blob no version references any more is reclaimable space. Collection skips the ones
          created in the last hours, because a deploy in progress has uploaded them but not
          linked them yet.
        </p>
        <div className="flex gap-2">
          <button className="btn-outline" onClick={async () => {
            const r = await api.adminGC(true)
            setGc(`${r.orphans} reclaimable blobs.`)
          }}>Count</button>
          <button className="btn-ghost" onClick={async () => {
            if (!confirm('Permanently delete orphan blobs?')) return
            const r = await api.adminGC(false)
            setGc(`Deleted ${r.deleted} of ${r.orphans} blobs.`)
            reload()
          }}>Delete</button>
          {gc && <span className="self-center text-sm text-ink-300">{gc}</span>}
        </div>
      </section>

      <section>
        <h2 className="mb-3 text-lg font-medium text-white">People</h2>
        <ul className="divide-y divide-ink-800 rounded-xl border border-ink-800">
          {users.map((u) => (
            <li key={u.id} className="flex items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm text-ink-100">{u.display_name}</div>
                <div className="truncate text-xs text-ink-500">{u.email} · joined {ago(u.created_at)}</div>
              </div>
              {u.is_admin && <span className="chip">admin</span>}
              {u.id !== user?.id && (
                <button className="text-xs text-ink-500 hover:text-ink-100"
                  onClick={async () => { await api.adminSetRole(u.id, !u.is_admin); reload() }}>
                  {u.is_admin ? 'Remove admin' : 'Make admin'}
                </button>
              )}
            </li>
          ))}
        </ul>
      </section>

      <section>
        <h2 className="mb-3 text-lg font-medium text-white">Activity</h2>
        <ul className="divide-y divide-ink-800 rounded-xl border border-ink-800 text-sm">
          {events.map((e) => (
            <li key={e.id} className="flex items-center gap-3 px-4 py-2">
              <span className="chip shrink-0">{e.kind}</span>
              <span className="min-w-0 flex-1 truncate text-ink-300">
                {e.actor_name || 'someone'} · {e.artifact_title || '—'}
              </span>
              {e.agent && <AgentBadge agent={e.agent} size="xs" />}
              <span className="shrink-0 text-xs text-ink-500">{e.via} · {ago(e.created_at)}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number | string | undefined }) {
  return (
    <div className="card p-4">
      <div className="text-2xl font-semibold text-white">{value ?? '—'}</div>
      <div className="mt-1 text-xs text-ink-500">{label}</div>
    </div>
  )
}
