import { useEffect, useState } from 'react'
import { api, type AgentStatus } from '../../lib/api'
import { useSession } from '../../lib/session'
import { ago } from '../../lib/format'
import { CopyBlock } from '../ui/CopyBlock'

/**
 * How to connect your own agent to the web chat.
 *
 * The message to get across is that this is the normal road, not a hack for
 * tinkerers: the Claude Code, Codex or Gemini CLI subscription you already
 * have covers the work, while an API key is pay-per-use.
 */
export function AgentPanel({ status, onRecheck }: {
  status: AgentStatus | null
  onRecheck(): void
}) {
  const { config } = useSession()
  const apiBase = config?.api_base_url ?? ''

  if (status?.connected) {
    return (
      <div className="card flex flex-wrap items-center gap-3 border-emerald-600/40 p-4">
        <span className="h-2 w-2 shrink-0 rounded-full bg-emerald-500" />
        <div className="min-w-0 flex-1">
          <p className="text-sm text-ink-100">
            Agent connected · <span className="text-ink-300">{status.runner}</span>
            {status.host && <span className="text-ink-500"> on {status.host}</span>}
          </p>
          <p className="truncate text-xs text-ink-500">
            {status.dir}
            {status.last_seen && ` · seen ${ago(status.last_seen)}`}
          </p>
        </div>
        <p className="w-full text-xs text-ink-500 sm:w-auto">
          Runs on your computer, on your subscription. No per-request cost.
        </p>
      </div>
    )
  }

  return (
    <div className="card space-y-4 p-5">
      <div>
        <h2 className="font-medium text-white">Connect your agent</h2>
        <p className="mt-1 text-sm text-ink-500">
          Claude Code, Codex or Gemini CLI run on your computer with the subscription you
          already pay for. This page sends them requests and shows you the answer: no API
          key, no metered cost.
        </p>
      </div>

      <Step n={1} title="Install the command line">
        <CopyBlock text={`curl -fsSL ${apiBase}/api/install | sh`} />
      </Step>

      <Step n={2} title="Sign in, once">
        <CopyBlock text="falconia login" />
      </Step>

      <Step n={3} title="Start the bridge in the folder where it should work">
        <CopyBlock text="falconia agent" />
        <p className="mt-2 text-xs text-ink-500">
          Leave it running. It finds Claude Code, Codex or Gemini CLI by itself; pick one
          with <code className="text-ink-300">--runner codex</code>, or use any other
          harness with <code className="text-ink-300">--cmd</code>.
        </p>
      </Step>

      <div className="flex items-center gap-3 border-t border-ink-800 pt-4">
        <button className="btn-outline text-sm" onClick={onRecheck}>Check again</button>
        <p className="text-xs text-ink-500">
          Model credentials never pass through this server.
        </p>
      </div>
    </div>
  )
}

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-3">
      <span className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-ink-800 text-xs text-ink-300">
        {n}
      </span>
      <div className="min-w-0 flex-1">
        <p className="mb-1.5 text-sm text-ink-100">{title}</p>
        {children}
      </div>
    </div>
  )
}

/** Checks periodically whether the bridge is alive. */
export function useAgentStatus(): [AgentStatus | null, () => void] {
  const [status, setStatus] = useState<AgentStatus | null>(null)
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    let alive = true
    const check = () => {
      api.agentStatus()
        .then((s) => { if (alive) setStatus(s) })
        .catch(() => { if (alive) setStatus({ connected: false }) })
    }
    check()
    // Ten seconds: soon enough to notice the bridge started, without filling
    // the logs with requests.
    const t = setInterval(check, 10_000)
    return () => { alive = false; clearInterval(t) }
  }, [nonce])

  return [status, () => setNonce((n) => n + 1)]
}
