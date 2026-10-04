import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { Shell } from './components/layout/Shell'
import { AuthPage } from './components/auth/AuthPage'
import { Dashboard } from './components/dashboard/Dashboard'
import { SpacePage } from './components/dashboard/SpacePage'
import { ArtifactPage } from './components/viewer/ArtifactPage'
import { LinkPage } from './components/viewer/LinkPage'
import { CreatorStudio } from './components/studio/CreatorStudio'
import { ConnectPage } from './components/settings/ConnectPage'
import { CliLoginPage } from './components/settings/CliLoginPage'
import { OrgsPage, OrgDetail } from './components/orgs/OrgsPage'
import { AdminPage } from './components/admin/AdminPage'
import { useSession } from './lib/session'
import type { ReactElement } from 'react'

export default function App() {
  const { user, loading } = useSession()
  const loc = useLocation()

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-ink-500">
        Loading…
      </div>
    )
  }

  if (!user) {
    // The destination is kept: whoever opens an artifact's link and finds out
    // they are signed out lands back on it after signing in, not on the home.
    return (
      <Routes>
        {/* A shared link opens without an account: that is its whole point. */}
        <Route path="/p/:token" element={<LinkPage />} />
        <Route path="/auth" element={<AuthPage />} />
        <Route path="*" element={<Navigate to="/auth" replace state={{ from: loc.pathname + loc.search }} />} />
      </Routes>
    )
  }

  return (
    <Routes>
      {/* The viewer takes the whole screen: the artifact is the content, not
          a section of the application. */}
      <Route path="/s/:space/:slug" element={<ArtifactPage />} />
      <Route path="/a/:id" element={<ArtifactPage />} />
      <Route path="/p/:token" element={<LinkPage />} />

      <Route element={<Shell />}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/s/:space" element={<SpacePage />} />
        <Route path="/new" element={<CreatorStudio />} />
        <Route path="/edit/:id" element={<CreatorStudio />} />
        <Route path="/chat" element={<Navigate to="/new" replace />} />
        <Route path="/teams" element={<OrgsPage />} />
        <Route path="/teams/:id" element={<OrgDetail />} />
        <Route path="/orgs" element={<Navigate to="/teams" replace />} />
        <Route path="/connect" element={<ConnectPage />} />
        <Route path="/settings/keys" element={<Navigate to="/connect" replace />} />
        <Route path="/cli-login" element={<CliLoginPage />} />
        <Route path="/admin" element={<AdminOnly><AdminPage /></AdminOnly>} />
        <Route path="/auth" element={<Navigate to="/" replace />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

function AdminOnly({ children }: { children: ReactElement }) {
  const { user } = useSession()
  return user?.is_admin ? children : <Navigate to="/" replace />
}
