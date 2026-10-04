import { useRef, useState } from 'react'
import { api } from '../../lib/api'
import { useSession } from '../../lib/session'
import { humanBytes, sha256Hex } from '../../lib/format'
import { Dialog } from '../viewer/Dialog'

type Picked = { path: string; sha256: string; size: number; content_type: string; blob: Blob }

/**
 * Uploading a folder from the browser.
 *
 * It takes the same three steps as the CLI, for the same reason: ask the
 * server which files it lacks and upload only those. Re-uploading the same
 * folder with one line changed transfers a few kilobytes instead of the whole
 * bundle, and in a browser the difference shows even more than in a terminal.
 */
export function UploadDialog({ defaultSpace, onClose, onDone }: {
  defaultSpace: string; onClose(): void; onDone(path: string): void
}) {
  const { spaces, config } = useSession()
  const inputRef = useRef<HTMLInputElement | null>(null)
  const [files, setFiles] = useState<Picked[] | null>(null)
  const [space, setSpace] = useState(defaultSpace)
  const [title, setTitle] = useState('')
  const [slug, setSlug] = useState('')
  const [visibility, setVisibility] = useState('private')
  const [status, setStatus] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const maxFiles = config?.max_bundle_files ?? 500
  const maxBytes = config?.max_upload_bytes ?? 64 * 1024 * 1024

  async function pick(list: FileList | null) {
    if (!list) return
    setErr('')
    setStatus('Reading files…')
    const out: Picked[] = []
    for (const f of Array.from(list)) {
      // webkitRelativePath starts with the chosen folder's name: strip it, or
      // every path in the bundle would start there and index.html would not
      // sit at the root.
      const rel = ((f as any).webkitRelativePath as string | undefined) || f.name
      const path = rel.includes('/') ? rel.slice(rel.indexOf('/') + 1) : rel
      if (!path || path.startsWith('.') || path.includes('/.')) continue
      if (f.size > maxBytes) {
        setErr(`${path} exceeds the ${humanBytes(maxBytes)} limit`)
        setStatus('')
        return
      }
      out.push({
        path, size: f.size, blob: f,
        content_type: f.type || 'application/octet-stream',
        sha256: await sha256Hex(f),
      })
    }
    if (out.length === 0) {
      setErr('No usable files in this folder.')
      setStatus('')
      return
    }
    if (out.length > maxFiles) {
      setErr(`Too many files: the limit is ${maxFiles}.`)
      setStatus('')
      return
    }
    setFiles(out)
    setStatus('')
    if (!title) setTitle(out[0].path.split('/')[0] || 'Artifact')
  }

  const entry = files?.find((f) => f.path === 'index.html')?.path
    ?? files?.find((f) => /^index\.(htm|md)$/.test(f.path))?.path
    ?? files?.find((f) => /\.(html?|md)$/i.test(f.path))?.path

  async function publish() {
    if (!files || !entry) return
    setBusy(true)
    setErr('')
    try {
      setStatus('Uploading missing files…')
      const r = await api.deployBundle({
        space, slug: slug.trim() || undefined, title: title.trim() || undefined,
        visibility, entry_path: entry, files,
      })
      onDone(r.artifact.path)
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setBusy(false)
      setStatus('')
    }
  }

  const total = files?.reduce((n, f) => n + f.size, 0) ?? 0

  return (
    <Dialog title="Upload a folder" onClose={onClose} wide>
      <div className="space-y-5">
        <div>
          <input
            ref={inputRef}
            type="file"
            className="hidden"
            // @ts-expect-error non-standard attribute, supported by browsers
            webkitdirectory=""
            directory=""
            multiple
            onChange={(e) => void pick(e.target.files)}
          />
          <button className="btn-outline w-full py-8" onClick={() => inputRef.current?.click()}>
            {files ? `${files.length} files · ${humanBytes(total)} — pick another` : 'Choose a folder'}
          </button>
          <p className="mt-2 text-xs text-ink-500">
            It must contain an <code className="text-ink-300">index.html</code>. From a terminal it is
            faster: <code className="text-ink-300">falconia deploy ./folder</code>
          </p>
        </div>

        {files && !entry && (
          <p className="text-sm text-amber-300">
            No document to open: an index.html is needed at the root.
          </p>
        )}

        {files && entry && (
          <>
            <div className="max-h-40 overflow-y-auto rounded-lg border border-ink-800">
              <ul className="divide-y divide-ink-800 text-xs">
                {files.slice(0, 50).map((f) => (
                  <li key={f.path} className="flex justify-between px-3 py-1.5">
                    <span className={`truncate ${f.path === entry ? 'text-accent-400' : 'text-ink-300'}`}>
                      {f.path}
                    </span>
                    <span className="shrink-0 pl-3 text-ink-500">{humanBytes(f.size)}</span>
                  </li>
                ))}
                {files.length > 50 && (
                  <li className="px-3 py-1.5 text-ink-500">… and {files.length - 50} more</li>
                )}
              </ul>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <Labeled label="Title">
                <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} />
              </Labeled>
              <Labeled label="URL identifier">
                <input className="input" placeholder="derived from the title"
                  value={slug} onChange={(e) => setSlug(e.target.value)} />
              </Labeled>
              <Labeled label="Space">
                <select className="input" value={space} onChange={(e) => setSpace(e.target.value)}>
                  {spaces.map((s) => (
                    <option key={s.id} value={s.slug}>{s.kind === 'org' ? `👥 ${s.name}` : `👤 ${s.slug}`}</option>
                  ))}
                </select>
              </Labeled>
              <Labeled label="General access">
                <select className="input" value={visibility} onChange={(e) => setVisibility(e.target.value)}>
                  <option value="private">Restricted</option>
                  {spaces.find((s) => s.slug === space)?.kind === 'org' && <option value="org">Team</option>}
                  <option value="link">Anyone with the link</option>
                </select>
              </Labeled>
            </div>
          </>
        )}

        {status && <p className="text-sm text-ink-500">{status}</p>}
        {err && <p className="text-sm text-red-400">{err}</p>}

        <div className="flex gap-2">
          <button className="btn-primary flex-1" disabled={!files || !entry || busy} onClick={publish}>
            {busy ? '…' : 'Publish'}
          </button>
          <button className="btn-ghost" onClick={onClose}>Cancel</button>
        </div>
      </div>
    </Dialog>
  )
}

function Labeled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm text-ink-300">{label}</span>
      {children}
    </label>
  )
}
