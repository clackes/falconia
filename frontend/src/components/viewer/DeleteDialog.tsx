import { useState } from 'react'
import { api, type Artifact } from '../../lib/api'
import { Dialog } from './Dialog'

/**
 * Deleting is permanent: versions, files, comments and share links go with
 * it. Typing the title is the speed bump that makes that a decision rather
 * than a misclick.
 */
export function DeleteDialog({ artifact, onClose, onDeleted }: {
  artifact: Artifact; onClose(): void; onDeleted(): void
}) {
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  return (
    <Dialog title="Delete artifact" onClose={onClose}>
      <p className="mb-3 text-sm text-ink-300">
        This permanently deletes <span className="font-medium text-white">{artifact.title}</span> with
        all its versions, comments and share links. It cannot be undone.
      </p>
      <label className="mb-1 block text-xs text-ink-500">Type the title to confirm</label>
      <input className="input mb-4 w-full" value={typed} autoFocus
        onChange={(e) => setTyped(e.target.value)} placeholder={artifact.title} />
      {err && <p className="mb-3 text-sm text-red-400">{err}</p>}
      <div className="flex justify-end gap-2">
        <button className="btn-ghost text-sm" onClick={onClose}>Cancel</button>
        <button className="btn bg-red-600 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-40"
          disabled={busy || typed.trim() !== artifact.title.trim()}
          onClick={async () => {
            setBusy(true)
            setErr('')
            try {
              await api.deleteArtifact(artifact.id)
              onDeleted()
            } catch (e: any) {
              setErr(e?.message ?? 'Delete failed')
              setBusy(false)
            }
          }}>
          {busy ? 'Deleting…' : 'Delete forever'}
        </button>
      </div>
    </Dialog>
  )
}
