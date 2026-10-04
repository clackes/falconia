## What and why

<!-- What does this change, and why is it needed? Link the issue if there is one. -->

## Checks

- [ ] `go vet ./... && go test ./...` in `backend/`
- [ ] `npm run typecheck` in `frontend/`
- [ ] `python3 tests/e2e/smoke.py` against a running stack (for behavior changes)
- [ ] Docs updated (`docs/`, `README.md`, `skill/SKILL.md`, MCP instructions) if behavior changed
- [ ] Works for every agent, not just one
