import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { Space, Visibility } from '../../lib/api'
import { visibilityHint, visibilityLabel } from '../../lib/format'
import { Menu, MenuDivider, MenuItem, MenuLabel } from '../ui/Menu'

export type Pane = 'split' | 'code' | 'preview'

type Props = {
  editing: boolean
  title: string; onTitle(v: string): void
  description: string; onDescription(v: string): void
  space: string; onSpace(v: string): void
  visibility: Visibility; onVisibility(v: Visibility): void
  message: string; onMessage(v: string): void
  spaces: Space[]
  pane: Pane; onPane(p: Pane): void
  chatOpen: boolean; onToggleChat(): void
  dirty: boolean
  busy: boolean
  onPublish(): void
  onUpload(): void
  onReloadPreview(): void
  onOpenPreview(): void
  onDownload(): void
  onDiscard(): void
  /** Where the published artifact lives, when editing one. */
  publishedPath?: string
}

/**
 * The Studio's toolbar.
 *
 * It only holds what gets touched all the time: the title, which pane to look
 * at, and the publish button. Space, access and description are chosen once
 * and live behind the button, because keeping them always open filled the top
 * of the page with empty fields and made the Studio look like a form. The
 * rest is grouped like in any editor: View for what you see, File for what
 * you do with the document.
 */
export function StudioHeader(p: Props) {
  const [open, setOpen] = useState(false)

  return (
    <header className="flex shrink-0 items-center gap-3 border-b border-ink-800 px-3 py-2.5">
      <button
        onClick={p.onToggleChat}
        aria-pressed={p.chatOpen}
        title={p.chatOpen ? 'Hide the conversation' : 'Show the conversation'}
        className={`grid h-8 w-8 shrink-0 place-items-center rounded-lg transition-colors ${
          p.chatOpen ? 'bg-accent-500 text-ink-950' : 'bg-ink-800 text-ink-300 hover:text-white'}`}
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="M21 11.5a8.4 8.4 0 0 1-9 8.4 8.4 8.4 0 0 1-3.8-.9L3 21l1.9-5.7A8.4 8.4 0 0 1 12 3a8.4 8.4 0 0 1 9 8.5z" />
        </svg>
      </button>

      <TitleField value={p.title} onChange={p.onTitle} />

      {p.dirty && (
        <span className="flex shrink-0 items-center gap-1.5 text-xs text-ink-500" title="There are unpublished changes">
          <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
          <span className="hidden sm:inline">unpublished</span>
        </span>
      )}

      <div className="ml-auto flex shrink-0 items-center gap-2">
        <ViewMenu {...p} />
        <FileMenu {...p} />

        <div className="relative">
          <div className="flex overflow-hidden rounded-lg">
            <button className="btn-primary !rounded-none !rounded-l-lg" onClick={p.onPublish} disabled={p.busy}>
              {p.busy ? '…' : p.editing ? 'Publish version' : 'Publish'}
            </button>
            <button
              className="btn-primary !rounded-none !rounded-r-lg !px-2 border-l border-ink-950/20"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              title="Publishing options"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3">
                <path d="M6 9l6 6 6-6" />
              </svg>
            </button>
          </div>
          {open && <PublishPanel {...p} onClose={() => setOpen(false)} />}
        </div>
      </div>
    </header>
  )
}

/**
 * The title is written where it is later read.
 *
 * It looks like a heading until touched: a bordered field at the top of the
 * page asks to be filled in before starting, which is not what to ask of
 * someone still figuring out what to build.
 */
function TitleField({ value, onChange }: { value: string; onChange(v: string): void }) {
  return (
    <input
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder="Untitled artifact"
      aria-label="Artifact title"
      className="min-w-0 max-w-sm flex-1 truncate rounded-lg border border-transparent bg-transparent px-2 py-1
                 text-[15px] font-medium text-ink-100 placeholder:text-ink-500
                 hover:border-ink-700 focus:border-accent-500 focus:bg-ink-900 focus:outline-none"
    />
  )
}

const panes: [Pane, string, string][] = [
  ['code', 'Code', 'M8 6 2 12l6 6M16 6l6 6-6 6'],
  ['split', 'Code + preview', 'M12 3v18M3 3h18v18H3z'],
  ['preview', 'Preview', 'M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z'],
]

function PaneIcon({ pane }: { pane: Pane }) {
  const path = panes.find(([id]) => id === pane)![2]
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d={path} />
      {pane === 'preview' && <circle cx="12" cy="12" r="3" />}
    </svg>
  )
}

/** What is on screen: which panes, the conversation, the preview itself. */
function ViewMenu(p: Props) {
  const current = panes.find(([id]) => id === p.pane)!
  return (
    <Menu title="Choose what to see"
      label={<><PaneIcon pane={p.pane} /><span className="hidden lg:inline">{current[1]}</span></>}>
      <MenuLabel>Panes</MenuLabel>
      {panes.map(([id, label]) => (
        <MenuItem key={id} checked={p.pane === id} onClick={() => p.onPane(id)}>{label}</MenuItem>
      ))}
      <MenuDivider />
      <MenuItem onClick={p.onToggleChat}>{p.chatOpen ? 'Hide the conversation' : 'Show the conversation'}</MenuItem>
      <MenuItem onClick={p.onReloadPreview} hint="Restart the artifact">Reload preview</MenuItem>
      <MenuItem onClick={p.onOpenPreview} hint="Full window, still sandboxed">Open preview in a new tab</MenuItem>
    </Menu>
  )
}

/** What to do with the document, besides publishing it. */
function FileMenu(p: Props) {
  const nav = useNavigate()
  return (
    <Menu label="File" title="File actions">
      {p.publishedPath && (
        <MenuItem onClick={() => nav(p.publishedPath!)} hint="With its comments and history">
          Open the published artifact
        </MenuItem>
      )}
      {!p.editing && (
        <MenuItem onClick={p.onUpload} hint="A whole site or app, with its assets">Upload a folder</MenuItem>
      )}
      <MenuItem onClick={p.onDownload} hint="This document as a .html file">Download HTML</MenuItem>
      <MenuDivider />
      <MenuItem onClick={p.onDiscard} danger disabled={!p.dirty}
        hint={p.editing ? 'Back to the last published version' : 'Back to the empty starter'}>
        Discard changes
      </MenuItem>
    </Menu>
  )
}

/** The choices made once, behind the button that uses them. */
function PublishPanel(p: Props & { onClose(): void }) {
  const ref = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const away = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) p.onClose()
    }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') p.onClose() }
    // Registered on the next tick: the click that opened the panel is still
    // bubbling up, and would close it immediately.
    const t = setTimeout(() => document.addEventListener('mousedown', away))
    document.addEventListener('keydown', esc)
    return () => {
      clearTimeout(t)
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', esc)
    }
  }, [p])

  return (
    <div ref={ref}
      className="absolute right-0 top-full z-40 mt-2 w-80 rounded-xl border border-ink-700 bg-ink-900 p-4 shadow-2xl">
      {p.editing ? (
        <Field label="What changed">
          <input className="input" autoFocus placeholder="fixed the numbers"
            value={p.message} onChange={(e) => p.onMessage(e.target.value)} />
          <p className="mt-1.5 text-xs text-ink-500">
            It goes into the version history, next to your name.
          </p>
        </Field>
      ) : (
        <div className="space-y-4">
          <Field label="Where">
            <select className="input" value={p.space} onChange={(e) => p.onSpace(e.target.value)}>
              {p.spaces.map((s) => (
                <option key={s.id} value={s.slug}>
                  {s.kind === 'org' ? `👥 ${s.name}` : `👤 ${s.slug}`}
                </option>
              ))}
            </select>
          </Field>

          <Field label="General access">
            <div className="space-y-1.5">
              {(['private', 'org', 'link'] as Visibility[]).filter((v) => v !== 'org' || p.spaces.find((s) => s.slug === p.space)?.kind === 'org').map((v) => (
                <label key={v}
                  className={`flex cursor-pointer gap-2.5 rounded-lg border p-2.5 transition-colors ${
                    p.visibility === v ? 'border-accent-500 bg-ink-800' : 'border-ink-700 hover:border-ink-500'}`}>
                  <input type="radio" name="vis" className="mt-0.5" checked={p.visibility === v}
                    onChange={() => p.onVisibility(v)} />
                  <span className="min-w-0">
                    <span className="block text-sm text-ink-100">{visibilityLabel[v]}</span>
                    <span className="block text-xs leading-snug text-ink-500">{visibilityHint[v]}</span>
                  </span>
                </label>
              ))}
            </div>
          </Field>

          <Field label="Description">
            <textarea className="input min-h-[60px] resize-y" placeholder="What it is for (optional)"
              value={p.description} onChange={(e) => p.onDescription(e.target.value)} />
          </Field>
        </div>
      )}

      <button className="btn-primary mt-4 w-full" disabled={p.busy}
        onClick={() => { p.onClose(); p.onPublish() }}>
        {p.busy ? '…' : p.editing ? 'Publish version' : 'Publish'}
      </button>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs uppercase tracking-wide text-ink-500">{label}</span>
      {children}
    </label>
  )
}
