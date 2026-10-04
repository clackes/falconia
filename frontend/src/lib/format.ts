/** Shared formatting, so the same date is not written three different ways. */

export function humanBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'kB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  const v = n / Math.pow(1024, i)
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`
}

/**
 * Relative time, which is what a list needs: "3 hours ago" tells at a glance
 * whether something is fresh, a date does not.
 */
export function ago(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const s = Math.max(1, Math.floor((Date.now() - then) / 1000))
  if (s < 60) return 'just now'
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} ${h === 1 ? 'hour' : 'hours'} ago`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d} ${d === 1 ? 'day' : 'days'} ago`
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

export function fullDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit',
  })
}

/** General access, worded like the Share dialog of a Google Doc. */
export const visibilityLabel: Record<string, string> = {
  private: 'Restricted',
  org: 'Team',
  link: 'Anyone with the link',
}

export const visibilityIcon: Record<string, string> = {
  private: '🔒',
  org: '👥',
  link: '🔗',
}

export const visibilityHint: Record<string, string> = {
  private: 'Only you and the people you add can open it.',
  org: 'Everyone in the team that owns this space.',
  link: 'Anyone on the internet with the link can view. No account needed.',
}

/** sha256 hex of a Blob, for deploying from the browser. */
export async function sha256Hex(blob: Blob): Promise<string> {
  const buf = await blob.arrayBuffer()
  const digest = await crypto.subtle.digest('SHA-256', buf)
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/** Copies text and reports success, without throwing on old browsers. */
export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
