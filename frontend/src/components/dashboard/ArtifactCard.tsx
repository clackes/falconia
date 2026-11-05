import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { Artifact } from '../../lib/api'
import { Menu, MenuItem } from '../ui/Menu'
import { MoveDialog } from '../viewer/MoveDialog'
import { DeleteDialog } from '../viewer/DeleteDialog'
import { ago, visibilityIcon, visibilityLabel } from '../../lib/format'
import { AgentStack } from '../ui/AgentBadge'

/**
 * One artifact in a list.
 *
 * Open comments come first when there are any: the most frequent question in
 * front of a list of published things is not "what is this" but "what are
 * people asking me to fix". The agents that worked on it come right after:
 * they are the visible proof that the artifact does not belong to any of them.
 */
export function ArtifactCard({ a, onChange }: {
  a: Artifact
  /** Called after a move (with the moved artifact) or a delete (with null). */
  onChange?(next: Artifact | null): void
}) {
  const [dialog, setDialog] = useState<'move' | 'delete' | null>(null)
  const manage = a.my_role === 'owner' && !!onChange

  return (
    <div className="relative flex">
    <Link to={a.path} className={`card-hover flex flex-1 flex-col p-4 ${manage ? 'pr-12' : ''}`}>
      <div className="flex items-start gap-3">
        <Thumb a={a} />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-2">
            <h3 className="truncate font-medium text-ink-100">{a.title}</h3>
            {a.open_threads > 0 && (
              <span className="chip shrink-0 !bg-amber-500/15 !text-amber-300">
                {a.open_threads} open
              </span>
            )}
          </div>
          {a.description ? (
            <p className="mt-1 line-clamp-2 text-sm text-ink-500">{a.description}</p>
          ) : (
            <p className="mt-1 text-sm text-ink-500">by {a.author_name}</p>
          )}
        </div>
      </div>
      <div className="mt-3 flex flex-1 items-end justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-ink-500">
          <span className="text-ink-300">{a.space_slug}</span>
          <span>v{a.latest_version}</span>
          <span title={visibilityLabel[a.visibility]}>{visibilityIcon[a.visibility]} {visibilityLabel[a.visibility]}</span>
          <span>{ago(a.updated_at)}</span>
        </div>
      </div>
      {a.agents.length > 0 && (
        <div className="mt-2"><AgentStack agents={a.agents} /></div>
      )}
    </Link>
    {/* Outside the link: a button inside an <a> is invalid and would navigate. */}
    {manage && (
      <div className="absolute right-2 top-2">
        <Menu label={<span aria-hidden>⋯</span>} title="Manage this artifact"
          className="btn-ghost !bg-transparent !px-2 !py-1 text-ink-500 hover:!bg-ink-800 hover:text-white">
          <MenuItem onClick={() => setDialog('move')} hint={`Now in ${a.space_name}`}>Move to…</MenuItem>
          <MenuItem onClick={() => setDialog('delete')} danger>Delete</MenuItem>
        </Menu>
      </div>
    )}
    {dialog === 'move' && (
      <MoveDialog artifact={a} onClose={() => setDialog(null)}
        onMoved={(next) => { setDialog(null); onChange?.({ ...a, ...next, my_role: a.my_role }) }} />
    )}
    {dialog === 'delete' && (
      <DeleteDialog artifact={a} onClose={() => setDialog(null)}
        onDeleted={() => { setDialog(null); onChange?.(null) }} />
    )}
    </div>
  )
}

function Thumb({ a }: { a: Artifact }) {
  if (a.logo_url) {
    return (
      <img src={a.logo_url} alt="" loading="lazy"
        className="h-11 w-11 shrink-0 rounded-lg bg-ink-800 object-cover" />
    )
  }
  return (
    <div
      className="grid h-11 w-11 shrink-0 place-items-center rounded-lg text-lg"
      style={{ background: a.theme_color || '#171a23' }}
    >
      {a.icon || '📄'}
    </div>
  )
}
