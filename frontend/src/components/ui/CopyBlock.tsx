import { useState } from 'react'
import { copyText } from '../../lib/format'

/** A block of text to copy: a command, a config file, a link. */
export function CopyBlock({ text, label, mono = true, wrap = false }: {
  text: string; label?: string; mono?: boolean; wrap?: boolean
}) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="relative">
      {label && <div className="mb-1 text-[11px] uppercase tracking-wider text-ink-500">{label}</div>}
      <pre className={`${wrap ? `whitespace-pre-wrap ${mono ? 'break-all' : 'break-words'}` : 'overflow-x-auto'} rounded-lg border border-ink-800 bg-ink-950 p-3 pr-20 text-ink-300 ${mono ? 'font-mono text-xs' : 'font-sans text-sm leading-relaxed'}`}>
        {text}
      </pre>
      <button
        className="btn-ghost absolute right-1.5 top-1.5 !px-2 !py-1 text-xs"
        style={label ? { top: '1.6rem' } : undefined}
        onClick={async () => {
          if (await copyText(text)) {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          }
        }}
      >
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  )
}

/** A one-line link with a copy button, for share links. */
export function CopyLine({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="flex items-center gap-2">
      <input readOnly value={text} className="input font-mono text-xs" onFocus={(e) => e.target.select()} />
      <button
        className="btn-primary shrink-0"
        onClick={async () => {
          if (await copyText(text)) {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          }
        }}
      >
        {copied ? 'Copied ✓' : 'Copy link'}
      </button>
    </div>
  )
}
