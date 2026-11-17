import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, ApiError } from '../../lib/api'
import { useSession } from '../../lib/session'
import { LogoMark } from '../ui/Logo'

/**
 * Where the link in the verification email lands.
 *
 * It works signed in or not: people often open the email on their phone,
 * where they never signed in, and the token in the link is the proof.
 */
export function VerifyPage() {
  const [params] = useSearchParams()
  const { user, refresh } = useSession()
  const [state, setState] = useState<'working' | 'done' | 'failed'>('working')
  const [msg, setMsg] = useState('')
  // React's development double-render would spend the single-use token twice
  // and report the second attempt as a failure.
  const sent = useRef(false)

  useEffect(() => {
    if (sent.current) return
    sent.current = true
    const token = params.get('token') ?? ''
    api.verifyEmail(token)
      .then((r) => { setState('done'); setMsg(r.email); void refresh() })
      .catch((e) => { setState('failed'); setMsg(e instanceof ApiError ? e.message : 'Something went wrong') })
  }, [params, refresh])

  return (
    <div className="grid h-full place-items-center px-4">
      <div className="card w-full max-w-sm p-6 text-center">
        <div className="mb-4 flex justify-center"><LogoMark size={40} /></div>
        {state === 'working' && <p className="text-ink-300">Checking the link…</p>}
        {state === 'done' && (
          <>
            <h1 className="text-lg font-semibold text-white">Email confirmed</h1>
            <p className="mt-2 text-sm text-ink-300">
              {msg} is verified. Everything shared with this address is now visible to you.
            </p>
          </>
        )}
        {state === 'failed' && (
          <>
            <h1 className="text-lg font-semibold text-white">This link did not work</h1>
            <p className="mt-2 text-sm text-ink-300">{msg}</p>
          </>
        )}
        <Link to={user ? '/' : '/auth'} className="btn-primary mt-6 inline-flex">
          {user ? 'Go to your artifacts' : 'Sign in'}
        </Link>
      </div>
    </div>
  )
}
