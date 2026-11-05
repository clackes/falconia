import { useState } from 'react'
import { api, ApiError } from '../../lib/api'
import { useSession } from '../../lib/session'

/**
 * Shown while the person's address is not trusted yet: until it is, what
 * others shared with that address stays hidden, and they should know why
 * rather than think nothing was shared.
 */
export function VerifyBanner() {
  const { user, config, emailTrusted } = useSession()
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  if (!user || emailTrusted || user.email_verified) return null

  async function resend() {
    setBusy(true)
    try {
      await api.resendVerification()
      setNote(`Sent to ${user!.email}. Check your inbox.`)
    } catch (e) {
      setNote(e instanceof ApiError ? e.message : 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-amber-500/20 bg-amber-500/10 px-4 py-2 text-sm text-amber-200">
      <span>
        Confirm your email to see what others share with {user.email}.
        {!config?.can_send_email && ' This instance cannot send email: ask an administrator to verify you.'}
      </span>
      {config?.can_send_email && (
        <button className="underline underline-offset-2 hover:text-white disabled:opacity-50" disabled={busy} onClick={resend}>
          Send the link again
        </button>
      )}
      {note && <span className="text-amber-100/80">{note}</span>}
    </div>
  )
}
