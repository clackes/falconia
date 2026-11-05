import { useState } from 'react'
import { api, type AccessRequest, type Artifact } from '../../lib/api'
import { Dialog } from './Dialog'

/** A viewer asks the owner for editor access, with an optional reason. */
export function RequestAccessDialog({ artifact, onClose, onSent }: {
  artifact: Artifact; onClose(): void; onSent(r: AccessRequest): void
}) {
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  return (
    <Dialog title="Request edit access" onClose={onClose}>
      <p className="mb-3 text-sm text-ink-300">
        {artifact.author_name} will see your request on their dashboard. Once approved, you and
        your agent can publish new versions of <span className="font-medium text-white">{artifact.title}</span>.
      </p>
      <label className="mb-1 block text-xs text-ink-500">Why (optional)</label>
      <textarea className="input mb-4 min-h-[80px] w-full resize-y" autoFocus maxLength={1000}
        placeholder="I'd like to fix the chart and add last month's numbers"
        value={message} onChange={(e) => setMessage(e.target.value)} />
      {err && <p className="mb-3 text-sm text-red-400">{err}</p>}
      <div className="flex justify-end gap-2">
        <button className="btn-ghost text-sm" onClick={onClose}>Cancel</button>
        <button className="btn-primary text-sm" disabled={busy} onClick={async () => {
          setBusy(true)
          setErr('')
          try {
            const r = await api.requestAccess(artifact.id, message)
            onSent(r.request)
          } catch (e: any) {
            setErr(e?.message ?? 'Request failed')
            setBusy(false)
          }
        }}>
          {busy ? 'Sending…' : 'Send request'}
        </button>
      </div>
    </Dialog>
  )
}
