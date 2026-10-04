import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  AI_PROVIDERS, loadProviderKey, saveProviderKey, setSelectedProvider,
  type AIProviderId,
} from '../../lib/ai-providers'
import { Dialog } from '../viewer/Dialog'
import { AgentPanel } from './AgentPanel'
import type { useChat } from './useChat'

type Chat = ReturnType<typeof useChat>

/**
 * The conversation column.
 *
 * It draws what the hook produces and nothing else: the logic lives in
 * useChat, so the same chat can sit next to the editor without being
 * rewritten.
 */
export function ChatPanel({ chat, hint, suggestions = [] }: {
  chat: Chat
  hint?: React.ReactNode
  /** Example prompts: clicking one fills the field, it does not send. */
  suggestions?: string[]
}) {
  const nav = useNavigate()
  const [input, setInput] = useState('')
  const [settings, setSettings] = useState(false)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const boxRef = useRef<HTMLTextAreaElement | null>(null)

  // The box grows with the text up to a cap: a single line means typing
  // blind, a tall fixed box steals space from the conversation.
  useEffect(() => {
    const el = boxRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 160) + 'px'
  }, [input])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [chat.msgs, chat.tools])

  const submit = (e: React.FormEvent | React.KeyboardEvent) => {
    e.preventDefault()
    if (chat.runtime === 'key' && !chat.hasKey) { setSettings(true); return }
    const text = input
    setInput('')
    void chat.send(text)
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex h-9 shrink-0 items-center gap-2 border-b border-ink-800 bg-ink-950 px-3">
        <span className="text-xs font-medium uppercase tracking-wide text-ink-500">Assistant</span>
        <div className="ml-auto flex items-center gap-1.5">
          {chat.runtime === 'key' && (
            <button className="rounded px-1.5 py-0.5 text-[11px] text-ink-500 hover:text-ink-100"
              onClick={() => setSettings(true)}>
              {chat.hasKey ? chat.provider : 'set up'}
            </button>
          )}
          {chat.msgs.length > 0 && (
            <button onClick={chat.reset} title="Start over" aria-label="Start over"
              className="grid h-6 w-6 place-items-center rounded text-ink-500 hover:bg-ink-800 hover:text-ink-100">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M3 12a9 9 0 1 0 2.6-6.4M3 3v6h6" />
              </svg>
            </button>
          )}
          <RuntimeSwitch
            runtime={chat.runtime}
            onChange={chat.setRuntime}
            connected={!!chat.agentStatus?.connected}
          />
        </div>
      </header>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
        {/* Disconnected: the panel explains how to connect and the chat stays
            locked. Connected: a confirmation line stays while the
            conversation is empty, then disappears to save space. */}
        {chat.runtime === 'agent' && (!chat.agentStatus?.connected || chat.msgs.length === 0) && (
          <AgentPanel status={chat.agentStatus} onRecheck={chat.recheckAgent} />
        )}

        {chat.msgs.length === 0 && chat.ready && (
          <>
            {hint}
            {suggestions.length > 0 && (
              <ul className="space-y-1.5">
                {suggestions.map((sug) => (
                  <li key={sug}>
                    {/* Fills the field instead of sending: one almost always
                        wants to adapt the example before really asking. */}
                    <button
                      type="button"
                      onClick={() => { setInput(sug); boxRef.current?.focus() }}
                      className="w-full rounded-lg border border-ink-800 px-3 py-2 text-left text-xs
                                 text-ink-500 transition-colors hover:border-ink-700
                                 hover:bg-ink-900 hover:text-ink-300"
                    >
                      {sug}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}

        {chat.msgs.map((m, i) => (
          <div key={i} className={m.role === 'user' ? 'text-right' : ''}>
            <div className={`inline-block max-w-[92%] whitespace-pre-wrap rounded-2xl px-3 py-2 text-left text-sm ${
              m.role === 'user' ? 'bg-accent-500 text-ink-950' : 'bg-ink-900 text-ink-100'}`}>
              {m.role === 'assistant'
                ? <Assistant text={m.content} pending={chat.busy && i === chat.msgs.length - 1} />
                : m.content}
            </div>
          </div>
        ))}

        {chat.tools.length > 0 && (
          <ul className="flex flex-wrap gap-1.5">
            {chat.tools.map((t, i) => (
              <li key={i} className="chip">
                {t.error ? '⚠' : t.done ? '✓' : '⋯'} {t.name}
              </li>
            ))}
          </ul>
        )}

        {chat.lastURL && (
          <button
            className="btn-outline w-full text-sm"
            onClick={() => nav(new URL(chat.lastURL, window.location.origin).pathname)}
          >
            Open the published artifact
          </button>
        )}

        {chat.err && <p className="text-sm text-red-400">{chat.err}</p>}
        <div ref={bottomRef} />
      </div>

      <form onSubmit={submit} className="shrink-0 border-t border-ink-800 p-2.5">
        <div className="rounded-xl border border-ink-700 bg-ink-900 focus-within:border-accent-500">
          <textarea
            ref={boxRef}
            rows={1}
            className="block max-h-40 w-full resize-none bg-transparent px-3 py-2.5 text-sm
                       text-ink-100 placeholder:text-ink-500 focus:outline-none"
            placeholder={
              chat.runtime === 'agent' && !chat.agentStatus?.connected
                ? 'Start `falconia agent` first…'
                : 'What do you want to build?'
            }
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              // Enter sends, Shift+Enter adds a line: the convention of every
              // chat, and long prompts still need line breaks.
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                submit(e)
              }
            }}
            disabled={chat.busy || !chat.ready}
          />
          <div className="flex items-center gap-2 px-2.5 pb-2">
            <span className="text-[11px] text-ink-600">
              {chat.busy ? 'working…' : 'Enter to send'}
            </span>
            {chat.busy ? (
              <button type="button" className="btn-ghost ml-auto !px-2 !py-1 text-xs" onClick={chat.stop}>
                Stop
              </button>
            ) : (
              <button className="btn-primary ml-auto !px-3 !py-1 text-xs"
                disabled={!input.trim() || !chat.ready}>
                Send
              </button>
            )}
          </div>
        </div>
      </form>

      {settings && (
        <ProviderDialog
          provider={chat.provider}
          onClose={() => setSettings(false)}
          onSave={(p) => { chat.setProvider(p); setSelectedProvider(p); setSettings(false) }}
        />
      )}
    </div>
  )
}

/**
 * Assistant shows the answer without the document inside.
 *
 * The code is already in the editor, two columns away: repeating it in full
 * in the bubble pushes off screen the sentence explaining what was done, which
 * is the only part worth reading here.
 */
function Assistant({ text, pending }: { text: string; pending: boolean }) {
  if (!text) return <>{pending ? '…' : ''}</>

  const parts: React.ReactNode[] = []
  const fence = /```[a-zA-Z]*\s*\n([\s\S]*?)(?:```|$)/g
  let last = 0
  let m: RegExpExecArray | null
  let n = 0

  while ((m = fence.exec(text)) !== null) {
    const before = text.slice(last, m.index).trim()
    if (before) parts.push(<span key={`t${n}`}>{before}</span>)
    const lines = m[1].trim().split('\n').length
    // A fence still open means the document is still arriving.
    const closed = m[0].endsWith('```')
    parts.push(
      <span key={`c${n}`} className="my-1.5 flex items-center gap-2 rounded-lg bg-ink-800 px-2.5 py-1.5 text-xs text-ink-300">
        <span aria-hidden>📄</span>
        {closed ? `${lines}-line document → in the editor` : 'writing the document…'}
      </span>,
    )
    last = m.index + m[0].length
    n++
  }
  const rest = text.slice(last).trim()
  if (rest) parts.push(<span key="tail">{rest}</span>)
  if (!parts.length) return <>{text}</>
  return <>{parts}</>
}

function RuntimeSwitch({ runtime, onChange, connected }: {
  runtime: 'agent' | 'key'; onChange(r: 'agent' | 'key'): void; connected: boolean
}) {
  return (
    <div className="flex gap-1 rounded-lg bg-ink-900 p-0.5">
      <button
        onClick={() => onChange('agent')}
        title="Runs on your computer with your subscription: no per-request cost"
        className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs transition-colors ${
          runtime === 'agent' ? 'bg-ink-700 text-white' : 'text-ink-300 hover:text-white'}`}
      >
        <span className={`h-1.5 w-1.5 rounded-full ${connected ? 'bg-emerald-500' : 'bg-ink-500'}`} />
        Your agent
      </button>
      <button
        onClick={() => onChange('key')}
        title="The server forwards your key to the provider: pay per use"
        className={`rounded-md px-2.5 py-1 text-xs transition-colors ${
          runtime === 'key' ? 'bg-ink-700 text-white' : 'text-ink-300 hover:text-white'}`}
      >
        API key
      </button>
    </div>
  )
}

function ProviderDialog({ provider, onClose, onSave }: {
  provider: AIProviderId; onClose(): void; onSave(p: AIProviderId): void
}) {
  const [id, setId] = useState<AIProviderId>(provider)
  const def = AI_PROVIDERS.find((p) => p.id === id)!
  const saved = loadProviderKey(id)
  const [apiKey, setApiKey] = useState(saved?.apiKey ?? '')
  const [model, setModel] = useState(saved?.model ?? def.defaultModel)
  const [baseURL, setBaseURL] = useState(saved?.baseURL ?? def.defaultBaseURL ?? '')

  return (
    <Dialog title="Model" onClose={onClose}>
      <div className="space-y-4">
        <p className="rounded-lg bg-ink-800 p-3 text-xs text-ink-300">
          This road is pay-per-use. If you already pay for Claude Code, Codex or Gemini
          CLI, “Your agent” uses that subscription at no extra cost.
        </p>

        <label className="block">
          <span className="mb-1.5 block text-sm text-ink-300">Provider</span>
          <select className="input" value={id} onChange={(e) => {
            const next = e.target.value as AIProviderId
            const d = AI_PROVIDERS.find((p) => p.id === next)!
            const s = loadProviderKey(next)
            setId(next)
            setApiKey(s?.apiKey ?? '')
            setModel(s?.model ?? d.defaultModel)
            setBaseURL(s?.baseURL ?? d.defaultBaseURL ?? '')
          }}>
            {AI_PROVIDERS.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
          </select>
        </label>

        <label className="block">
          <span className="mb-1.5 block text-sm text-ink-300">API key{def.keyOptional ? ' (optional)' : ''}</span>
          <input className="input font-mono text-xs" type="password" value={apiKey}
            onChange={(e) => setApiKey(e.target.value)} placeholder="sk-…" />
          <span className="mt-1 block text-xs text-ink-500">
            Stays in this browser and is forwarded to the provider with each request; the server never stores it.
            {def.docsUrl && <> <a className="text-accent-400" href={def.docsUrl} target="_blank" rel="noreferrer">Where to get one</a></>}
          </span>
        </label>

        <label className="block">
          <span className="mb-1.5 block text-sm text-ink-300">Model</span>
          <input className="input" value={model} onChange={(e) => setModel(e.target.value)} list="falconia-models" />
          <datalist id="falconia-models">
            {def.suggestedModels.map((m) => <option key={m} value={m} />)}
          </datalist>
        </label>

        {def.needsBaseURL && (
          <label className="block">
            <span className="mb-1.5 block text-sm text-ink-300">Base URL</span>
            <input className="input" value={baseURL} onChange={(e) => setBaseURL(e.target.value)}
              placeholder="http://localhost:11434/v1" />
            <span className="mt-1 block text-xs text-ink-500">
              With a local model (Ollama, LM Studio) this road costs nothing either.
            </span>
          </label>
        )}

        <button className="btn-primary w-full" onClick={() => {
          saveProviderKey(id, apiKey.trim(), model.trim() || def.defaultModel, baseURL.trim() || undefined)
          onSave(id)
        }}>Save</button>
      </div>
    </Dialog>
  )
}
