import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Artifact, type Member, type Org } from '../../lib/api'
import { useSession } from '../../lib/session'
import { ArtifactCard } from '../dashboard/ArtifactCard'

/**
 * Teams.
 *
 * A team is a group of people plus the space they share. Creating one creates
 * the other: two separate concepts in the interface would only be one more
 * step to explain. Each member brings whatever agent they use; the team's
 * artifacts belong to the team.
 */
export function OrgsPage() {
  const { refresh } = useSession()
  const [orgs, setOrgs] = useState<Org[]>([])
  const [name, setName] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const reload = () => api.orgs().then((r) => setOrgs(r.orgs)).catch(() => setOrgs([]))
  useEffect(() => { void reload() }, [])

  async function create(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) return
    setBusy(true)
    setErr('')
    try {
      await api.createOrg(name.trim())
      setName('')
      await reload()
      // The sidebar lists spaces: after a new team it must reload, or the
      // team's space would not show up until a page refresh.
      await refresh()
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-8 sm:px-6">
      <header className="mb-6">
        <h1 className="text-2xl font-semibold text-white">Teams</h1>
        <p className="mt-1 text-sm text-ink-500">
          A group of people and the space they share. Share an artifact with a team in one click.
        </p>
      </header>

      <form onSubmit={create} className="card mb-6 flex gap-2 p-4">
        <input className="input" placeholder="Team name"
          value={name} onChange={(e) => setName(e.target.value)} />
        <button className="btn-primary shrink-0" disabled={busy}>Create team</button>
      </form>
      {err && <p className="mb-4 text-sm text-red-400">{err}</p>}

      {orgs.length === 0 ? (
        <p className="card px-6 py-12 text-center text-ink-500">
          You are not in any team yet.
        </p>
      ) : (
        <ul className="space-y-2">
          {orgs.map((o) => (
            <li key={o.id}>
              <Link to={`/teams/${o.id}`} className="card-hover flex items-center gap-3 p-4">
                <span className="grid h-10 w-10 place-items-center rounded-lg bg-ink-800">👥</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-ink-100">{o.name}</span>
                  <span className="block text-xs text-ink-500">
                    space <code>{o.space_slug || o.slug}</code> · you are {roleLabel(o.my_role)}
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function OrgDetail() {
  const { id = '' } = useParams()
  const [members, setMembers] = useState<Member[]>([])
  const [artifacts, setArtifacts] = useState<Artifact[]>([])
  const [email, setEmail] = useState('')
  const [role, setRole] = useState('member')
  const [note, setNote] = useState('')
  const [err, setErr] = useState('')

  const reload = () => {
    api.orgMembers(id).then((r) => setMembers(r.members)).catch(() => setMembers([]))
    api.orgArtifacts(id).then((r) => setArtifacts(r.artifacts)).catch(() => setArtifacts([]))
  }
  useEffect(reload, [id])

  async function invite(e: React.FormEvent) {
    e.preventDefault()
    setErr('')
    setNote('')
    try {
      const r = await api.inviteToOrg(id, email.trim(), role)
      setNote(r.result === 'member'
        ? 'Added: they already had an account.'
        : 'Invitation saved: they join the first time they sign in.')
      setEmail('')
      reload()
    } catch (e: any) {
      setErr(e.message ?? 'Something went wrong')
    }
  }

  return (
    <div className="mx-auto max-w-4xl space-y-8 px-4 py-8 sm:px-6">
      <Link to="/teams" className="text-sm text-ink-500 hover:text-ink-300">← Teams</Link>

      <section>
        <h2 className="mb-3 text-lg font-medium text-white">People</h2>
        <form onSubmit={invite} className="mb-4 flex flex-col gap-2 sm:flex-row">
          <input className="input" type="email" required placeholder="teammate@example.com"
            value={email} onChange={(e) => setEmail(e.target.value)} />
          <select className="input sm:w-40" value={role} onChange={(e) => setRole(e.target.value)}>
            <option value="member">Member</option>
            <option value="admin">Admin</option>
            <option value="owner">Owner</option>
          </select>
          <button className="btn-primary shrink-0">Invite</button>
        </form>
        {note && <p className="mb-3 text-sm text-emerald-400">{note}</p>}
        {err && <p className="mb-3 text-sm text-red-400">{err}</p>}

        <ul className="divide-y divide-ink-800 rounded-xl border border-ink-800">
          {members.map((m) => (
            <li key={m.email} className="flex items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm text-ink-100">{m.name || m.email}</div>
                <div className="truncate text-xs text-ink-500">{m.email}</div>
              </div>
              <span className="chip">{roleLabel(m.role)}</span>
              {m.pending && <span className="chip !bg-amber-500/15 !text-amber-300">pending</span>}
              {!m.pending && (
                <button className="text-xs text-ink-500 hover:text-red-400"
                  onClick={async () => {
                    try { await api.removeOrgMember(id, m.user_id); reload() }
                    catch (e: any) { setErr(e.message) }
                  }}>Remove</button>
              )}
            </li>
          ))}
        </ul>
      </section>

      <section>
        <h2 className="mb-3 text-lg font-medium text-white">Team artifacts</h2>
        {artifacts.length === 0 ? (
          <p className="card px-6 py-10 text-center text-sm text-ink-500">
            Nothing in this space yet.
          </p>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {artifacts.map((a) => <ArtifactCard key={a.id} a={a} />)}
          </div>
        )}
      </section>
    </div>
  )
}

function roleLabel(r: string): string {
  return r === 'owner' ? 'owner' : r === 'admin' ? 'admin' : 'member'
}
