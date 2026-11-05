import { useState } from 'react'
import { api, type Artifact } from '../../lib/api'
import { useSession } from '../../lib/session'
import { Dialog } from './Dialog'

/**
 * Moves an artifact between the owner's personal space and their teams, in
 * any direction. The backend keeps the slug when it is free there and drops
 * team-wide access when the destination is not a team.
 */
export function MoveDialog({ artifact, onClose, onMoved }: {
  artifact: Artifact; onClose(): void; onMoved(a: Artifact): void
}) {
  const { spaces } = useSession()
  const targets = spaces.filter((s) => s.id !== artifact.space_id)
  const [picked, setPicked] = useState(targets[0]?.slug ?? '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const dest = targets.find((s) => s.slug === picked)

  return (
    <Dialog title="Move artifact" onClose={onClose}>
      <p className="mb-3 text-sm text-ink-300">
        Now in <span className="font-medium text-white">{artifact.space_name}</span>. Its versions,
        comments and share links move with it.
      </p>
      {targets.length === 0 ? (
        <p className="text-sm text-ink-500">You have no other space to move it to. Create a team first.</p>
      ) : (
        <div className="mb-4 space-y-1">
          {targets.map((s) => (
            <label key={s.id} className={`flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm ${
              picked === s.slug ? 'bg-ink-800 text-white' : 'text-ink-300 hover:bg-ink-900'}`}>
              <input type="radio" name="space" checked={picked === s.slug} onChange={() => setPicked(s.slug)} />
              <span className="flex-1">{s.name}</span>
              <span className="text-xs text-ink-500">{s.kind === 'personal' ? 'Personal' : 'Team'}</span>
            </label>
          ))}
        </div>
      )}
      {artifact.visibility === 'org' && dest?.kind === 'personal' && (
        <p className="mb-3 text-xs text-amber-300">
          A personal space has no team: general access will go back to restricted.
        </p>
      )}
      {err && <p className="mb-3 text-sm text-red-400">{err}</p>}
      <div className="flex justify-end gap-2">
        <button className="btn-ghost text-sm" onClick={onClose}>Cancel</button>
        <button className="btn-primary text-sm" disabled={busy || !dest}
          onClick={async () => {
            setBusy(true)
            setErr('')
            try {
              const r = await api.move(artifact.id, picked)
              onMoved(r.artifact)
            } catch (e: any) {
              setErr(e?.message ?? 'Move failed')
              setBusy(false)
            }
          }}>
          {busy ? 'Moving…' : `Move to ${dest?.name ?? '…'}`}
        </button>
      </div>
    </Dialog>
  )
}
