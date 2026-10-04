import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import Editor from '@monaco-editor/react'
import { api, type Artifact, type Visibility } from '../../lib/api'
import { useSession } from '../../lib/session'
import { configureMonaco } from '../../lib/monaco'
import { humanBytes } from '../../lib/format'
import { ChatPanel } from '../chat/ChatPanel'
import { useChat } from '../chat/useChat'
import { buildPreview, loadShim, STARTER, useContentBase } from './preview'
import { StudioHeader, type Pane } from './StudioHeader'
import { PaneButton, PaneHeader, PreviewFrame } from './Panes'
import { UploadDialog } from './UploadDialog'

configureMonaco()

/**
 * The Studio: ask, look, publish.
 *
 * Three panes — conversation, code, preview — because they are the three
 * moments of the same gesture. Whatever the model writes lands in the editor,
 * not in the archive: publishing stays an explicit act of the person in front
 * of the screen. The model can be your own agent (through `falconia agent`)
 * or any provider with your key.
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
  const [code, setCode] = useState(STARTER)
  const [message, setMessage] = useState('')
  const [shim, setShim] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [uploading, setUploading] = useState(false)
  const [chatOpen, setChatOpen] = useState(true)
  const [pane, setPane] = useState<Pane>('split')
  const [entryPath, setEntryPath] = useState('index.html')
  const [bundleFiles, setBundleFiles] = useState(0)
  // The code at the last save: tells whether there is something to lose.
  const [baseline, setBaseline] = useState(STARTER)

  const frameRef = useRef<HTMLIFrameElement | null>(null)
  const codeRef = useRef(code)
  codeRef.current = code

  const dirty = code.trim() !== baseline.trim()
  const lines = code.split('\n').length

  const chat = useChat({
    preamble: () => studioPreamble(codeRef.current),
    onDocument: (doc) => {
      setCode(doc)
      setTitle((t) => t.trim() || doc.match(/<title>([^<]{1,80})<\/title>/i)?.[1]?.trim() || t)
      // Whoever just asked for something wants to see its effect, not the source.
      setPane((p) => (p === 'code' ? 'split' : p))
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
        setCode(r.version.code)
        setBaseline(r.version.code)
      }
      if (r.version?.entry_path) setEntryPath(r.version.entry_path)
      setBundleFiles(r.files?.length ?? 0)
    }).catch((e) => alive && setErr(e.message))
    return () => { alive = false }
  }, [id])

  // Closing the tab with unpublished work is irreversible: the editor's state
  // lives nowhere until Publish is pressed.
  useEffect(() => {
    if (!dirty) return
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  // The preview updates lazily: rebuilding the document on every keystroke
  // would restart the artifact thirty times while typing one word.
  const preview = useMemo(() => buildPreview(shim, code), [shim, code])
  const paint = useCallback(() => {
    if (frameRef.current) frameRef.current.srcdoc = preview
  }, [preview])
  useEffect(() => {
    const t = setTimeout(paint, 400)
    return () => clearTimeout(t)
  }, [paint])

  async function save() {
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
    <div className="flex h-full flex-col">
      <StudioHeader
        editing={!!artifact}
        title={title} onTitle={setTitle}
        description={description} onDescription={setDescription}
        space={space} onSpace={setSpace}
        visibility={visibility} onVisibility={setVisibility}
        message={message} onMessage={setMessage}
        spaces={spaces}
        pane={pane} onPane={setPane}
        chatOpen={chatOpen} onToggleChat={() => setChatOpen((v) => !v)}
        dirty={dirty}
        busy={busy}
        onPublish={save}
        onUpload={() => setUploading(true)}
      />

      {bundleFiles > 1 && (
        <p className="shrink-0 border-b border-ink-800 bg-ink-900 px-4 py-2 text-xs text-ink-300">
          This artifact has {bundleFiles} files. You are editing <code className="text-ink-100">{entryPath}</code>;
          publishing keeps every other file as it is. Assets referenced by relative paths may not show in this preview.
        </p>
      )}

      {err && (
        <p className="shrink-0 border-b border-red-500/20 bg-red-500/10 px-4 py-2 text-sm text-red-400">
          {err}
        </p>
      )}

      <div className="flex min-h-0 flex-1">
        {chatOpen && (
          // Wide: the conversation sits next to the code. Narrow: it replaces
          // it, because three columns on a small screen are not three columns,
          // they are three strips.
          <div className="w-full shrink-0 border-r border-ink-800 lg:w-[380px]">
            <ChatPanel
              chat={chat}
              hint={<StudioHint />}
              suggestions={[
                'A vacation-days tracker, one row per person',
                'Add a bar chart of the days used',
                'Make it readable in light mode too',
              ]}
            />
          </div>
        )}

        <div className={`min-h-0 flex-1 ${chatOpen ? 'hidden lg:flex' : 'flex'}`}>
          {pane !== 'preview' && (
            <section className="flex min-h-0 min-w-0 flex-1 flex-col border-r border-ink-800">
              <PaneHeader label={entryPath}>
                <span className="text-[11px] text-ink-600">
                  {lines === 1 ? '1 line' : `${lines} lines`} · {humanBytes(new Blob([code]).size)}
                </span>
              </PaneHeader>
              <div className="min-h-0 flex-1">
                <Editor
                  height="100%"
                  defaultLanguage="html"
                  theme="vs-dark"
                  value={code}
                  onChange={(v) => setCode(v ?? '')}
                  options={{
                    minimap: { enabled: false },
                    fontSize: 13,
                    lineNumbersMinChars: 3,
                    wordWrap: 'on',
                    scrollBeyondLastLine: false,
                    padding: { top: 12, bottom: 12 },
                    renderLineHighlight: 'none',
                    overviewRulerLanes: 0,
                  }}
                />
              </div>
            </section>
          )}

          {pane !== 'code' && (
            <section className="flex min-h-0 min-w-0 flex-1 flex-col">
              <PaneHeader label="Preview">
                <PaneButton title="Open in a new tab" onClick={() => openStandalone(preview)}>
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6M15 3h6v6M10 14 21 3" />
                  </svg>
                </PaneButton>
              </PaneHeader>
              <PreviewFrame address={address} frameRef={frameRef} onReload={paint} />
            </section>
          )}
        </div>
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

function StudioHint() {
  return (
    <div className="card p-4">
      <p className="text-sm text-ink-300">Describe what you want.</p>
      <p className="mt-1 text-xs text-ink-500">
        The document appears in the editor and stays yours until you press Publish.
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
  const base = `You are working inside Falconia's artifact editor.

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
