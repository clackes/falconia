import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'

const CloseCtx = createContext<() => void>(() => {})

/**
 * A dropdown: one button, a short list of actions under it.
 *
 * Secondary actions live here so a toolbar keeps only what is pressed all the
 * time. Clicking away or Esc closes it, and arrow keys move between items.
 */
export function Menu({ label, title, children, align = 'right', className = 'btn-ghost text-sm' }: {
  label: ReactNode; title?: string; children: ReactNode
  align?: 'left' | 'right'; className?: string
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const keys = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { setOpen(false); return }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
      const items = Array.from(ref.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]:not([disabled])') ?? [])
      if (!items.length) return
      e.preventDefault()
      const i = items.indexOf(document.activeElement as HTMLElement)
      const next = e.key === 'ArrowDown' ? (i + 1) % items.length : (i - 1 + items.length) % items.length
      items[next].focus()
    }
    document.addEventListener('mousedown', away)
    document.addEventListener('keydown', keys)
    return () => {
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', keys)
    }
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button className={`${className} gap-1.5`} onClick={(e) => { e.preventDefault(); e.stopPropagation(); setOpen((v) => !v) }}
        aria-haspopup="menu" aria-expanded={open} title={title}>
        {label}
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" aria-hidden>
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      {open && (
        <div role="menu"
          className={`absolute ${align === 'right' ? 'right-0' : 'left-0'} top-full z-50 mt-1.5 min-w-[13rem] rounded-xl border border-ink-700 bg-ink-900 p-1.5 shadow-2xl`}
          onClick={(e) => e.stopPropagation()}>
          <CloseCtx.Provider value={() => setOpen(false)}>{children}</CloseCtx.Provider>
        </div>
      )}
    </div>
  )
}

export function MenuItem({ onClick, children, hint, danger, checked, disabled }: {
  onClick(): void; children: ReactNode; hint?: string
  danger?: boolean; checked?: boolean; disabled?: boolean
}) {
  const close = useContext(CloseCtx)
  return (
    <button role={checked === undefined ? 'menuitem' : 'menuitemradio'} aria-checked={checked} disabled={disabled}
      onClick={(e) => { e.preventDefault(); close(); onClick() }}
      className={`flex w-full items-start gap-2 rounded-lg px-2.5 py-2 text-left text-sm transition-colors
        focus:outline-none disabled:opacity-40 ${danger
          ? 'text-red-400 hover:bg-red-500/10 focus:bg-red-500/10'
          : 'text-ink-100 hover:bg-ink-800 focus:bg-ink-800'}`}>
      {checked !== undefined && (
        <span className="w-3.5 shrink-0 text-accent-400" aria-hidden>{checked ? '✓' : ''}</span>
      )}
      <span className="min-w-0 flex-1">
        <span className="block">{children}</span>
        {hint && <span className="block text-xs text-ink-500">{hint}</span>}
      </span>
    </button>
  )
}

export function MenuDivider() {
  return <div className="my-1 h-px bg-ink-800" role="separator" />
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className="px-2.5 pb-1 pt-1.5 text-[11px] uppercase tracking-wider text-ink-500">{children}</div>
}
