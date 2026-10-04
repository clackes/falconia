import { useState } from 'react'
import { api } from '../../lib/api'
import { useSession } from '../../lib/session'

/**
 * Confirming a CLI login.
 *
 * Here you type the code the terminal showed and say yes. The page says
 * plainly what is being authorized and which machine (and agent) asks for it:
 * whoever confirms must be able to recognize their own terminal, and get
 * suspicious when they do not.
 */
export function CliLoginPage() {
  const { user } = useSession()
  const [code, setCode] = useState('')
  const [info, setInfo] = useState<{ label: string } | null>(null)
  const [done, setDone] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  async function lookup(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const r = await api.describeDevice(code.trim().toUpperCase())
      setInfo({ label: r.label })
    } catch {
      setErr('Invalid or expired code. Run `falconia login` again.')
      setInfo(null)
    } finally {
      setBusy(false)
    }
  }

  async function approve() {
    setBusy(true)
    setErr('')
    try {
      await api.approveDevice(code.trim().toUpperCase())
      setDone(true)
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-md px-6 py-16">
      <h1 className="text-2xl font-semibold text-white">Connect a terminal</h1>
      <p className="mt-2 text-sm text-ink-500">
        Enter the code shown by <code className="text-ink-300">falconia login</code>.
      </p>

      {done ? (
        <div className="card mt-8 p-6 text-center">
          <p className="text-ink-100">Done.</p>
          <p className="mt-2 text-sm text-ink-500">
            You can go back to the terminal: it has already unlocked by itself.
          </p>
        </div>
      ) : (
        <form onSubmit={lookup} className="card mt-8 space-y-4 p-6">
          <input
            className="input text-center font-mono text-2xl tracking-[0.3em]"
            placeholder="XXXX-XXXX"
            value={code}
            onChange={(e) => { setCode(e.target.value.toUpperCase()); setInfo(null) }}
            maxLength={9}
            autoFocus
          />

          {info ? (
            <div className="rounded-lg border border-ink-700 bg-ink-800 p-4 text-sm">
              <p className="text-ink-300">You are about to give access to:</p>
              <p className="mt-1 font-medium text-ink-100">{info.label}</p>
              <p className="mt-3 text-xs text-ink-500">
                It will be able to read and publish as {user?.email}, but not delete
                artifacts or create other keys. If you do not recognize this machine, do
                not confirm.
              </p>
            </div>
          ) : null}

          {err && <p className="text-sm text-red-400">{err}</p>}

          {info ? (
            <button type="button" className="btn-primary w-full" disabled={busy} onClick={approve}>
              Authorize
            </button>
          ) : (
            <button className="btn-outline w-full" disabled={busy || code.length < 8}>
              Continue
            </button>
          )}
        </form>
      )}
    </div>
  )
}
