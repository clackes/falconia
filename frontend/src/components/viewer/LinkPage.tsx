import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { api, type LinkArtifact } from '../../lib/api'
import { mountArtifact, type SandboxHandle } from '../../lib/sandbox'
import { useSession } from '../../lib/session'
import { ago } from '../../lib/format'
import { AgentStack } from '../ui/AgentBadge'
import { Logo } from '../ui/Logo'
import { ArtifactPage } from './ArtifactPage'

/**
 * /p/<token>: an artifact shared with "anyone with the link".
 *
 * Signed out, it is a clean, read-only view — the artifact, who made it and
 * with which agents, and an invitation to sign in to comment. Signed in, the
 * backend remembers the visit, which grants view access, and the full viewer
 * with comments takes over.
 */
export function LinkPage() {
  const { token = '' } = useParams()
  const { user } = useSession()
  const loc = useLocation()
  const [artifact, setArtifact] = useState<LinkArtifact | null>(null)
  const [contentURL, setContentURL] = useState('')
  const [signedInRole, setSignedInRole] = useState<string | undefined>()
  const [err, setErr] = useState('')
  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  const handleRef = useRef<SandboxHandle | null>(null)

  useEffect(() => {
    let alive = true
    setErr('')
    api.openLink(token)
      .then((r) => {
        if (!alive) return
        setArtifact(r.artifact)
        setContentURL(r.content.url)
        setSignedInRole(r.my_role)
      })
      .catch((e) => {
        if (alive) setErr(e?.status === 404
          ? 'This link does not work any more. Its owner may have stopped sharing it, or replaced it with a new one.'
          : (e?.message ?? 'Error'))
      })
    return () => { alive = false }
  }, [token, user?.id])

  const fullViewer = !!user && !!signedInRole && !!artifact

  useEffect(() => {
    if (fullViewer || !artifact || !contentURL || !iframeRef.current) return
    handleRef.current?.destroy()
    handleRef.current = mountArtifact({ artifactId: artifact.id, iframe: iframeRef.current, url: contentURL })
    return () => { handleRef.current?.destroy(); handleRef.current = null }
  }, [artifact, contentURL, fullViewer])

  if (fullViewer) return <ArtifactPage artifactId={artifact!.id} />

  if (err) {
    return (
      <div className="grid h-full place-items-center px-6 text-center">
        <div className="max-w-md">
          <div className="mb-6 flex justify-center"><Logo /></div>
          <p className="text-ink-300">{err}</p>
          {!user && (
            <Link to="/auth" state={{ from: loc.pathname }} className="btn-outline mt-6">Sign in</Link>
          )}
        </div>
      </div>
    )
  }
  if (!artifact) return <div className="grid h-full place-items-center text-ink-500">Loading…</div>

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-ink-800 bg-ink-950 px-4 py-2.5">
        <a href="/" className="shrink-0" title="Falconia"><Logo size={24} /></a>
        <div className="min-w-0 flex-1">
          <h1 className="truncate font-medium text-ink-100">{artifact.title}</h1>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-500">
            <span>by {artifact.author_name}</span>
            <span>· v{artifact.latest_version}</span>
            <span>· updated {ago(artifact.updated_at)}</span>
            {artifact.agents.length > 0 && <span className="ml-1"><AgentStack agents={artifact.agents} /></span>}
          </div>
        </div>
        {!user && (
          <Link to="/auth" state={{ from: loc.pathname }} className="btn-outline shrink-0 text-sm">
            Sign in to comment
          </Link>
        )}
      </header>
      <div className="relative min-h-0 flex-1 bg-white">
        <iframe ref={iframeRef} className="artifact-frame" title={artifact.title} />
      </div>
    </div>
  )
}
