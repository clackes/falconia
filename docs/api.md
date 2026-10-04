# API reference

Everything lives under `/api`. Authentication is `Authorization: Bearer <token>`,
where the token is either a session JWT or an API key (`falconia_pat_…`; keys
issued before the rename start with `sfa_pat_` and keep working).

Anonymous routes: `/api/health`, `/api/config`, sign-in, the CLI installer, and
`/api/p/{token}` — the artifacts their owners shared with "anyone with the
link".

## What a key can do

Always less than the person who issued it:

| | Session | API key |
| --- | --- | --- |
| read, publish, comment, share | yes | if it has the scope |
| delete an artifact | yes | **no** |
| create teams, invite to teams | yes | **no** |
| issue or revoke keys | yes | **no** |
| administer the instance | if admin | **no** |

Scopes: `artifacts:read`, `artifacts:write`, `comments:read`, `comments:write`.

## Agent provenance

Requests made with a key may say which AI agent is behind them:

```
X-Falconia-Agent: codex
```

The CLI sets it automatically (it recognizes Claude Code, Codex, Gemini CLI and
others from their environment; `FALCONIA_AGENT` overrides). MCP clients are
recognized from the name they send at `initialize`, or from `?agent=` on the
endpoint URL. The value is recorded on versions (`agent`), comments (`agent`)
and events, and is a display hint only — never an authorization input.

## Addressing an artifact

Where routes say `{id}`, a UUID or `space:slug` is accepted — with a colon,
because a slash would break the path. The CLI and the frontend use the same
convention.

## Authentication

| | |
| --- | --- |
| `POST /api/auth/register` | `{email, password, display_name}` → `{token, user}` |
| `POST /api/auth/login` | `{email, password}` → `{token, user}` |
| `GET /api/auth/me` | → `{user, spaces}` |

### Command-line login

A device-code flow, because the CLI is often launched inside an agent's
session, where a password would end up in the model's context and the shell
history.

| | |
| --- | --- |
| `POST /api/auth/device/start` | → `{device_code, user_code, verification_url}` |
| `POST /api/auth/device/poll` | `{device_code}` → `202` pending, `200` with the token, `410` expired |
| `GET /api/auth/device/{code}` | describes what is about to be authorized |
| `POST /api/auth/device/approve` | `{user_code}` — requires a session |

The token is handed over exactly once and the row deleted in the same
statement.

## Artifacts

| | |
| --- | --- |
| `GET /api/artifacts` | `?q=&space=&category=&agent=&limit=` — only what you can open |
| `GET /api/me/artifacts` | created by me |
| `GET /api/me/shared` | shared with me (by email, or links I opened while signed in) |
| `GET /api/s/{space}/{slug}` | resolves the signed-in address |
| `POST /api/artifacts` | create |
| `GET /api/artifacts/{id}` | `?v=N` — metadata, version, file list |
| `PATCH /api/artifacts/{id}` | metadata and general access; fields not named are untouched |
| `DELETE /api/artifacts/{id}` | session only |
| `POST /api/artifacts/{id}/commit` | `{code, message}` — replaces the entry document, keeps every other file |
| `POST /api/artifacts/{id}/fork` | `{title, space}` |
| `POST /api/artifacts/{id}/move` | `{space}` |
| `GET /api/artifacts/{id}/versions` | the history, with the agent of each version |
| `GET /api/artifacts/{id}/download` | `?v=N` — the version as a zip (edit access) |
| `GET /api/artifacts/{id}/content-url` | the signed URL for the iframe |
| `GET /api/artifacts/{id}/stats` | views, comments, versions |
| `GET /api/artifacts/{id}/events` | the activity log, owner only |

A version's source is returned only to those who could change it: read-only
access lets you watch the artifact run, not read its code.

The artifact object carries `agents` (every agent that published a version,
newest first), `visibility`, `team_role` and, when shared by link, `link_path`.

## Sharing

General access, like a Google Doc:

| `visibility` | Who can open |
| --- | --- |
| `private` | the owner and the people added by email ("Restricted") |
| `org` | everyone in the team that owns the space, with `team_role` `viewer` or `editor` |
| `link` | anyone with the secret link `/p/<token>` — no account needed |

| | |
| --- | --- |
| `PATCH /api/artifacts/{id}` | `{visibility, team_role}` |
| `POST /api/artifacts/{id}/link/rotate` | new secret link; the old one stops working |
| `GET /api/artifacts/{id}/shares` | people with direct access |
| `POST /api/artifacts/{id}/shares` | `{email, role}` — `viewer` or `editor`; works before they sign up |
| `DELETE /api/artifacts/{id}/shares/{email}` | |
| `GET /api/p/{token}` | anonymous. → `{artifact, content, signed_in, my_role?}` |

`/api/p/{token}` returns a minimal description (title, author, agents, version)
and a signed content URL. When the caller is signed in (session or key), the
visit is remembered: the artifact then appears in their "Shared with me" and
they can comment on it. Switching general access away from `link` closes the
door on everyone at once; rotating the link also forgets those visitors.

## Deploying a bundle

Three steps, designed for whoever publishes the same thing often.

```
POST /api/blobs/missing   {shas: [...]}     → {missing: [...]}
PUT  /api/blobs/<sha256>  <raw bytes>       → 201
POST /api/deploy          {manifest}        → {artifact, version, url, share_url?}
```

The first step is the one that matters: the server already has almost every
file, under the same hash. The hash in the second step's URL is recomputed from
the bytes and the request rejected if it does not match — without that check
anyone could replace the content of a hash ten artifacts share.

The `/api/deploy` manifest:

```json
{
  "space": "growth", "slug": "report",
  "title": "Report", "message": "fixed the numbers",
  "visibility": "link", "entry_path": "index.html",
  "files": [{"path": "index.html", "sha256": "…", "size": 1234, "content_type": "text/html"}]
}
```

`artifact_id` may replace `space`+`slug`. When nothing exists at `space`+`slug`,
the artifact is created; otherwise a new version is born. A pipeline does not
need to know whether it is the first time. Optional metadata only updates the
artifact when set.

A path trying to climb out of the bundle root is rejected, not silently cleaned.

## Comments

| | |
| --- | --- |
| `GET /api/artifacts/{id}/threads` | `?status=open&page=` |
| `POST /api/artifacts/{id}/threads` | `{body, page_path, anchor}` |
| `GET /api/artifacts/{id}/review` | Markdown digest, for an agent |
| `POST /api/threads/{tid}/replies` | `{body}` — replying reopens a resolved thread |
| `POST /api/threads/{tid}/status` | `{resolved}` — thread author, owner or editors |
| `DELETE /api/threads/{tid}` | |
| `PATCH /api/comments/{cid}` | own only |
| `DELETE /api/comments/{cid}` | own only |

The anchor:

```json
{"selector": "main > section:nth-of-type(2) > h2", "quote": "Q3 revenue", "x": 0.42, "y": 0.18}
```

Three clues because the DOM changes from one version to the next. They are
tried in that order; when none holds, the thread becomes "detached" and stays
visible in the side panel.

## Teams

| | |
| --- | --- |
| `GET/POST /api/orgs` | |
| `GET/POST /api/orgs/{id}/members` | |
| `DELETE /api/orgs/{id}/members/{uid}` | the last owner cannot be removed |
| `GET /api/orgs/{id}/artifacts` | |

## Keys and administration

| | |
| --- | --- |
| `GET/POST /api/keys`, `DELETE /api/keys/{id}` | session only |
| `GET /api/admin/stats`, `/users`, `/events` | admins only |
| `POST /api/admin/users/{id}/role` | `{is_admin}` |
| `POST /api/admin/gc?dry_run=false` | frees orphan blobs |

## Built-in chat

`POST /api/chat` streams SSE. Body: `{provider, model, api_key, base_url?,
system?, messages}` with `provider` among `anthropic`, `openai`, `gemini`,
`openrouter`, `local`, `custom`. The key is used for this request only and never
stored. What the chat publishes is labeled `chat:<model>`.

## Local agent bridge

The bridge between the interface and the agent on the person's computer. No
model credential passes through: only the request text and what the agent
prints. See [local-agent.md](local-agent.md).

| | |
| --- | --- |
| `GET /api/agent/status` | is an agent connected? which one, on which machine |
| `GET /api/agent/poll` | the bridge's long-poll, up to 25 s; `204` when idle |
| `POST /api/agent/disconnect` | the bridge leaves gracefully |
| `POST /api/agent/runs` | the browser asks for something; `409` if no agent |
| `POST /api/agent/runs/{id}/events` | the bridge sends output back, in batches |
| `GET /api/agent/runs/{id}/stream` | SSE to the browser; `?from=N` resumes |

## MCP

`POST /mcp` — Streamable HTTP with JSON responses, JSON-RPC 2.0, authenticated
with an API key (`401` otherwise). Implemented: `initialize` (protocol versions
`2025-06-18`, `2025-03-26`, `2024-11-05`), `ping`, `tools/list`, `tools/call`,
empty `resources/list` and `prompts/list`, notifications (`202`) and batches.
`initialize` returns an `Mcp-Session-Id` that remembers the client's name for
attribution. The tool list is filtered on the key's scopes.

`GET /mcp/info` describes how to connect. The tools are documented in
[agents.md](agents.md#mcp-tools).

## Content

Served from `CONTENT_BASE_URL`, a different origin.

```
GET /c/<grant>/<path>     a version's files
GET /__sfa/sdk.js         the shim injected into artifacts
```

The grant is an HMAC signed with `CONTENT_SECRET` holding artifact, version,
user (empty for anonymous link visitors) and expiry. It lives in the path, not
in a cookie: third-party cookies are blocked by almost every browser, and every
relative URL inside the artifact inherits the grant by itself.
