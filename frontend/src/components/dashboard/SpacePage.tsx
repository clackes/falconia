import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Artifact } from '../../lib/api'
import { useSession } from '../../lib/session'
import { ArtifactCard } from './ArtifactCard'

/** The artifacts of a single space. */
export function SpacePage() {
  const { space = '' } = useParams()
  const { spaces } = useSession()
  const [items, setItems] = useState<Artifact[] | null>(null)
  const meta = spaces.find((s) => s.slug === space)

  useEffect(() => {
    let alive = true
    setItems(null)
    api.feed({ space, limit: 200 })
      .then((r) => { if (alive) setItems(r.artifacts) })
      .catch(() => { if (alive) setItems([]) })
    return () => { alive = false }
  }, [space])

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-white">{meta?.name ?? space}</h1>
          <p className="mt-1 text-sm text-ink-500">
            {meta?.kind === 'org' ? 'Team space' : 'Your personal space'}
            {' · '}<code className="text-ink-300">{space}</code>
          </p>
        </div>
        <Link to="/new" className="btn-outline">Publish here</Link>
      </header>

      {items === null ? (
        <p className="py-12 text-center text-ink-500">Loading…</p>
      ) : items.length === 0 ? (
        <div className="card px-6 py-16 text-center text-ink-500">
          This space is empty. From a terminal, or from any agent:{' '}
          <code className="text-ink-300">falconia deploy ./folder --space {space}</code>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((a) => <ArtifactCard key={a.id} a={a} />)}
        </div>
      )}
    </div>
  )
}
