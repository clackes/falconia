import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  api, ref as makeRef,
  type AccessRequest, type Anchor, type Artifact, type ArtifactVersion, type Thread,
} from '../../lib/api'
import { mountArtifact, type SandboxHandle } from '../../lib/sandbox'
import { useSession } from '../../lib/session'
import { ago, visibilityIcon, visibilityLabel } from '../../lib/format'
import { AgentBadge, AgentStack } from '../ui/AgentBadge'
import { ReviewPanel } from './ReviewPanel'
import { ShareDialog } from './ShareDialog'
import { VersionsDialog } from './VersionsDialog'
import { ContinueDialog } from './ContinueDialog'
import { DeleteDialog } from './DeleteDialog'
import { MoveDialog } from './MoveDialog'
import { RequestAccessDialog } from './RequestAccessDialog'
import { AccessRequestList } from './AccessRequestList'
import { Menu, MenuDivider, MenuItem } from '../ui/Menu'

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
  const [search] = useSearchParams()

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
  // ?connect=1 comes from "Connect your agent" on a shared link, after sign-in.
  const [dialog, setDialog] = useState<'share' | 'versions' | 'continue' | 'move' | 'delete' | 'request' | null>(
    () => search.get('connect') ? 'continue' : null)
  const [requests, setRequests] = useState<AccessRequest[]>([])
  const [myRequest, setMyRequest] = useState<AccessRequest | null>(null)
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

  // Owners see who is waiting for edit access; everyone else, their own request.
  useEffect(() => {
    if (!artifact || artifact.my_role === 'editor') return
    let alive = true
    api.accessRequests(artifact.id).then((r) => {
      if (!alive) return
      setRequests(r.requests ?? [])
      setMyRequest(r.mine ?? null)
    }).catch(() => { /* never block the artifact over this */ })
    return () => { alive = false }
  }, [artifact?.id, artifact?.my_role])

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

          {/* The one thing each role comes here to do stays in sight: owners
              and editors edit, viewers ask to. Everything else is in More. */}
          {canDeploy ? (
            <Link to={`/edit/${artifact.id}`} className="btn-ghost text-sm">Edit</Link>
          ) : myRequest?.status === 'pending' ? (
            <span className="chip text-xs" title="The owner has not answered yet">⏳ Edit access requested</span>
          ) : (
            <button className="btn-ghost text-sm" onClick={() => setDialog('request')}
              title={myRequest?.status === 'declined' ? 'Your last request was declined' : 'Ask the owner to let you edit'}>
              {myRequest?.status === 'declined' ? 'Request access again' : 'Request edit access'}
            </button>
          )}

          <button
            className={`btn text-sm ${showPanel ? 'bg-ink-700 text-white' : 'btn-ghost'}`}
            onClick={() => setShowPanel((v) => !v)}
          >
            Comments{openThreads > 0 ? ` · ${openThreads}` : ''}
          </button>

          <Menu label="More" title="More actions">
            <MenuItem onClick={() => setDialog('continue')}
              hint={canDeploy ? 'Edit it from Claude, Codex, Gemini…' : 'Let your AI read and comment'}>
              {canDeploy ? 'Continue with an agent' : 'Connect your agent'}
            </MenuItem>
            <MenuItem onClick={() => setDialog('versions')} hint="Every published version">History</MenuItem>
            {artifact.forkable && !canDeploy && (
              <MenuItem onClick={async () => {
                const r = await api.fork(artifactRef)
                nav(r.artifact.path)
              }} hint="Your own editable copy">Make a copy</MenuItem>
            )}
            {isOwner && (
              <>
                <MenuDivider />
                <MenuItem onClick={() => setDialog('move')} hint={`Now in ${artifact.space_name}`}>Move to…</MenuItem>
                <MenuItem onClick={() => setDialog('delete')} danger>Delete</MenuItem>
              </>
            )}
          </Menu>

          {isOwner && (
            <button className="btn-primary text-sm" onClick={() => setDialog('share')}>
              Share
            </button>
          )}
        </div>
      </header>

      {isOwner && requests.length > 0 && (
        <div className="border-b border-ink-800 bg-accent-500/5 px-3 py-2.5 sm:px-4">
          <AccessRequestList requests={requests}
            onDecided={(id) => setRequests((rs) => rs.filter((x) => x.id !== id))} />
        </div>
      )}

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
        <ContinueDialog artifact={artifact} onClose={() => setDialog(null)}
          onRequestAccess={myRequest?.status === 'pending' ? undefined : () => setDialog('request')} />
      )}
      {dialog === 'move' && (
        <MoveDialog artifact={artifact} onClose={() => setDialog(null)}
          onMoved={(a) => {
            setDialog(null)
            setArtifact({ ...artifact, ...a, my_role: artifact.my_role })
            // The /s/<space>/<slug> address changed with the space.
            if (!artifactId) nav(a.path, { replace: true })
          }} />
      )}
      {dialog === 'request' && (
        <RequestAccessDialog artifact={artifact} onClose={() => setDialog(null)}
          onSent={(r) => { setMyRequest(r); setDialog(null) }} />
      )}
      {dialog === 'delete' && (
        <DeleteDialog artifact={artifact} onClose={() => setDialog(null)} onDeleted={() => nav('/')} />
      )}
    </div>
  )
}
