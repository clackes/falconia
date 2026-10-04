/**
 * The bridge between the application and an artifact.
 *
 * The artifact runs in an iframe pointing at ANOTHER origin (the content
 * server), and on top of that sandboxed without allow-same-origin, so in an
 * opaque origin. Two walls instead of one: even if someone removed the sandbox
 * attribute tomorrow to make something work, the artifact's code would still
 * sit on a domain that owns neither the session nor the viewer's data.
 *
 * The price is that nothing inside is visible from here: no DOM, no storage,
 * no variables. The only channel is a MessageChannel handed to the iframe
 * right after it loads, and everything that follows goes through it.
 */

import type { Anchor, Thread } from './api'

const NS = (artifactId: string) => `artifact_state_${artifactId}::`

export type PinState = {
  id: string
  n: number
  status: 'open' | 'resolved'
  anchor?: Anchor
  title?: string
}

export type MountOptions = {
  artifactId: string
  iframe: HTMLIFrameElement
  url: string
  /** Read-only: the artifact's state does not touch the viewer's browser. */
  readOnly?: boolean
  onReady?: () => void
  onResize?: (height: number) => void
  /** The person pointed at a spot of the page in comment mode. */
  onPick?: (anchor: Anchor) => void
  /** The person clicked an existing pin. */
  onFocusThread?: (threadId: string) => void
  /** Threads whose anchors could not be resolved on this version. */
  onOrphans?: (ids: string[]) => void
}

export type SandboxHandle = {
  destroy(): void
  /** Turns "point at a spot" mode on or off. */
  setCommentMode(on: boolean): void
  /** Updates the pins drawn over the artifact. */
  showThreads(threads: Thread[]): void
}

/**
 * Mounts an artifact and returns the controls to drive it.
 */
export function mountArtifact(opts: MountOptions): SandboxHandle {
  const { iframe, url, artifactId } = opts

  // The attribute is enforced here rather than left to the markup: it is the
  // defense that must not depend on someone remembering to write it.
  iframe.setAttribute('sandbox',
    'allow-scripts allow-forms allow-modals allow-popups allow-downloads allow-popups-to-escape-sandbox')
  iframe.setAttribute('referrerpolicy', 'no-referrer')
  iframe.removeAttribute('srcdoc')

  let port: MessagePort | null = null
  let destroyed = false
  let pendingThreads: Thread[] | null = null
  let pendingMode: boolean | null = null

  const send = (msg: unknown) => { try { port?.postMessage(msg) } catch { /* iframe already gone */ } }

  const handshake = () => {
    if (destroyed) return
    const channel = new MessageChannel()
    port?.close()
    port = channel.port1
    port.onmessage = onGuestMessage
    iframe.contentWindow?.postMessage(
      { type: '__SFA_HANDSHAKE__', readOnly: !!opts.readOnly },
      '*', [channel.port2],
    )
    // Navigation inside the artifact reloads the document and resets the
    // shim: whatever it had been told must be told again.
    if (pendingThreads) showThreads(pendingThreads)
    if (pendingMode !== null) send({ type: 'comment:mode', on: pendingMode })
  }

  function onGuestMessage(ev: MessageEvent) {
    const msg = ev.data
    if (!msg || typeof msg !== 'object') return
    switch (msg.type) {
      case 'storage:hydrate':
        send({
          type: 'storage:snapshot',
          entries: opts.readOnly ? {} : readNamespace(artifactId),
        })
        break
      case 'storage:set':
        if (opts.readOnly) break
        if (typeof msg.key === 'string' && typeof msg.value === 'string') {
          try { localStorage.setItem(NS(artifactId) + msg.key, msg.value) }
          catch { /* quota exceeded or storage blocked: the artifact goes on in memory */ }
          send({ type: 'storage:ack', key: msg.key })
        }
        break
      case 'storage:del':
        if (!opts.readOnly && typeof msg.key === 'string') {
          try { localStorage.removeItem(NS(artifactId) + msg.key) } catch { /* ignore */ }
        }
        break
      case 'storage:clear':
        if (!opts.readOnly) clearNamespace(artifactId)
        break
      case '__SFA_READY__':
        opts.onReady?.()
        break
      case 'ui:resize':
        opts.onResize?.(Math.max(0, Number(msg.height) || 0))
        break
      case 'comment:pick':
        if (msg.anchor) opts.onPick?.(msg.anchor as Anchor)
        pendingMode = false
        break
      case 'comment:focus':
        if (typeof msg.id === 'string') opts.onFocusThread?.(msg.id)
        break
      case 'comment:anchored':
        if (Array.isArray(msg.orphans)) opts.onOrphans?.(msg.orphans as string[])
        break
    }
  }

  function showThreads(threads: Thread[]) {
    pendingThreads = threads
    const pins: PinState[] = threads.map((t, i) => ({
      id: t.id,
      n: i + 1,
      status: t.status,
      anchor: t.anchor,
      title: t.comments[0]?.body?.slice(0, 80),
    }))
    send({ type: 'comment:anchors', anchors: pins })
  }

  iframe.addEventListener('load', handshake)
  iframe.src = url

  return {
    destroy() {
      destroyed = true
      iframe.removeEventListener('load', handshake)
      try { port?.close() } catch { /* ignore */ }
      try { iframe.src = 'about:blank' } catch { /* ignore */ }
    },
    setCommentMode(on: boolean) {
      pendingMode = on
      send({ type: 'comment:mode', on })
    },
    showThreads,
  }
}

/**
 * An artifact's state lives in the viewer's browser, under a prefix that holds
 * its id. Two artifacts never see each other, and a fork starts empty because
 * it has a new id.
 */
function readNamespace(artifactId: string): Record<string, string> {
  const prefix = NS(artifactId)
  const out: Record<string, string> = {}
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k && k.startsWith(prefix)) out[k.slice(prefix.length)] = localStorage.getItem(k) ?? ''
    }
  } catch { /* storage unavailable: the artifact starts empty */ }
  return out
}

function clearNamespace(artifactId: string) {
  const prefix = NS(artifactId)
  try {
    const doomed: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k && k.startsWith(prefix)) doomed.push(k)
    }
    doomed.forEach((k) => localStorage.removeItem(k))
  } catch { /* ignore */ }
}
