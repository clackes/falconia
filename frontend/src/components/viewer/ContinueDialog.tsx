import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Artifact } from '../../lib/api'
import { CopyBlock } from '../ui/CopyBlock'
import { Dialog } from './Dialog'

type Tab = 'mcp' | 'cli' | 'download'

/**
 * "Continue with any agent": how to pick this artifact up from whatever AI
 * you use today.
 *
 * This is the promise of the whole product made concrete. The artifact may
 * have been made with Claude; if you now pay for Codex, or a teammate prefers
 * Gemini, the source, the history and the open comments are all one prompt or
 * one command away.
 */
export function ContinueDialog({ artifact, onClose }: { artifact: Artifact; onClose(): void }) {
  const [tab, setTab] = useState<Tab>('mcp')
  const [err, setErr] = useState('')
  const url = window.location.origin + artifact.path
  const ref = `${artifact.space_slug}/${artifact.slug ?? artifact.id}`

  const prompt = artifact.open_threads > 0
    ? `Open the Falconia artifact ${url}: read its source with get_artifact and its open comments with list_threads. Fix what the comments ask, publish a new version with deploy_artifact (send only the files you change), then resolve each thread you addressed.`
    : `Open the Falconia artifact ${url} with get_artifact and read its source. Then <describe the change you want>, and publish a new version with deploy_artifact (send only the files you change).`

  return (
    <Dialog title="Continue with any agent" onClose={onClose} wide>
      <p className="mb-4 text-sm text-ink-500">
        This artifact is not tied to the agent that made it. Pick it up from Claude Code,
        Codex, Gemini CLI, Cursor or anything else — every version and comment stays here.
      </p>

      <div className="mb-4 flex gap-1 rounded-lg bg-ink-900 p-1">
        {([['mcp', 'Agent prompt (MCP)'], ['cli', 'Terminal (CLI)'], ['download', 'Download']] as [Tab, string][]).map(([id, label]) => (
          <button key={id} onClick={() => setTab(id)}
            className={`flex-1 rounded-md px-3 py-1.5 text-sm transition-colors ${
              tab === id ? 'bg-ink-700 text-white' : 'text-ink-300 hover:text-white'}`}>
            {label}
          </button>
        ))}
      </div>

      {tab === 'mcp' && (
        <div className="space-y-3">
          <p className="text-sm text-ink-300">Paste this into any agent connected to Falconia:</p>
          <CopyBlock text={prompt} mono={false} wrap />
          <p className="text-xs text-ink-500">
            Agent not connected yet? <Link to="/connect" className="text-accent-400">Connect it</Link> in a minute.
          </p>
        </div>
      )}

      {tab === 'cli' && (
        <div className="space-y-3">
          <CopyBlock label="1 · Get the files" text={`falconia pull ${ref}`} />
          <p className="text-xs text-ink-500">
            Then ask your agent to edit them — any agent that can touch files works.
          </p>
          <CopyBlock label="2 · Read the feedback" text={`falconia comments ${ref}`} />
          <CopyBlock label="3 · Publish the new version" text={`falconia deploy ${artifact.slug ?? 'artifact'} -m "what changed"`} />
          <p className="text-xs text-ink-500">
            The pulled folder remembers this artifact, so the deploy updates it instead of
            creating a copy, and the version is labeled with the agent that ran the command.
          </p>
        </div>
      )}

      {tab === 'download' && (
        <div className="space-y-3">
          <p className="text-sm text-ink-300">
            Every file of the current version, as a zip. Yours to keep, edit anywhere, or
            publish again.
          </p>
          <button className="btn-primary" onClick={async () => {
            setErr('')
            try {
              await api.download(artifact.id, `${artifact.slug ?? 'artifact'}-v${artifact.latest_version}.zip`)
            } catch (e: any) {
              setErr(e.message ?? 'Download failed')
            }
          }}>
            ⬇ Download v{artifact.latest_version}
          </button>
          {err && <p className="text-sm text-red-400">{err}</p>}
        </div>
      )}
    </Dialog>
  )
}
