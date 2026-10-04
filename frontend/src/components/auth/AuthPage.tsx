import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { api, ApiError } from '../../lib/api'
import { useSession } from '../../lib/session'
import { LogoMark } from '../ui/Logo'

const AGENTS = ['Claude Code', 'Codex', 'Gemini CLI', 'Cursor', 'VS Code', 'Windsurf', 'opencode']

/**
 * Sign in and sign up.
 *
 * There is no guest mode for the dashboard — shared links (/p/...) are the
 * public face — and the admin can close sign-up. It stays open anyway while
 * the instance has no users, otherwise a fresh install would have no way to
 * get its first administrator.
 */
export function AuthPage() {
  const { config, signIn } = useSession()
  const nav = useNavigate()
  const loc = useLocation()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const canRegister = config?.allow_registration !== false
  const destination = (loc.state as { from?: string } | null)?.from || '/'

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setBusy(true)
    try {
      const r = mode === 'login'
        ? await api.login(email, password)
        : await api.register(email, password, name)
      signIn(r.token, r.user)
      nav(destination, { replace: true })
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid h-full lg:grid-cols-2">
      <section className="relative hidden flex-col justify-between overflow-hidden border-r border-ink-800 bg-ink-900 p-12 lg:flex">
        <div className="pointer-events-none absolute -right-32 -top-32 h-96 w-96 rounded-full bg-accent-500/10 blur-3xl" />
        <div className="flex items-center gap-3">
          <LogoMark size={36} />
          <span className="text-lg font-semibold text-white">Falconia</span>
        </div>
        <div className="max-w-md">
          <h2 className="text-3xl font-semibold leading-tight text-white">
            Artifacts from every AI agent, in one place your team owns.
          </h2>
          <p className="mt-4 text-ink-300">
            Make a dashboard with Claude, fix it with Codex, let a teammate improve it with
            Gemini. Share it with a link, collect comments pinned on the page, keep every
            version — whatever subscription anyone pays for.
          </p>
          <div className="mt-6 flex flex-wrap gap-2">
            {AGENTS.map((a) => <span key={a} className="chip">{a}</span>)}
            <span className="chip">any MCP client</span>
          </div>
        </div>
        <p className="text-xs text-ink-500">Open source · self-hosted · Apache-2.0</p>
      </section>

      <section className="grid place-items-center px-4 py-12">
        <div className="w-full max-w-sm">
          <div className="mb-8 text-center lg:hidden">
            <div className="mb-3 flex justify-center"><LogoMark size={48} /></div>
            <h1 className="text-xl font-semibold text-white">Falconia</h1>
            <p className="mt-1 text-sm text-ink-500">Artifacts from every AI agent, in one place.</p>
          </div>
          <h1 className="mb-6 hidden text-xl font-semibold text-white lg:block">
            {mode === 'login' ? 'Sign in' : 'Create your account'}
          </h1>

          <form onSubmit={submit} className="card space-y-4 p-6">
            {mode === 'register' && (
              <Field label="Name">
                <input className="input" value={name} onChange={(e) => setName(e.target.value)}
                  autoComplete="name" placeholder="What your teammates call you" />
              </Field>
            )}
            <Field label="Email">
              <input className="input" type="email" required value={email}
                onChange={(e) => setEmail(e.target.value)} autoComplete="email" />
            </Field>
            <Field label="Password">
              <input className="input" type="password" required value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                minLength={mode === 'register' ? 10 : undefined} />
              {mode === 'register' && (
                <p className="mt-1 text-xs text-ink-500">At least 10 characters.</p>
              )}
            </Field>

            {err && <p className="text-sm text-red-400">{err}</p>}

            <button className="btn-primary w-full" disabled={busy}>
              {busy ? '…' : mode === 'login' ? 'Sign in' : 'Create account'}
            </button>

            {canRegister && (
              <button type="button" className="w-full text-center text-sm text-ink-500 hover:text-ink-300"
                onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setErr('') }}>
                {mode === 'login' ? 'No account yet? Sign up' : 'Already have an account? Sign in'}
              </button>
            )}
            {!canRegister && mode === 'login' && (
              <p className="text-center text-xs text-ink-500">
                Sign-up is closed: ask whoever runs this instance for an invitation.
              </p>
            )}
          </form>
        </div>
      </section>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-sm text-ink-300">{label}</span>
      {children}
    </label>
  )
}
