import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type Artifact, type Visibility } from '../../lib/api'
import { useSession } from '../../lib/session'
import { ChatPanel } from '../chat/ChatPanel'
import { useChat } from '../chat/useChat'
import { buildPreview, loadShim, STARTER, useContentBase } from './preview'
import { StudioHeader, type View } from './StudioHeader'
import { PaneButton, PaneHeader, PreviewFrame } from './Panes'
import { UploadDialog } from './UploadDialog'

/**
 * The Studio: ask, look, publish.
 *
 * Two panes — the conversation with the agent and the live preview — and no
 * code editor. People come here to describe what they want and watch it take
 * shape; the source is the agent's business. Whoever wants to touch the code
 * by hand has `falconia pull` and any editor they like, which is the
 * no-lock-in promise anyway.
 *
 * Whatever the model writes lands in the preview, not in the archive:
 * publishing stays an explicit act of the person in front of the screen. Each
 * answer is kept in a local history, so a change that made things worse is one
 * click away from undone — the editor used to be the only way back.
 */
export function CreatorStudio() {
  const { id } = useParams()
  const nav = useNavigate()
  const { spaces, personalSpace } = useSession()
  const contentBase = useContentBase()

  const [artifact, setArtifact] = useState<Artifact | null>(null)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [space, setSpace] = useState('')
  const [visibility, setVisibility] = useState<Visibility>('private')
  const [message, setMessage] = useState('')
  const [shim, setShim] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [uploading, setUploading] = useState(false)
  // On a narrow screen only one pane fits: this says which.
  const [view, setView] = useState<View>('chat')
  const [entryPath, setEntryPath] = useState('index.html')
  const [bundleFiles, setBundleFiles] = useState(0)

  // history[cursor] is the document on screen. Every answer from the agent is
  // appended; undo and redo move the cursor; a new answer after an undo drops
  // the redo branch, like any editor's history.
  const [history, setHistory] = useState<string[]>([STARTER])
  const [cursor, setCursor] = useState(0)
  // The document at the last save: tells whether there is something to lose.
  const [baseline, setBaseline] = useState(STARTER)
  const code = history[cursor]

  const frameRef = useRef<HTMLIFrameElement | null>(null)
  const codeRef = useRef(code)
  codeRef.current = code
  const cursorRef = useRef(cursor)
  cursorRef.current = cursor

  const dirty = code.trim() !== baseline.trim()
  const isEmpty = code.trim() === STARTER.trim()

  const chat = useChat({
    preamble: () => studioPreamble(codeRef.current),
    onDocument: (doc) => {
      setHistory((h) => [...h.slice(0, cursorRef.current + 1), doc])
      setCursor(cursorRef.current + 1)
      setTitle((t) => t.trim() || doc.match(/<title>([^<]{1,80})<\/title>/i)?.[1]?.trim() || t)
      // Whoever just asked for something wants to see its effect.
      setView('preview')
    },
  })
  useEffect(() => { void loadShim(contentBase).then(setShim) }, [contentBase])
  useEffect(() => { if (!space && personalSpace) setSpace(personalSpace.slug) }, [personalSpace, space])

  useEffect(() => {
    if (!id) return
    let alive = true
    api.artifact(id).then((r) => {
      if (!alive) return
      setArtifact(r.artifact)
      setTitle(r.artifact.title)
      setDescription(r.artifact.description)
      setSpace(r.artifact.space_slug)
      setVisibility(r.artifact.visibility)
      if (r.version?.code) {
        setHistory([r.version.code])
        setCursor(0)
        setBaseline(r.version.code)
      }
      if (r.version?.entry_path) setEntryPath(r.version.entry_path)
      setBundleFiles(r.files?.length ?? 0)
      // Editing starts from what exists: show it.
      setView('preview')
    }).catch((e) => alive && setErr(e.message))
    return () => { alive = false }
  }, [id])

  // Closing the tab with unpublished work is irreversible: the Studio's state
  // lives nowhere until Publish is pressed.
  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  const preview = useMemo(() => buildPreview(shim, code), [shim, code])
  const paint = useCallback(() => {
    if (frameRef.current) frameRef.current.srcdoc = preview
  }, [preview])
  useEffect(() => { paint() }, [paint])

  const canUndo = cursor > 0
  const canRedo = cursor < history.length - 1

  async function save() {
    if (isEmpty && !artifact) {
      setErr('Nothing to publish yet: describe what you want to the assistant first.')
      return
    }
    setBusy(true)
    setErr('')
    try {
      if (artifact) {
        await api.commit(artifact.id, code, message.trim() || 'edit')
        setBaseline(code)
        setMessage('')
        nav(artifact.path)
      } else {
        const r = await api.create({
          title: title.trim() || 'Untitled',
          description, code, space, visibility,
        })
        setBaseline(code)
        nav(r.artifact.path)
      }
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  const address = artifact
    ? artifact.path
    : `/s/${space || '…'}/${slugify(title) || '…'}`

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <StudioHeader
        editing={!!artifact}
        title={title} onTitle={setTitle}
        description={description} onDescription={setDescription}
        space={space} onSpace={setSpace}
        visibility={visibility} onVisibility={setVisibility}
        message={message} onMessage={setMessage}
        spaces={spaces}
        view={view} onView={setView}
        dirty={dirty}
        busy={busy}
        onPublish={save}
        onUpload={() => setUploading(true)}
      />

      {bundleFiles > 1 && (
        <p className="shrink-0 border-b border-ink-800 bg-ink-900 px-4 py-2 text-xs text-ink-300">
          This artifact has {bundleFiles} files. The assistant edits <code className="text-ink-100">{entryPath}</code>;
          publishing keeps every other file as it is. Assets referenced by relative paths may not show in this preview.
        </p>
      )}

      {err && (
        <p role="alert" className="shrink-0 border-b border-red-500/20 bg-red-500/10 px-4 py-2 text-sm text-red-400">
          {err}
        </p>
      )}

      <div className="flex min-h-0 flex-1">
        {/* Wide: conversation and preview side by side. Narrow: one at a
            time, switched from the header, because two columns on a phone
            are two unreadable strips. */}
        <div className={`min-h-0 w-full shrink-0 border-ink-800 lg:block lg:w-[420px] lg:border-r ${
          view === 'chat' ? 'block' : 'hidden'}`}>
          <ChatPanel
            chat={chat}
            hint={<StudioHint editing={!!artifact} />}
            suggestions={artifact ? [
              'Make it readable in light mode too',
              'Add a short summary at the top',
              'Make it work better on a phone',
            ] : [
              'A vacation-days tracker, one row per person',
              'A one-page project status report',
              'A quiz with ten questions and a score at the end',
            ]}
          />
        </div>

        <section className={`min-h-0 min-w-0 flex-1 flex-col lg:flex ${view === 'preview' ? 'flex' : 'hidden'}`}>
          <PaneHeader label="Preview">
            <PaneButton title="Undo the last change" onClick={() => canUndo && setCursor((c) => c - 1)} disabled={!canUndo}>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="M9 14 4 9l5-5M4 9h11a5 5 0 0 1 0 10h-3" />
              </svg>
            </PaneButton>
            <PaneButton title="Redo" onClick={() => canRedo && setCursor((c) => c + 1)} disabled={!canRedo}>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <path d="m15 14 5-5-5-5M20 9H9a5 5 0 0 0 0 10h3" />
              </svg>
            </PaneButton>
            <PaneButton title="Open in a new tab" onClick={() => openStandalone(preview)}>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6M15 3h6v6M10 14 21 3" />
              </svg>
            </PaneButton>
          </PaneHeader>
          <PreviewFrame address={address} frameRef={frameRef} onReload={paint} busy={chat.busy} />
        </section>
      </div>

      {uploading && (
        <UploadDialog
          defaultSpace={space}
          onClose={() => setUploading(false)}
          onDone={(path) => nav(path)}
        />
      )}
    </div>
  )
}

/**
 * openStandalone opens the preview in a separate tab.
 *
 * It goes through a blob rather than document.write so the work-in-progress
 * code does not get the application's origin: a blob: URL has its own origin,
 * and what runs inside cannot see the writer's session.
 */
function openStandalone(html: string) {
  const url = URL.createObjectURL(new Blob([html], { type: 'text/html' }))
  window.open(url, '_blank', 'noopener,noreferrer')
  // Revoking right away would break the tab just opened; a minute is enough
  // for it to load without keeping the blob for the whole session.
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}

function slugify(s: string): string {
  return s.toLowerCase().trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
}

function StudioHint({ editing }: { editing: boolean }) {
  return (
    <div className="card p-4">
      <p className="text-sm text-ink-300">
        {editing ? 'Say what to change.' : 'Describe what you want.'}
      </p>
      <p className="mt-1 text-xs text-ink-500">
        The result appears in the preview and stays yours until you press Publish.
        Every change can be undone from the preview.
      </p>
    </div>
  )
}

/**
 * The instructions sent with every request made from the Studio.
 *
 * They serve one purpose: getting a document back instead of having it
 * published. An agent with the CLI at hand, left alone, would publish by
 * itself — useful from a terminal, wrong here.
 */
function studioPreamble(current: string): string {
  const base = `You are working inside Falconia's artifact Studio.

ALWAYS answer with ONE complete, self-contained HTML document, inside an \`\`\`html block.
No network requests, no scripts from CDNs: inline everything.
It runs in an opaque-origin iframe, so no cookies or window.parent: to persist data use the
global Falconia object (Falconia.setSchemaVersion, Falconia.onReady, Falconia.save, Falconia.load).
Make it good: it must work on a phone screen, read well in light and dark mode, and be
keyboard-navigable.

Do NOT publish, do NOT create files, do NOT use the command line: the person reading takes care
of the rest. Write one line of explanation, then the document.`

  const trimmed = current.trim()
  if (!trimmed || trimmed === STARTER.trim()) return base

  return `${base}

The current document, to modify, is:

\`\`\`html
${current}
\`\`\``
}
