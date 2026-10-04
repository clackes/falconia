import { useEffect, useState } from 'react'
import { api, type Artifact, type Visibility } from '../../lib/api'
import { copyText, visibilityHint, visibilityIcon } from '../../lib/format'
import { Dialog } from './Dialog'

type Share = { email: string; role: string }

/**
 * Who can open this artifact — modeled on the Share dialog of a Google Doc,
 * because everyone already knows how that one works.
 *
 * Two separate levers: PEOPLE adds individuals by email (they need not have an
 * account yet), GENERAL ACCESS decides who else gets in — nobody, the whole
 * team, or anyone holding the link. Keeping them apart avoids the usual
 * mistake of opening a document to everyone just to show it to one colleague.
 */
export function ShareDialog({ artifact, onClose, onChanged }: {
  artifact: Artifact; onClose(): void; onChanged(a: Artifact): void
}) {
  const [art, setArt] = useState(artifact)
  const [shares, setShares] = useState<Share[]>([])
  const [email, setEmail] = useState('')
  const [role, setRole] = useState('viewer')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [copied, setCopied] = useState(false)
  const [confirmRotate, setConfirmRotate] = useState(false)

  const isTeamSpace = art.space_kind === 'org'
  const link = window.location.origin + (art.visibility === 'link' && art.link_path ? art.link_path : art.path)

  useEffect(() => {
    api.shares(art.id).then((r) => setShares(r.shares)).catch(() => setShares([]))
  }, [art.id])

  async function patch(p: Record<string, unknown>) {
    setErr('')
    try {
      const r = await api.updateMeta(art.id, p)
      setArt(r.artifact)
      onChanged(r.artifact)
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    }
  }

  async function addPerson(e: React.FormEvent) {
    e.preventDefault()
    if (!email.trim()) return
    setBusy(true)
    setErr('')
    try {
      await api.addShare(art.id, email.trim(), role)
      setShares((await api.shares(art.id)).shares)
      setEmail('')
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  async function changeRole(s: Share, newRole: string) {
    if (newRole === 'remove') {
      await api.removeShare(art.id, s.email)
      setShares((list) => list.filter((x) => x.email !== s.email))
      return
    }
    await api.addShare(art.id, s.email, newRole)
    setShares((list) => list.map((x) => (x.email === s.email ? { ...x, role: newRole } : x)))
  }

  async function rotate() {
    setErr('')
    try {
      const r = await api.rotateLink(art.id)
      setArt(r.artifact)
      onChanged(r.artifact)
      setConfirmRotate(false)
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    }
  }

  return (
    <Dialog title={`Share “${art.title}”`} onClose={onClose} wide>
      <div className="space-y-6">
        <form onSubmit={addPerson} className="flex flex-col gap-2 sm:flex-row">
          <input className="input" type="email" placeholder="Add people by email"
            value={email} onChange={(e) => setEmail(e.target.value)} />
          <select className="input sm:w-36" value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="viewer">Viewer</option>
            <option value="editor">Editor</option>
          </select>
          <button className="btn-primary shrink-0" disabled={busy || !email.trim()}>Add</button>
        </form>

        <section>
          <h3 className="mb-2 text-sm font-medium text-ink-100">People with access</h3>
          <ul className="space-y-1">
            <Person name={art.author_name} detail="Owner" />
            {shares.map((s) => (
              <Person key={s.email} name={s.email}
                detail={
                  <select className="rounded-md border border-ink-700 bg-ink-900 px-2 py-1 text-xs text-ink-300"
                    value={s.role} onChange={(e) => changeRole(s, e.target.value)}>
                    <option value="viewer">Viewer</option>
                    <option value="editor">Editor</option>
                    <option value="remove">Remove access</option>
                  </select>
                } />
            ))}
          </ul>
          <p className="mt-2 text-xs text-ink-500">
            Viewers can open and comment. Editors can also publish new versions — with any agent.
            People without an account get access the first time they sign in.
          </p>
        </section>

        <section>
          <h3 className="mb-2 text-sm font-medium text-ink-100">General access</h3>
          <div className="flex items-start gap-3 rounded-xl border border-ink-800 p-3">
            <span className="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-ink-800 text-base">
              {visibilityIcon[art.visibility]}
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <select className="rounded-md border border-ink-700 bg-ink-900 px-2 py-1 text-sm text-ink-100"
                  value={art.visibility}
                  onChange={(e) => patch({ visibility: e.target.value as Visibility })}>
                  <option value="private">Restricted</option>
                  {isTeamSpace && <option value="org">Team · {art.space_name}</option>}
                  <option value="link">Anyone with the link</option>
                </select>
                {art.visibility === 'org' && (
                  <select className="rounded-md border border-ink-700 bg-ink-900 px-2 py-1 text-sm text-ink-300"
                    value={art.team_role} onChange={(e) => patch({ team_role: e.target.value })}>
                    <option value="viewer">Viewer</option>
                    <option value="editor">Editor</option>
                  </select>
                )}
                {art.visibility === 'link' && <span className="text-sm text-ink-500">Viewer</span>}
              </div>
              <p className="mt-1 text-xs text-ink-500">
                {visibilityHint[art.visibility]}
                {!isTeamSpace && art.visibility === 'private' && (
                  <> Move it to a team space to share it with a whole team.</>
                )}
              </p>
              {art.visibility === 'link' && (
                <p className="mt-1 text-xs text-ink-500">
                  Signed-in people who open the link can also comment.
                  {' '}{confirmRotate ? (
                    <span className="text-amber-300">
                      The current link will stop working.{' '}
                      <button className="underline" onClick={rotate}>Reset it</button>{' · '}
                      <button className="underline" onClick={() => setConfirmRotate(false)}>Keep it</button>
                    </span>
                  ) : (
                    <button className="text-ink-300 underline" onClick={() => setConfirmRotate(true)}>
                      Reset link
                    </button>
                  )}
                </p>
              )}
            </div>
          </div>
        </section>

        {err && <p className="text-sm text-red-400">{err}</p>}

        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-ink-800 pt-4">
          <button className="btn-outline" onClick={async () => {
            if (await copyText(link)) {
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            }
          }}>
            🔗 {copied ? 'Link copied' : 'Copy link'}
          </button>
          <button className="btn-primary" onClick={onClose}>Done</button>
        </div>
        <p className="-mt-3 break-all font-mono text-[11px] text-ink-500">{link}</p>
      </div>
    </Dialog>
  )
}

function Person({ name, detail }: { name: string; detail: React.ReactNode }) {
  return (
    <li className="flex items-center gap-3 rounded-lg px-1 py-1.5">
      <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-ink-800 text-sm text-ink-300">
        {name.slice(0, 1).toUpperCase()}
      </span>
      <span className="min-w-0 flex-1 truncate text-sm text-ink-100">{name}</span>
      <span className="shrink-0 text-xs text-ink-500">{detail}</span>
    </li>
  )
}
