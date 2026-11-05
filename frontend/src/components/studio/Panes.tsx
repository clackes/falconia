import type { ReactNode, RefObject } from 'react'

/**
 * A pane's header.
 *
 * Without it, code and preview were two rectangles split by a thin line, and
 * telling which was which meant looking inside. It costs thirty-two pixels and
 * is worth them.
 */
export function PaneHeader({ label, children }: { label: string; children?: ReactNode }) {
  return (
    <div className="flex h-9 shrink-0 items-center gap-2 border-b border-ink-800 bg-ink-950 px-3">
      <span className="text-xs font-medium uppercase tracking-wide text-ink-500">{label}</span>
      <div className="ml-auto flex items-center gap-1">{children}</div>
    </div>
  )
}

export function PaneButton({ title, onClick, children, disabled = false }: {
  title: string; onClick(): void; children: ReactNode; disabled?: boolean
}) {
  return (
    <button onClick={onClick} title={title} aria-label={title} disabled={disabled}
      className="grid h-6 w-6 place-items-center rounded text-ink-500 transition-colors hover:bg-ink-800 hover:text-ink-100
                 disabled:cursor-default disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-ink-500">
      {children}
    </button>
  )
}

/**
 * The preview, framed.
 *
 * An artifact almost always has a white background, and without a frame that
 * white against the edges reads as a broken panel instead of a page. The bar
 * with the future address does two things: it says this is a page, and it
 * shows right away the address that will be shared.
 */
export function PreviewFrame({ address, frameRef, onReload, busy = false }: {
  address: string
  frameRef: RefObject<HTMLIFrameElement>
  onReload(): void
  /** The agent is working: say so on the page it is about to replace. */
  busy?: boolean
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-ink-950 p-3">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-ink-700 bg-white shadow-2xl">
        <div className="flex h-8 shrink-0 items-center gap-2 border-b border-black/10 bg-[#f3f4f6] px-3">
          <span className="flex gap-1.5" aria-hidden>
            <span className="h-2.5 w-2.5 rounded-full bg-[#d1d5db]" />
            <span className="h-2.5 w-2.5 rounded-full bg-[#d1d5db]" />
            <span className="h-2.5 w-2.5 rounded-full bg-[#d1d5db]" />
          </span>
          <span className="mx-auto max-w-[70%] truncate rounded-md bg-white px-2.5 py-0.5 font-mono text-[11px] text-[#6b7280]">
            {address}
          </span>
          <button onClick={onReload} title="Reload the preview" aria-label="Reload the preview"
            className="grid h-5 w-5 place-items-center rounded text-[#9ca3af] hover:text-[#374151]">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
              <path d="M21 12a9 9 0 1 1-2.6-6.4M21 3v6h-6" />
            </svg>
          </button>
        </div>
        {busy && (
          <div role="status" className="flex h-1 shrink-0 overflow-hidden bg-accent-500/20">
            <span className="sr-only">The assistant is updating the page</span>
            <span className="h-full w-1/3 animate-pulse bg-accent-500" />
          </div>
        )}
        <iframe
          ref={frameRef}
          title="Preview"
          // An opaque origin here too: work-in-progress code must not be able
          // to touch the writer's session.
          sandbox="allow-scripts allow-forms allow-modals"
          className="min-h-0 flex-1 border-0 bg-white"
          style={{ colorScheme: 'light' }}
        />
      </div>
    </div>
  )
}
