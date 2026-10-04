# AGENTS.md — working on the Falconia codebase

Guidance for AI coding agents (Codex, Claude Code, Gemini CLI, Cursor…) that
contribute to this repository. Humans: see CONTRIBUTING.md, which says the same
at more length.

## What this is

Falconia is a self-hosted, open-source home for artifacts (HTML pages,
dashboards, docs, mini-apps) made by **any** AI agent. Teams share them with
Google-Docs-style access, comment on them, and any agent can continue any
artifact. Agent neutrality is the core product value: never build something
that only works with one vendor's agent.

## Layout

- `backend/` — Go 1.25. Entry `cmd/api`. Packages in `internal/`: `storage`
  (Postgres + embedded migrations), `handlers` (HTTP), `tools` (agent toolset
  for MCP and chat), `publish` (patch-style versions), `mcp`, `content`
  (separate-origin server + `assets/sdk.js`), `agents` (agent identification),
  `agentbridge`, `ai`.
- `cli/` — Go, standard library only. Binary `falconia`.
- `frontend/` — React 18, Vite, Tailwind, TypeScript.
- `docs/`, `skill/SKILL.md`, `tests/e2e/smoke.py`.

## Commands

```bash
docker compose up --build -d                          # full dev stack
cd backend && go vet ./... && go test ./...           # backend checks
cd cli && go vet ./... && go build ./...              # CLI checks
cd frontend && npm run typecheck && npm run build     # frontend checks
python3 tests/e2e/smoke.py                            # e2e, needs the stack running
```

Run the relevant checks before declaring a task done.

## Rules

- Everything user-facing and every comment is in **English**.
- Comments explain *why*, in prose, like the existing code. Do not add
  comments that restate the code.
- Artifacts are untrusted: never serve them from the app origin, never add
  `allow-same-origin` to their iframe, never let the content server learn about
  sessions.
- API keys must not gain destructive or privilege-widening abilities
  (`humanOnly` routes stay human-only).
- New schema → a new numbered file in `backend/internal/storage/migrations/`.
  Never edit the SQL of an existing migration.
- Keep backward compatibility with the pre-rename names: `sfa_pat_` keys, the
  `SFA` JS global, `SFA_*` env vars, `/__sfa/sdk.js`, `sfa.*` localStorage keys.
- When tool behavior changes, update `tools.Instructions`, `skill/SKILL.md`
  and `docs/agents.md` together.
