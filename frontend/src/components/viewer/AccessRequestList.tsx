import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type AccessRequest } from '../../lib/api'
import { ago } from '../../lib/format'

/**
 * Pending requests for edit access, each answered in one click. Approving
 * adds the person as an editor, exactly like sharing with them by email.
 */
export function AccessRequestList({ requests, onDecided, showArtifact }: {
  requests: AccessRequest[]; onDecided(id: string, approved: boolean): void; showArtifact?: boolean
}) {
  const [busy, setBusy] = useState<string | null>(null)
  const [err, setErr] = useState('')

  async function decide(r: AccessRequest, approve: boolean) {
    setBusy(r.id)
    setErr('')
    try {
      await api.decideAccess(r.id, approve)
      onDecided(r.id, approve)
    } catch (e: any) {
      setErr(e?.message ?? 'Something went wrong')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="space-y-2">
      {requests.map((r) => (
        <div key={r.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg bg-ink-900 px-3 py-2.5">
          <div className="min-w-0 flex-1">
            <p className="text-sm text-ink-100">
              <span className="font-medium">{r.name || r.email}</span>
              <span className="text-ink-500"> wants to edit </span>
              {showArtifact && r.artifact_path
                ? <Link to={r.artifact_path} className="font-medium text-accent-400 hover:underline">{r.artifact_title}</Link>
                : <span className="text-ink-500">this artifact</span>}
              <span className="text-xs text-ink-500"> · {ago(r.created_at)}</span>
            </p>
            {r.message && <p className="mt-0.5 text-sm text-ink-300">“{r.message}”</p>}
            <p className="text-xs text-ink-500">{r.email}</p>
          </div>
          <div className="flex shrink-0 gap-2">
            <button className="btn-ghost text-sm" disabled={busy === r.id} onClick={() => decide(r, false)}>
              Decline
            </button>
            <button className="btn-primary text-sm" disabled={busy === r.id} onClick={() => decide(r, true)}>
              Make editor
            </button>
          </div>
        </div>
      ))}
      {err && <p className="text-sm text-red-400">{err}</p>}
    </div>
  )
}
