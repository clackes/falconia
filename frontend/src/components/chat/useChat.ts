import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBase, getToken, type AgentEvent } from '../../lib/api'
import { loadProviderKey, providerInfo, selectedProvider, type AIProviderId } from '../../lib/ai-providers'
import { useAgentStatus } from './AgentPanel'
import type { AgentStatus } from '../../lib/api'

export type ChatMsg = { role: 'user' | 'assistant'; content: string }
export type ChatTool = { name: string; done?: boolean; error?: boolean }

/** Where the answering model runs. */
export type Runtime = 'agent' | 'key'

const RUNTIME_KEY = 'sfa.chat.runtime.v1'

export type ChatOptions = {
  /**
   * Instructions prepended to every request. In the Studio they tell the
   * model to return a document instead of publishing it itself.
   */
  preamble?: () => string
  /** Called when a complete HTML document appears in the answer. */
  onDocument?: (code: string) => void
}

/**
 * The chat's logic, separate from how it is drawn.
 *
 * It exists because the same conversation is needed in more than one place,
 * and two copies of an SSE reader drift apart at the first bug fixed in only
 * one of them.
 */
export function useChat(opts: ChatOptions = {}) {
  const [agentStatus, recheckAgent] = useAgentStatus()
  const [runtime, setRuntimeState] = useState<Runtime>(
    () => (localStorage.getItem(RUNTIME_KEY) as Runtime) || 'agent')
  const [provider, setProvider] = useState<AIProviderId>(selectedProvider())
  const [msgs, setMsgs] = useState<ChatMsg[]>([])
  const [tools, setTools] = useState<ChatTool[]>([])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [lastURL, setLastURL] = useState('')
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => () => abortRef.current?.abort(), [])

  const setRuntime = useCallback((r: Runtime) => {
    setRuntimeState(r)
    localStorage.setItem(RUNTIME_KEY, r)
  }, [])

  const creds = loadProviderKey(provider)
  const hasKey = !!creds?.apiKey || (!!creds?.model && !!providerInfo(provider).keyOptional)
  const ready = runtime === 'agent' ? !!agentStatus?.connected : hasKey

  const appendToLast = (text: string) => {
    setMsgs((m) => {
      const copy = [...m]
      const last = copy[copy.length - 1]
      if (last?.role === 'assistant') {
        copy[copy.length - 1] = { role: 'assistant', content: last.content + text }
      }
      return copy
    })
  }

  async function send(text: string) {
    text = text.trim()
    if (!text || busy || !ready) return

    const next: ChatMsg[] = [...msgs, { role: 'user', content: text }]
    setMsgs([...next, { role: 'assistant', content: '' }])
    setTools([])
    setErr('')
    setLastURL('')
    setBusy(true)

    const ctrl = new AbortController()
    abortRef.current = ctrl
    const preamble = opts.preamble?.() ?? ''

    // The full answer text is needed at the end to look for a document in it:
    // React state arrives too late, being asynchronous.
    let full = ''
    const take = (t: string) => { full += t; appendToLast(t) }

    try {
      if (runtime === 'agent') await runOnAgent(text, preamble, ctrl, take)
      else await runOnProvider(next, preamble, ctrl, take)

      const doc = extractDocument(full)
      if (doc) opts.onDocument?.(doc)
    } catch (e: any) {
      if (e?.name !== 'AbortError') setErr(e?.message ?? 'Error')
    } finally {
      setBusy(false)
      abortRef.current = null
    }
  }

  /** The agent on the writer's own computer. */
  async function runOnAgent(
    prompt: string, preamble: string,
    ctrl: AbortController, take: (t: string) => void,
  ) {
    // The agent has no notion of a "system prompt": instructions go in front
    // of the request.
    const body = preamble ? `${preamble}\n\n---\n\n${prompt}` : prompt
    const resp = await fetch(`${apiBase}/api/agent/runs`, {
      method: 'POST',
      signal: ctrl.signal,
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
      body: JSON.stringify({ prompt: body }),
    })
    if (!resp.ok) throw new Error((await resp.json().catch(() => null))?.error ?? await resp.text())
    const { id } = await resp.json()

    const stream = await fetch(`${apiBase}/api/agent/runs/${id}/stream`, {
      signal: ctrl.signal,
      headers: { Authorization: `Bearer ${getToken()}` },
    })
    if (!stream.ok || !stream.body) throw new Error(await stream.text())

    await readSSE(stream.body, (raw) => {
      const ev = JSON.parse(raw) as AgentEvent
      switch (ev.type) {
        case 'status':
          setTools((t) => [...t, { name: ev.text ?? 'starting', done: true }])
          break
        case 'text':
          take(ev.text ?? '')
          if (ev.url) setLastURL(ev.url)
          break
        case 'tool':
          setTools((t) => [...t, { name: ev.tool ?? 'tool', done: true }])
          break
        case 'error':
          setErr(ev.text ?? 'error')
          break
        case 'done':
          if (ev.exit_code) setErr(`The agent exited with code ${ev.exit_code}.`)
          break
      }
    })
  }

  /** The provider, with the person's own key. */
  async function runOnProvider(
    history: ChatMsg[], preamble: string,
    ctrl: AbortController, take: (t: string) => void,
  ) {
    const resp = await fetch(`${apiBase}/api/chat`, {
      method: 'POST',
      signal: ctrl.signal,
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
      body: JSON.stringify({
        provider, model: creds!.model, api_key: creds!.apiKey,
        base_url: creds!.baseURL, system: preamble, messages: history,
      }),
    })
    if (!resp.ok || !resp.body) {
      const text = await resp.text()
      let msg = text
      try { msg = JSON.parse(text).error ?? text } catch { /* not JSON */ }
      throw new Error(msg)
    }

    await readSSE(resp.body, (raw) => {
      const ev = JSON.parse(raw)
      switch (ev.kind) {
        case 'text': take(ev.text ?? ''); break
        case 'tool_call': setTools((t) => [...t, { name: ev.tool_name }]); break
        case 'tool_result':
          setTools((t) => t.map((x, i) =>
            i === t.length - 1 ? { ...x, done: true, error: ev.is_error } : x))
          try {
            const parsed = JSON.parse(ev.result_text ?? '')
            const url = parsed?.share_url || parsed?.url
            if (url) setLastURL(String(url))
          } catch { /* not every result is an object */ }
          break
        case 'error': setErr(ev.text ?? 'Error'); break
      }
    })
  }

  return {
    runtime, setRuntime,
    provider, setProvider,
    agentStatus: agentStatus as AgentStatus | null, recheckAgent,
    msgs, tools, busy, err, lastURL, ready, hasKey,
    send,
    stop: () => abortRef.current?.abort(),
    reset: () => { setMsgs([]); setTools([]); setErr(''); setLastURL('') },
  }
}

/**
 * extractDocument looks for a complete HTML document in the answer.
 *
 * It takes the LAST one: when the model first shows a fragment and then the
 * whole file, the second is the one wanted in the editor.
 */
export function extractDocument(text: string): string | null {
  const fenced = [...text.matchAll(/```(?:html|HTML)?\s*\n([\s\S]*?)```/g)]
    .map((m) => m[1].trim())
    .filter((c) => /<html[\s>]/i.test(c) || /<!doctype html/i.test(c))
  if (fenced.length) return fenced[fenced.length - 1]

  // Some models return the document without a fence.
  const bare = text.match(/<!doctype html[\s\S]*<\/html>/i)
  return bare ? bare[0].trim() : null
}

/**
 * readSSE reads an event stream.
 *
 * Frames are split only on a blank line: a `data:` line can arrive split
 * across two packets, and splitting on a single newline would produce randomly
 * truncated JSON.
 */
export async function readSSE(body: ReadableStream<Uint8Array>, onData: (raw: string) => void) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let sep: number
    while ((sep = buffer.indexOf('\n\n')) >= 0) {
      const frame = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      const line = frame.split('\n').find((l) => l.startsWith('data:'))
      if (line) {
        try { onData(line.slice(5).trim()) } catch { /* one broken frame does not stop the stream */ }
      }
    }
  }
}
