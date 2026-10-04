import { useEffect, useState } from 'react'
import { api, type ArtifactVersion } from '../../lib/api'
import { fullDate, humanBytes } from '../../lib/format'
import { AgentBadge } from '../ui/AgentBadge'
import { Dialog } from './Dialog'

/**
 * The artifact's history. Each row is one deploy, none can be rewritten, and
 * each says which agent made it: the record of an artifact passing from one
 * AI to another.
 */
export function VersionsDialog({ artifactRef, current, onPick, onClose }: {
  artifactRef: string; current: number; onPick(n: number): void; onClose(): void
}) {
  const [versions, setVersions] = useState<ArtifactVersion[] | null>(null)

  useEffect(() => {
    api.versions(artifactRef).then((r) => setVersions(r.versions)).catch(() => setVersions([]))
  }, [artifactRef])

  return (
    <Dialog title="Version history" onClose={onClose} wide>
      {versions === null ? (
        <p className="text-ink-500">Loading…</p>
      ) : (
        <ul className="divide-y divide-ink-800">
          {versions.map((v) => (
            <li key={v.id}>
              <button
                className={`flex w-full items-start gap-3 px-1 py-3 text-left hover:bg-ink-800/50 ${
                  v.version_number === current ? 'bg-ink-800/40' : ''}`}
                onClick={() => onPick(v.version_number)}
              >
                <span className="chip shrink-0">v{v.version_number}</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm text-ink-100">
                    {v.commit_message || 'no description'}
                  </span>
                  <span className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-500">
                    <AgentBadge agent={v.agent} size="xs" />
                    <span>{v.author_name} · {fullDate(v.created_at)}</span>
                    {v.kind === 'bundle' && <span>· {v.file_count} files, {humanBytes(v.total_bytes)}</span>}
                  </span>
                </span>
                {v.version_number === current && (
                  <span className="shrink-0 text-xs text-accent-400">viewing</span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  )
}
