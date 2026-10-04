import { useState } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useSession } from '../../lib/session'
import { Logo } from '../ui/Logo'

/**
 * The application frame: sidebar on the left, content on the right.
 *
 * The sidebar is organized by SPACE rather than by object type, because that
 * is how people look for things: "that page of the growth team", not "that
 * HTML document". On narrow screens it collapses behind a menu button.
 */
export function Shell() {
  const { user, spaces, signOut } = useSession()
  const nav = useNavigate()
  const loc = useLocation()
  const [open, setOpen] = useState(false)
  const teamSpaces = spaces.filter((s) => s.kind === 'org')
  const personal = spaces.find((s) => s.kind === 'personal')

  // Close the mobile menu on navigation.
  const [lastPath, setLastPath] = useState(loc.pathname)
  if (lastPath !== loc.pathname) {
    setLastPath(loc.pathname)
    if (open) setOpen(false)
  }

  return (
    <div className="flex h-full">
      <aside className={`${open ? 'flex' : 'hidden'} fixed inset-y-0 left-0 z-40 w-64 shrink-0 flex-col border-r border-ink-800 bg-ink-950 md:static md:flex`}>
        <div className="px-4 py-5">
          <NavLink to="/"><Logo /></NavLink>
        </div>

        <nav className="flex-1 space-y-6 overflow-y-auto px-2 pb-4">
          <div className="space-y-1">
            <Item to="/" label="All artifacts" icon="🗂" end />
            <Item to="/new" label="New artifact" icon="✨" />
            <Item to="/connect" label="Connect agents" icon="🔌" />
          </div>

          {personal && (
            <Section title="Your space">
              <Item to={`/s/${personal.slug}`} label={personal.slug} icon="👤" />
            </Section>
          )}

          {teamSpaces.length > 0 && (
            <Section title="Teams">
              {teamSpaces.map((s) => (
                <Item key={s.id} to={`/s/${s.slug}`} label={s.name} icon="👥" />
              ))}
            </Section>
          )}

          <Section title="Settings">
            <Item to="/teams" label="Manage teams" icon="🛡" />
            {user?.is_admin && <Item to="/admin" label="Instance" icon="⚙️" />}
          </Section>
        </nav>

        <div className="border-t border-ink-800 p-3">
          <div className="px-2 pb-2">
            <div className="truncate text-sm text-ink-100">{user?.display_name}</div>
            <div className="truncate text-xs text-ink-500">{user?.email}</div>
          </div>
          <button
            className="side-btn text-sm"
            onClick={() => { signOut(); nav('/auth') }}
          >
            ⏻ Sign out
          </button>
        </div>
      </aside>

      {open && (
        <div className="fixed inset-0 z-30 bg-black/50 md:hidden" onClick={() => setOpen(false)} aria-hidden />
      )}

      <main className="min-w-0 flex-1 overflow-y-auto">
        <div className="sticky top-0 z-20 flex items-center gap-3 border-b border-ink-800 bg-ink-950/90 px-4 py-2 backdrop-blur md:hidden">
          <button className="btn-ghost !px-2" onClick={() => setOpen(true)} aria-label="Open menu">☰</button>
          <Logo size={24} />
        </div>
        <Outlet />
      </main>
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <div className="px-3 text-[11px] uppercase tracking-wider text-ink-500">{title}</div>
      {children}
    </div>
  )
}

function Item({ to, label, icon, end }: { to: string; label: string; icon: string; end?: boolean }) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        `side-btn text-sm ${isActive ? 'bg-ink-800 text-white' : ''}`
      }
    >
      <span aria-hidden>{icon}</span>
      <span className="truncate">{label}</span>
    </NavLink>
  )
}
