import { agentInfo } from '../../lib/agents'

/** A small label naming the agent that made something. */
export function AgentBadge({ agent, size = 'sm', title }: {
  agent: string; size?: 'xs' | 'sm'; title?: string
}) {
  const a = agentInfo(agent)
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border border-ink-700 bg-ink-900 ${
        size === 'xs' ? 'px-1.5 py-0 text-[10px]' : 'px-2 py-0.5 text-xs'} text-ink-300`}
      title={title ?? (agent ? `Published with ${a.label}` : 'Edited by a person in the browser')}
    >
      <span className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: a.color }} />
      <span className="truncate">{a.label}</span>
    </span>
  )
}

/** The agents behind an artifact, newest first, collapsed past a few. */
export function AgentStack({ agents, max = 3 }: { agents: string[]; max?: number }) {
  if (!agents.length) return null
  const shown = agents.slice(0, max)
  const rest = agents.length - shown.length
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {shown.map((a) => <AgentBadge key={a} agent={a} size="xs" />)}
      {rest > 0 && <span className="text-[10px] text-ink-500">+{rest}</span>}
    </span>
  )
}
