# Contributing to Falconia

Thanks for wanting to help! Falconia is a small, readable codebase on purpose:
a Go backend, a dependency-free Go CLI, and a React frontend.

## Run it locally

```bash
git clone https://github.com/clackes/falconia && cd falconia
cp .env.example .env
docker compose up --build -d
```

- Interface: <http://localhost:5173> — the first account becomes admin
- API, MCP, CLI installer: <http://localhost:8080>
- Artifact content: <http://localhost:8081>

Ports already taken? Change `APP_PORT`, `API_PORT`, `CONTENT_PORT` in `.env`
together with the matching URLs.

The frontend container hot-reloads. After backend changes:
`docker compose up -d --build backend`.

## Checks

Run what CI runs before opening a pull request:

```bash
# Backend (Go 1.25)
cd backend && go vet ./... && go test ./...
# CLI
cd cli && go vet ./... && go build ./...
# Frontend
cd frontend && npm ci && npm run typecheck && npm run build
# End-to-end, against a running dev stack (docker compose up)
python3 tests/e2e/smoke.py
```

No Go toolchain? Use Docker:
`docker run --rm -v "$PWD":/src -w /src/backend golang:1.25-alpine go test ./...`

## Where things are

```
backend/
  cmd/api/              entry point: two servers, app (:8080) and content (:8081)
  internal/agents/      which AI agent did something (labels, detection)
  internal/storage/     PostgreSQL access + migrations/ (embedded, run at startup)
  internal/handlers/    HTTP routes
  internal/tools/       the agent toolset, shared by MCP and the built-in chat
  internal/publish/     turns "these files changed" into a complete version
  internal/mcp/         MCP transport (Streamable HTTP, JSON-RPC)
  internal/content/     the separate-origin content server and the in-artifact SDK
  internal/agentbridge/ the bridge to people's local agents (falconia agent)
  internal/ai/          built-in chat with the person's own model key
cli/                    the falconia CLI, standard library only
frontend/src/           React + Vite + Tailwind
docs/                   self-hosting, agents, API, CI
skill/                  the skill agents install to use Falconia well
tests/e2e/              end-to-end smoke test against a running instance
```

## Principles

These shape most review comments, so it helps to know them up front:

1. **Agent-neutral.** Every feature works for every agent. If you add something
   for one vendor's client, add the equivalent for the others or make it
   generic. The point of Falconia is that nothing is locked to one AI.
2. **Artifacts are hostile.** They are model-written code nobody reviewed. They
   run on a separate origin in an opaque sandbox; never weaken either wall.
3. **Keys do less than people.** Destructive and privilege-widening actions
   need a browser session.
4. **No silent data loss.** A deploy that sends only some files keeps the
   others; slugs never collide into errors; history is append-only.
5. **Comments explain why.** The code is commented in prose that explains the
   reasoning, not the syntax. Match the surrounding style.

## Database changes

Add a new file in `backend/internal/storage/migrations/` with the next number
(`009_….sql`). Migrations must be idempotent where reasonable and must never
edit an applied file's SQL. They run automatically at startup inside an
advisory lock.

## Pull requests

- One topic per pull request, with a description of the why.
- Update docs when behavior changes (`docs/`, `README.md`, `skill/SKILL.md`,
  and the MCP `Instructions` in `backend/internal/tools/tools.go` if agents are
  affected).
- By contributing, you agree that your contributions are licensed under the
  Apache License 2.0.

## Reporting bugs and ideas

Use the issue templates. For security issues, see [SECURITY.md](SECURITY.md).
