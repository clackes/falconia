import { useState } from 'react'
import { api, type Anchor, type Thread } from '../../lib/api'
import { ago } from '../../lib/format'
import { AgentBadge } from '../ui/AgentBadge'

type Props = {
  artifactRef: string
  threads: Thread[]
  orphanIds: Set<string>
  focusedId: string | null
  /** Anchor captured inside the iframe, waiting for the comment's text. */
  pendingAnchor: Anchor | null
  /** Can resolve any thread: the owner and editors (and their agents). */
  canModerate: boolean
  /** Who is looking, to know which threads they can close. */
  currentUserId: string
  onRefresh(): void
  onCancelPending(): void
  onFocus(id: string | null): void
  onStartPicking(): void
  picking: boolean
  onClose(): void
}

/**
 * The comments column.
 *
 * A thread starts from a spot on the page, gathers replies, and at some point
 * gets closed — often by an agent, right after it published the fix. Anyone
 * who can see the artifact can reply: the point is that people who never see
 * the code can still say what is wrong.
 */
export function ReviewPanel(props: Props) {
  const { threads, orphanIds, focusedId, pendingAnchor, picking } = props
  const [showResolved, setShowResolved] = useState(false)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const open = threads.filter((t) => t.status === 'open')
  const resolved = threads.filter((t) => t.status === 'resolved')
  const visible = showResolved ? threads : open

  async function submitNew(e: React.FormEvent) {
    e.preventDefault()
    if (!draft.trim()) return
    setBusy(true)
    setErr('')
    try {
      await api.createThread(props.artifactRef, {
        body: draft.trim(),
        anchor: pendingAnchor ?? undefined,
      })
      setDraft('')
      props.onCancelPending()
      props.onRefresh()
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <aside className="fixed inset-x-0 bottom-0 z-30 flex max-h-[70vh] flex-col border-t border-ink-800 bg-ink-950 md:static md:max-h-none md:w-96 md:shrink-0 md:border-l md:border-t-0">
      <header className="flex items-center gap-2 border-b border-ink-800 px-4 py-3">
        <h2 className="text-sm font-medium text-white">Comments</h2>
        {open.length > 0 && (
          <span className="chip !bg-amber-500/15 !text-amber-300">{open.length} open</span>
        )}
        <button
          className={`ml-auto btn ${picking ? 'bg-accent-500 text-ink-950' : 'btn-ghost'}`}
          onClick={props.onStartPicking}
          title="Click a spot of the artifact to attach a comment to it"
        >
          {picking ? 'Pick a spot…' : '＋ Comment'}
        </button>
        <button className="btn-ghost !px-2 md:hidden" onClick={props.onClose} aria-label="Close comments">✕</button>
      </header>

      {(pendingAnchor || picking) && (
        <form onSubmit={submitNew} className="border-b border-ink-800 bg-ink-900 p-4">
          {pendingAnchor ? (
            <p className="mb-2 text-xs text-ink-500">
              Attached to{' '}
              <span className="text-ink-300">
                {pendingAnchor.quote?.slice(0, 60) || pendingAnchor.selector || 'a spot on the page'}
              </span>
            </p>
          ) : (
            <p className="mb-2 text-xs text-ink-500">
              Click a spot of the artifact, or write here for a general comment.
            </p>
          )}
          <textarea
            className="input min-h-[80px] resize-y"
            placeholder="What should change?"
            value={draft}
            autoFocus
            onChange={(e) => setDraft(e.target.value)}
          />
          {err && <p className="mt-2 text-xs text-red-400">{err}</p>}
          <div className="mt-2 flex gap-2">
            <button className="btn-primary flex-1" disabled={busy || !draft.trim()}>
              {busy ? '…' : 'Post'}
            </button>
            <button type="button" className="btn-ghost"
              onClick={() => { setDraft(''); setErr(''); props.onCancelPending() }}>
              Cancel
            </button>
          </div>
        </form>
      )}

      <div className="flex-1 overflow-y-auto">
        {visible.length === 0 ? (
          <div className="px-6 py-12 text-center text-sm text-ink-500">
            <p>No open comments.</p>
            <p className="mt-2 text-xs">
              Comments pinned here can be read and fixed by any agent:
              {' '}<code className="text-ink-300">falconia comments</code> or the MCP tool
              {' '}<code className="text-ink-300">list_threads</code>.
            </p>
          </div>
        ) : (
          <ul className="divide-y divide-ink-800">
            {visible.map((t) => (
              <ThreadItem
                key={t.id}
                t={t}
                n={threads.indexOf(t) + 1}
                orphan={orphanIds.has(t.id)}
                focused={focusedId === t.id}
                canResolve={props.canModerate || t.created_by === props.currentUserId}
                onFocus={() => props.onFocus(t.id)}
                onRefresh={props.onRefresh}
              />
            ))}
          </ul>
        )}
      </div>

      {resolved.length > 0 && (
        <button
          className="border-t border-ink-800 px-4 py-3 text-left text-xs text-ink-500 hover:text-ink-300"
          onClick={() => setShowResolved((v) => !v)}
        >
          {showResolved ? 'Hide' : 'Show'} {resolved.length} resolved
        </button>
      )}
    </aside>
  )
}

function ThreadItem({ t, n, orphan, focused, canResolve, onFocus, onRefresh }: {
  t: Thread; n: number; orphan: boolean; focused: boolean
  canResolve: boolean; onFocus(): void; onRefresh(): void
}) {
  const [reply, setReply] = useState('')
  const [busy, setBusy] = useState(false)

  async function send() {
    if (!reply.trim()) return
    setBusy(true)
    try {
      await api.reply(t.id, reply.trim())
      setReply('')
      onRefresh()
    } finally {
      setBusy(false)
    }
  }

  async function toggle() {
    setBusy(true)
    try {
      await api.setThreadStatus(t.id, t.status === 'open')
      onRefresh()
    } finally {
      setBusy(false)
    }
  }

  return (
    <li
      className={`px-4 py-3 transition-colors ${focused ? 'bg-ink-900' : 'hover:bg-ink-900/50'}`}
      onClick={onFocus}
    >
      <div className="flex items-center gap-2">
        <span className={`grid h-5 w-5 place-items-center rounded-full text-[11px] font-semibold text-white ${
          t.status === 'resolved' ? 'bg-emerald-600' : 'bg-amber-500'}`}>
          {n}
        </span>
        <span className="truncate text-xs text-ink-500">
          {t.page_path} · v{t.version_number}
        </span>
        {orphan && (
          <span className="chip !bg-ink-800 !text-ink-500" title="The spot it was attached to no longer exists in this version">
            detached
          </span>
        )}
      </div>

      {t.anchor?.quote && (
        <p className="mt-2 line-clamp-2 border-l-2 border-ink-700 pl-2 text-xs italic text-ink-500">
          {t.anchor.quote}
        </p>
      )}

      <ul className="mt-2 space-y-2">
        {t.comments.map((c) => (
          <li key={c.id}>
            <div className="flex flex-wrap items-baseline gap-2">
              <span className="text-sm font-medium text-ink-100">{c.user_name}</span>
              {c.agent && <AgentBadge agent={c.agent} size="xs" title={`Written through ${c.agent}`} />}
              <span className="text-[11px] text-ink-500">{ago(c.created_at)}</span>
            </div>
            <p className="whitespace-pre-wrap text-sm text-ink-300">{c.body}</p>
          </li>
        ))}
      </ul>

      {focused && (
        <div className="mt-3 space-y-2" onClick={(e) => e.stopPropagation()}>
          <textarea
            className="input min-h-[56px] resize-y text-sm"
            placeholder="Reply…"
            value={reply}
            onChange={(e) => setReply(e.target.value)}
          />
          <div className="flex gap-2">
            <button className="btn-ghost text-xs" disabled={busy || !reply.trim()} onClick={send}>
              Reply
            </button>
            {canResolve && (
              <button className="btn-outline text-xs" disabled={busy} onClick={toggle}>
                {t.status === 'open' ? 'Mark as resolved' : 'Reopen'}
              </button>
            )}
          </div>
        </div>
      )}
    </li>
  )
}
