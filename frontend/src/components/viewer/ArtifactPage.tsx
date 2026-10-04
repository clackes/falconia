import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  api, ref as makeRef,
  type Anchor, type Artifact, type ArtifactVersion, type Thread,
} from '../../lib/api'
import { mountArtifact, type SandboxHandle } from '../../lib/sandbox'
import { useSession } from '../../lib/session'
import { ago, visibilityIcon, visibilityLabel } from '../../lib/format'
import { AgentBadge, AgentStack } from '../ui/AgentBadge'
import { ReviewPanel } from './ReviewPanel'
import { ShareDialog } from './ShareDialog'
import { VersionsDialog } from './VersionsDialog'
import { ContinueDialog } from './ContinueDialog'

/**
 * The viewer: the artifact full screen, comments on the side.
 *
 * The artifact comes from another origin through a signed URL that lasts a few
 * hours. That URL is requested here and is never part of the address people
 * share: what goes in a chat is /s/space/slug (sign-in required) or the
 * secret /p/ link (anyone), and both last forever.
 */
export function ArtifactPage({ artifactId }: { artifactId?: string } = {}) {
  const { space, slug, id } = useParams()
  const { user } = useSession()
  const nav = useNavigate()

  const artifactRef = artifactId ?? id ?? makeRef(space!, slug!)

  const [artifact, setArtifact] = useState<Artifact | null>(null)
  const [version, setVersion] = useState<ArtifactVersion | null>(null)
  const [pickedVersion, setPickedVersion] = useState<number | undefined>()
  const [threads, setThreads] = useState<Thread[]>([])
  const [orphans, setOrphans] = useState<Set<string>>(new Set())
  const [focused, setFocused] = useState<string | null>(null)
  const [pendingAnchor, setPendingAnchor] = useState<Anchor | null>(null)
  const [picking, setPicking] = useState(false)
  const [showPanel, setShowPanel] = useState(() => window.innerWidth >= 768)
  const [dialog, setDialog] = useState<'share' | 'versions' | 'continue' | null>(null)
  const [menu, setMenu] = useState(false)
  const [err, setErr] = useState('')

  const iframeRef = useRef<HTMLIFrameElement | null>(null)
  const handleRef = useRef<SandboxHandle | null>(null)
  // The latest threads, for an iframe that mounts after they arrived: the
  // effect below only fires on change, and comments usually load first.
  const threadsRef = useRef<Thread[]>([])
  threadsRef.current = threads

  const loadThreads = useCallback(async () => {
    try {
      const r = await api.threads(artifactRef)
      setThreads(r.threads)
    } catch { /* comments must not prevent seeing the artifact */ }
  }, [artifactRef])

  // Metadata and content.
  useEffect(() => {
    let alive = true
    setErr('')
    ;(async () => {
      try {
        const meta = await api.artifact(artifactRef, pickedVersion)
        if (!alive) return
        setArtifact(meta.artifact)
        setVersion(meta.version ?? null)
      } catch (e: any) {
        if (alive) setErr(e?.status === 404
          ? 'This artifact does not exist, or you do not have access to it.'
          : (e?.message ?? 'Error'))
      }
    })()
    void loadThreads()
    return () => { alive = false }
  }, [artifactRef, pickedVersion, loadThreads])

  // Mount the iframe. It remounts when the shown version changes.
  useEffect(() => {
    const iframe = iframeRef.current
    if (!iframe || !artifact) return
    let alive = true
    ;(async () => {
      try {
        const { url } = await api.contentURL(artifactRef, pickedVersion)
        if (!alive || !iframeRef.current) return
        handleRef.current?.destroy()
        handleRef.current = mountArtifact({
          artifactId: artifact.id,
          iframe: iframeRef.current,
          url,
          // Viewers who cannot fork leave no trace in their browser: the
          // artifact runs, but its state does not outlive the tab.
          readOnly: artifact.my_role === 'viewer' && !artifact.forkable,
          onPick: (anchor) => { setPendingAnchor(anchor); setPicking(false) },
          onFocusThread: (tid) => { setFocused(tid); setShowPanel(true) },
          onOrphans: (ids) => setOrphans(new Set(ids)),
        })
        handleRef.current.showThreads(threadsRef.current)
      } catch (e: any) {
        if (alive) setErr(e?.message ?? 'Cannot load the content')
      }
    })()
    return () => { alive = false; handleRef.current?.destroy(); handleRef.current = null }
  }, [artifact?.id, artifactRef, pickedVersion])

  // The pins inside the iframe follow the threads.
  useEffect(() => { handleRef.current?.showThreads(threads) }, [threads])
  useEffect(() => { handleRef.current?.setCommentMode(picking) }, [picking])

  if (err) {
    return (
      <div className="grid h-full place-items-center px-6 text-center">
        <div>
          <p className="text-ink-300">{err}</p>
          <Link to="/" className="btn-outline mt-4">Back to all artifacts</Link>
        </div>
      </div>
    )
  }
  if (!artifact) {
    return <div className="grid h-full place-items-center text-ink-500">Loading…</div>
  }

  const isOwner = artifact.my_role === 'owner'
  const canDeploy = isOwner || artifact.my_role === 'editor'
  const viewingOld = pickedVersion != null && pickedVersion !== artifact.latest_version
  const openThreads = threads.filter((t) => t.status === 'open').length

  return (
    <div className="flex h-full flex-col">
      <header className="relative flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-ink-800 bg-ink-950 px-3 py-2.5 sm:px-4">
        <Link to="/" className="btn-ghost !px-2" title="Back to all artifacts">←</Link>

        <div className="min-w-[55%] flex-1 sm:min-w-0">
          <div className="flex items-center gap-2">
            <h1 className="truncate font-medium text-ink-100">{artifact.title}</h1>
            <span className="chip hidden shrink-0 sm:inline-flex" title={visibilityLabel[artifact.visibility]}>
              {visibilityIcon[artifact.visibility]} {visibilityLabel[artifact.visibility]}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-500">
            <Link to={`/s/${artifact.space_slug}`} className="hover:text-ink-300">
              {artifact.space_slug}
            </Link>
            <span>· {artifact.author_name}</span>
            <span>· v{version?.version_number ?? artifact.latest_version}</span>
            <span>· {ago(artifact.updated_at)}</span>
            {version?.agent !== undefined && (
              <span className="hidden sm:inline-flex"><AgentBadge agent={version.agent} size="xs" /></span>
            )}
          </div>
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {artifact.agents.length > 1 && (
            <span className="hidden lg:inline-flex" title="Agents that worked on this artifact">
              <AgentStack agents={artifact.agents} />
            </span>
          )}
          {/* Secondary actions: inline on wide screens, behind ⋯ on phones. */}
          <div className={`${menu ? 'flex' : 'hidden'} absolute right-3 top-14 z-40 w-52 flex-col gap-1 rounded-xl border border-ink-700 bg-ink-900 p-2 shadow-2xl sm:static sm:flex sm:w-auto sm:flex-row sm:gap-2 sm:border-0 sm:bg-transparent sm:p-0 sm:shadow-none`}
            onClick={() => setMenu(false)}>
            <button className="btn-ghost justify-start text-sm" onClick={() => setDialog('versions')}>
              History
            </button>
            {canDeploy && (
              <button className="btn-ghost justify-start text-sm" onClick={() => setDialog('continue')}
                title="Edit this artifact with any AI agent">
                Continue with…
              </button>
            )}
            {canDeploy && (
              <Link to={`/edit/${artifact.id}`} className="btn-ghost justify-start text-sm">Edit</Link>
            )}
            {artifact.forkable && !canDeploy && (
              <button className="btn-ghost justify-start text-sm" onClick={async () => {
                const r = await api.fork(artifactRef)
                nav(r.artifact.path)
              }}>Make a copy</button>
            )}
          </div>
          <button className="btn-ghost !px-2.5 text-sm sm:hidden" onClick={() => setMenu((v) => !v)}
            aria-label="More actions" aria-expanded={menu}>⋯</button>
          <button
            className={`btn text-sm ${showPanel ? 'bg-ink-700 text-white' : 'btn-ghost'}`}
            onClick={() => setShowPanel((v) => !v)}
          >
            Comments{openThreads > 0 ? ` · ${openThreads}` : ''}
          </button>
          {isOwner && (
            <button className="btn-primary text-sm" onClick={() => setDialog('share')}>
              Share
            </button>
          )}
        </div>
      </header>

      {viewingOld && (
        <div className="flex items-center gap-3 bg-amber-500/10 px-4 py-2 text-sm text-amber-200">
          You are viewing version {pickedVersion} of {artifact.latest_version}.
          <button className="underline" onClick={() => setPickedVersion(undefined)}>
            Go to the latest
          </button>
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 bg-white">
          <iframe ref={iframeRef} className="artifact-frame" title={artifact.title} />
          {picking && (
            <div className="pointer-events-none absolute inset-x-0 top-3 flex justify-center">
              <span className="rounded-full bg-accent-500 px-3 py-1.5 text-sm font-medium text-ink-950 shadow-lg">
                Click the spot you want to comment on
              </span>
            </div>
          )}
        </div>

        {showPanel && (
          <ReviewPanel
            artifactRef={artifactRef}
            threads={threads}
            orphanIds={orphans}
            focusedId={focused}
            pendingAnchor={pendingAnchor}
            picking={picking}
            canModerate={canDeploy}
            currentUserId={user?.id ?? ''}
            onRefresh={loadThreads}
            onFocus={setFocused}
            onCancelPending={() => { setPendingAnchor(null); setPicking(false) }}
            onStartPicking={() => { setPendingAnchor(null); setPicking((v) => !v) }}
            onClose={() => setShowPanel(false)}
          />
        )}
      </div>

      {dialog === 'share' && (
        <ShareDialog
          artifact={artifact}
          onClose={() => setDialog(null)}
          onChanged={(a) => setArtifact({ ...artifact, ...a, my_role: artifact.my_role })}
        />
      )}
      {dialog === 'versions' && (
        <VersionsDialog
          artifactRef={artifactRef}
          current={version?.version_number ?? artifact.latest_version}
          onPick={(n) => { setPickedVersion(n); setDialog(null) }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === 'continue' && (
        <ContinueDialog artifact={artifact} onClose={() => setDialog(null)} />
      )}
    </div>
  )
}
