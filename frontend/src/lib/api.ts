/**
 * The API client.
 *
 * One place that knows how to talk to the backend, and one place that knows
 * where the token lives. The rest of the interface calls `api.something()` and
 * never holds a URL or a header.
 */

// Empty means "same origin": the production image serves the interface and
// proxies /api and /mcp from one host, so one build works on any domain. Dev
// sets VITE_API_BASE because Vite and the backend run on different ports.
const BASE: string = ((import.meta as any).env?.VITE_API_BASE ?? '').replace(/\/$/, '')

// The key name predates the rename: changing it would sign everyone out.
const TOKEN_KEY = 'sfa.session.v2'

export function getToken(): string | null {
  try { return localStorage.getItem(TOKEN_KEY) } catch { return null }
}
export function setToken(t: string) {
  try { localStorage.setItem(TOKEN_KEY, t) } catch { /* private mode */ }
}
export function clearToken() {
  try { localStorage.removeItem(TOKEN_KEY) } catch { /* ignore */ }
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getToken()
  const headers: Record<string, string> = { ...(init?.headers as any) }
  // An explicit Content-Type from the caller wins: a blob upload sends raw
  // bytes, not JSON.
  if (init?.body && !headers['Content-Type']) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`

  const r = await fetch(`${BASE}${path}`, { ...init, headers })
  if (r.status === 204) return undefined as T
  const ct = r.headers.get('content-type') || ''
  const isJSON = ct.includes('application/json')
  const payload = isJSON ? await r.json().catch(() => null) : await r.text()

  if (!r.ok) {
    const msg = (isJSON && payload?.error) || (typeof payload === 'string' && payload) || r.statusText
    throw new ApiError(r.status, msg)
  }
  return payload as T
}

const GET = <T,>(p: string) => req<T>(p)
const POST = <T,>(p: string, body?: unknown) =>
  req<T>(p, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })
const PATCH = <T,>(p: string, body: unknown) =>
  req<T>(p, { method: 'PATCH', body: JSON.stringify(body) })
const DELETE = <T,>(p: string) => req<T>(p, { method: 'DELETE' })

/**
 * An artifact can be addressed by UUID or by "space/slug". In a route path the
 * slash would break routing, so it becomes a colon: the same convention the
 * CLI uses.
 */
export function ref(spaceOrId: string, slug?: string): string {
  return slug ? `${spaceOrId}:${slug}` : spaceOrId
}

// ---------------- Types ----------------

export type User = {
  id: string; email: string; display_name: string
  is_admin: boolean; created_at: string
}

export type Space = {
  id: string; slug: string; name: string
  kind: 'personal' | 'org'; my_role?: string
}

/** General access, like a Google Doc's. */
export type Visibility = 'private' | 'org' | 'link'

export type Artifact = {
  id: string
  title: string; description: string
  visibility: Visibility
  team_role: 'viewer' | 'editor'
  slug?: string; path: string
  link_path?: string
  icon: string; logo_url: string; theme_color: string
  category: string; forkable: boolean
  space_id: string; space_slug: string; space_name: string; space_kind: string
  author_id: string; author_name: string
  latest_version: number
  fork_parent_id?: string
  open_threads: number
  agents: string[]
  created_at: string; updated_at: string
  my_role?: 'owner' | 'editor' | 'viewer'
  likes: number; liked: boolean
}

/** What an anonymous visitor of a shared link learns about an artifact. */
export type LinkArtifact = {
  id: string; title: string; description: string
  author_name: string; latest_version: number
  agents: string[]; updated_at: string
}

export type ArtifactVersion = {
  id: number; version_number: number; commit_message: string
  author_id: string; author_name: string
  agent: string
  entry_path: string; kind: 'doc' | 'bundle'
  file_count: number; total_bytes: number
  code_hash: string; created_at: string; code?: string
}

export type BundleFile = { path: string; content_type: string; size: number }

export type Anchor = { selector?: string; quote?: string; x?: number; y?: number }

export type Comment = {
  id: string; thread_id: string; user_id: string; user_name: string
  body: string; via: string; agent: string; edited: boolean; created_at: string
}

export type Thread = {
  id: string; artifact_id: string; version_number: number
  page_path: string; anchor?: Anchor
  status: 'open' | 'resolved'
  created_by: string; created_by_name: string
  resolved_at?: string; created_at: string
  comments: Comment[]
}

export type ApiKey = {
  id: string; label: string; prefix: string; kind: string
  scopes: string[]; created_at: string
  expires_at?: string; last_used_at?: string
}

export type Org = {
  id: string; slug: string; name: string
  my_role: string; space_slug: string; created_at: string
}

export type Member = {
  user_id: string; email: string; name: string
  role: string; joined_at: string; pending: boolean
}

export type Event = {
  id: number; artifact_id?: string; artifact_title?: string
  actor_name: string; kind: string; version_number?: number
  via: string; agent: string; meta?: Record<string, unknown>; created_at: string
}

export type Stats = {
  views: number; unique_views: number
  open_threads: number; total_threads: number
  likes: number; versions: number
}

export type AgentStatus = {
  connected: boolean
  runner?: string
  host?: string
  dir?: string
  last_seen?: string
}

export type AgentEvent = {
  type: 'status' | 'text' | 'tool' | 'error' | 'done'
  text?: string
  tool?: string
  exit_code?: number
  url?: string
}

export type InstanceConfig = {
  name: string; version: string
  app_base_url: string; content_base_url: string; api_base_url: string
  mcp_url: string
  allow_registration: boolean
  max_upload_bytes: number; max_bundle_files: number
}

export type ContentGrant = { url: string; version: number; entry_path: string; expires_in: number }

// ---------------- Calls ----------------

export const api = {
  config: () => GET<InstanceConfig>('/api/config'),
  categories: () => GET<{ categories: { slug: string; label: string; icon: string }[] }>('/api/categories'),

  register: (email: string, password: string, display_name: string) =>
    POST<{ token: string; user: User }>('/api/auth/register', { email, password, display_name }),
  login: (email: string, password: string) =>
    POST<{ token: string; user: User }>('/api/auth/login', { email, password }),
  me: () => GET<{ user: User; spaces: Space[] }>('/api/auth/me'),

  spaces: () => GET<{ spaces: Space[] }>('/api/spaces'),

  feed: (q?: { query?: string; space?: string; category?: string; agent?: string; limit?: number }) => {
    const p = new URLSearchParams()
    if (q?.query) p.set('q', q.query)
    if (q?.space) p.set('space', q.space)
    if (q?.category) p.set('category', q.category)
    if (q?.agent) p.set('agent', q.agent)
    if (q?.limit) p.set('limit', String(q.limit))
    const qs = p.toString()
    return GET<{ artifacts: Artifact[] }>(`/api/artifacts${qs ? '?' + qs : ''}`)
  },
  myArtifacts: () => GET<{ artifacts: Artifact[] }>('/api/me/artifacts'),
  sharedWithMe: () => GET<{ artifacts: Artifact[] }>('/api/me/shared'),

  artifact: (r: string, version?: number) =>
    GET<{ artifact: Artifact; version?: ArtifactVersion; files?: BundleFile[] }>(
      `/api/artifacts/${r}${version ? `?v=${version}` : ''}`),
  byPath: (space: string, slug: string, version?: number) =>
    GET<{ artifact: Artifact; version?: ArtifactVersion; files?: BundleFile[] }>(
      `/api/s/${space}/${slug}${version ? `?v=${version}` : ''}`),

  /** Opens a shared link. Works signed out; signed in, it also grants view access. */
  openLink: (token: string, version?: number) =>
    GET<{ artifact: LinkArtifact; content: ContentGrant; signed_in: boolean; my_role?: string }>(
      `/api/p/${encodeURIComponent(token)}${version ? `?v=${version}` : ''}`),

  create: (body: Record<string, unknown>) =>
    POST<{ artifact: Artifact; version: ArtifactVersion }>('/api/artifacts', body),
  updateMeta: (r: string, patch: Record<string, unknown>) =>
    PATCH<{ artifact: Artifact }>(`/api/artifacts/${r}`, patch),
  remove: (r: string) => DELETE<void>(`/api/artifacts/${r}`),
  commit: (r: string, code: string, message: string) =>
    POST<{ version: ArtifactVersion }>(`/api/artifacts/${r}/commit`, { code, message }),
  fork: (r: string, title?: string, space?: string) =>
    POST<{ artifact: Artifact }>(`/api/artifacts/${r}/fork`, { title, space }),
  move: (r: string, space: string) =>
    POST<{ artifact: Artifact }>(`/api/artifacts/${r}/move`, { space }),
  versions: (r: string) => GET<{ versions: ArtifactVersion[] }>(`/api/artifacts/${r}/versions`),
  forks: (r: string) => GET<{ artifacts: Artifact[] }>(`/api/artifacts/${r}/forks`),
  stats: (r: string) => GET<Stats>(`/api/artifacts/${r}/stats`),
  events: (r: string) => GET<{ events: Event[] }>(`/api/artifacts/${r}/events`),
  rotateLink: (r: string) => POST<{ artifact: Artifact }>(`/api/artifacts/${r}/link/rotate`, {}),

  contentURL: (r: string, version?: number) =>
    GET<ContentGrant>(`/api/artifacts/${r}/content-url${version ? `?v=${version}` : ''}`),

  /** Downloads a version as a zip, with the session's credentials. */
  async download(r: string, filename: string, version?: number) {
    const token = getToken()
    const res = await fetch(`${BASE}/api/artifacts/${r}/download${version ? `?v=${version}` : ''}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
    if (!res.ok) throw new ApiError(res.status, (await res.json().catch(() => null))?.error ?? res.statusText)
    const url = URL.createObjectURL(await res.blob())
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 10_000)
  },

  like: (r: string) => POST<{ likes: number; liked: boolean }>(`/api/artifacts/${r}/likes`),
  unlike: (r: string) => DELETE<{ likes: number; liked: boolean }>(`/api/artifacts/${r}/likes`),

  shares: (r: string) => GET<{ shares: { email: string; role: string }[] }>(`/api/artifacts/${r}/shares`),
  addShare: (r: string, email: string, role: string) =>
    POST<void>(`/api/artifacts/${r}/shares`, { email, role }),
  removeShare: (r: string, email: string) =>
    DELETE<void>(`/api/artifacts/${r}/shares/${encodeURIComponent(email)}`),

  threads: (r: string, onlyOpen = false) =>
    GET<{ threads: Thread[] }>(`/api/artifacts/${r}/threads${onlyOpen ? '?status=open' : ''}`),
  createThread: (r: string, body: {
    body: string; page_path?: string; version_number?: number; anchor?: Anchor
  }) => POST<{ thread: Thread }>(`/api/artifacts/${r}/threads`, body),
  reply: (threadId: string, body: string) =>
    POST<{ comment: Comment }>(`/api/threads/${threadId}/replies`, { body }),
  setThreadStatus: (threadId: string, resolved: boolean) =>
    POST<{ thread: Thread }>(`/api/threads/${threadId}/status`, { resolved }),
  deleteThread: (threadId: string) => DELETE<void>(`/api/threads/${threadId}`),
  editComment: (id: string, body: string) => PATCH<void>(`/api/comments/${id}`, { body }),
  deleteComment: (id: string) => DELETE<void>(`/api/comments/${id}`),

  keys: () => GET<{ keys: ApiKey[] }>('/api/keys'),
  createKey: (body: { label: string; kind: string; scopes?: string[]; expires_in_days?: number }) =>
    POST<{ key: ApiKey; token: string }>('/api/keys', body),
  revokeKey: (id: string) => DELETE<void>(`/api/keys/${id}`),

  describeDevice: (code: string) =>
    GET<{ user_code: string; label: string; approved: boolean }>(`/api/auth/device/${code}`),
  approveDevice: (user_code: string) => POST<{ ok: boolean }>('/api/auth/device/approve', { user_code }),

  orgs: () => GET<{ orgs: Org[] }>('/api/orgs'),
  createOrg: (name: string, slug?: string) => POST<{ org: Org }>('/api/orgs', { name, slug }),
  orgMembers: (id: string) => GET<{ members: Member[] }>(`/api/orgs/${id}/members`),
  inviteToOrg: (id: string, email: string, role: string) =>
    POST<{ result: string }>(`/api/orgs/${id}/members`, { email, role }),
  removeOrgMember: (id: string, uid: string) => DELETE<void>(`/api/orgs/${id}/members/${uid}`),
  orgArtifacts: (id: string) => GET<{ artifacts: Artifact[] }>(`/api/orgs/${id}/artifacts`),

  adminStats: () => GET<Record<string, number | string>>('/api/admin/stats'),
  adminUsers: () => GET<{ users: User[] }>('/api/admin/users'),
  adminEvents: () => GET<{ events: Event[] }>('/api/admin/events'),
  adminSetRole: (id: string, is_admin: boolean) =>
    POST<void>(`/api/admin/users/${id}/role`, { is_admin }),
  agentStatus: () => GET<AgentStatus>('/api/agent/status'),
  agentRun: (prompt: string) => POST<{ id: string }>('/api/agent/runs', { prompt }),

  adminGC: (dryRun: boolean) =>
    POST<{ orphans: number; deleted: number }>(`/api/admin/gc?dry_run=${dryRun}`),

  /** Deploys a bundle from the browser: the same three steps as the CLI. */
  async deployBundle(payload: {
    space?: string; slug?: string; title?: string; message?: string
    visibility?: string; entry_path: string
    files: { path: string; sha256: string; size: number; content_type: string; blob: Blob }[]
  }) {
    const shas = payload.files.map((f) => f.sha256)
    const { missing } = await POST<{ missing: string[] }>('/api/blobs/missing', { shas })
    const need = new Set(missing)
    for (const f of payload.files) {
      if (!need.has(f.sha256)) continue
      await req<void>(`/api/blobs/${f.sha256}`, {
        method: 'PUT',
        body: f.blob,
        headers: { 'Content-Type': 'application/octet-stream' },
      })
      need.delete(f.sha256)
    }
    return POST<{ artifact: Artifact; version: ArtifactVersion; url: string }>('/api/deploy', {
      ...payload,
      files: payload.files.map(({ blob, ...rest }) => rest),
    })
  },
}

/** apiBase composes the URLs shown to people (installer, MCP endpoint). */
export const apiBase = BASE
