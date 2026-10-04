import type { ReactNode } from 'react'
import { useEffect } from 'react'

/** A modal window with the bare minimum: Esc closes, clicking the backdrop closes. */
export function Dialog({ title, onClose, children, wide }: {
  title: string; onClose(): void; children: ReactNode; wide?: boolean
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-black/60 p-4"
      onClick={onClose}
      role="presentation"
    >
      <div
        className={`card w-full ${wide ? 'max-w-2xl' : 'max-w-md'} max-h-[85vh] overflow-y-auto`}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        <header className="flex items-center justify-between border-b border-ink-800 px-5 py-3">
          <h2 className="font-medium text-white">{title}</h2>
          <button className="btn-ghost !px-2" onClick={onClose} aria-label="Close">✕</button>
        </header>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}
