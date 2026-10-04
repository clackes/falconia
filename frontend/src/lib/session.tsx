import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  api, ApiError, clearToken, getToken, setToken,
  type InstanceConfig, type Space, type User,
} from './api'

type SessionValue = {
  user: User | null
  spaces: Space[]
  config: InstanceConfig | null
  loading: boolean
  /** The personal space, the default destination of every publish. */
  personalSpace: Space | null
  signIn(token: string, user: User): void
  signOut(): void
  refresh(): Promise<void>
}

const Ctx = createContext<SessionValue | null>(null)

export function SessionProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [spaces, setSpaces] = useState<Space[]>([])
  const [config, setConfig] = useState<InstanceConfig | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    // Without a token there is nothing to ask: the request would return 401
    // and fill the console with errors that are not errors.
    if (!getToken()) {
      setUser(null)
      setSpaces([])
      return
    }
    try {
      const me = await api.me()
      setUser(me.user)
      setSpaces(me.spaces)
    } catch (e) {
      setUser(null)
      setSpaces([])
      // Only an explicit rejection from the server means the token is no
      // longer valid. A network error means there was no network: dropping
      // the session over a VPN hiccup, or a request cancelled by navigation,
      // would force people to sign in again and again without knowing why.
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
        clearToken()
      }
    }
  }, [])

  useEffect(() => {
    let alive = true
    ;(async () => {
      // The instance configuration matters to signed-out visitors too: it
      // says, for instance, whether sign-up is open.
      const cfg = await api.config().catch(() => null)
      if (alive) setConfig(cfg)
      await refresh()
      if (alive) setLoading(false)
    })()
    return () => { alive = false }
  }, [refresh])

  const value = useMemo<SessionValue>(() => ({
    user, spaces, config, loading,
    personalSpace: spaces.find((s) => s.kind === 'personal') ?? null,
    signIn(token, u) {
      setToken(token)
      setUser(u)
      void refresh()
    },
    signOut() {
      clearToken()
      setUser(null)
      setSpaces([])
    },
    refresh,
  }), [user, spaces, config, loading, refresh])

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useSession(): SessionValue {
  const v = useContext(Ctx)
  if (!v) throw new Error('useSession outside SessionProvider')
  return v
}
