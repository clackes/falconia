# Changelog

## Unreleased — the Falconia release

The project formerly known as *Social for Artifacts* becomes **Falconia**: an
open-source, self-hosted home for artifacts made by any AI agent.

### Added
- **Google-Docs-style sharing**: general access (Restricted / Team / Anyone with
  the link), a people list with viewer and editor roles, a team role, secret
  `/p/<token>` links that work without an account, and link rotation.
- **Agent provenance**: every version, comment and event records which agent
  made it (Claude Code, Codex, Gemini CLI, Cursor, VS Code, Windsurf, opencode,
  …). Badges, history and an "agent" filter in the dashboard and the CLI.
- **Connect agents** page with a ready-to-paste MCP config for every major agent
  and an `AGENTS.md` snippet.
- **No lock-in tools**: `falconia pull`, zip download of any version, a
  "Continue with any agent" dialog, MCP `get_file`, patch-style
  `deploy_artifact` (send only the files that changed), `rotate_link`.
- MCP: protocol negotiation (2025-06-18, 2025-03-26, 2024-11-05), session ids
  that remember the client, `?agent=` hint, batches, proper `401`/`202`.
- CLI: `falconia share`, `falconia mcp`, agent auto-detection, `.falconia.json`
  folder links, Gemini CLI support in `falconia agent`.
- Built-in chat: Google Gemini provider; local models without a key.
- Open-source scaffolding: Apache-2.0 license, contributing guide, security
  policy, CI, release workflow publishing images to GHCR, unit and end-to-end
  tests.

### Fixed
- Two sign-ups on an empty instance could both become administrator; with
  sign-up closed, several "first" accounts could slip in.
- Team admins could make themselves owners, demote owners or remove them; two
  owners removing each other at once could leave a team with none.
- `/api/chat` accepted API keys, letting a read-only key drive tools with the
  account's full rights; custom `base_url` could reach cloud metadata
  addresses.
- Blob garbage collection deleted bytes before rows and could break a version
  being deployed at that moment; a blob whose row was lost could never be
  re-registered.
- File sizes came from the deploy manifest and were served as Content-Length;
  they are now the measured ones (existing rows are repaired by migration 010).
- An artifact that had been forked could not be deleted.
- Concurrent creates asking for the same slug failed with a 500 instead of
  getting the next free one.
- Approving a CLI login twice at once minted an orphan year-long key; expired
  login codes were only purged at startup.
- Queued agent requests hung forever when the agent disconnected; a negative
  `from` on the stream crashed the handler.
- A zip export silently skipped unreadable files.
- Comment edits and deletes no longer work after losing access to the artifact;
  empty or invalid input returns `400` instead of `500`, and storage errors no
  longer leak into responses.
- Login now costs the same for unknown and known emails and is rate limited per
  account; health checks include the database; one structured access-log line
  per request.

### Changed
- Everything is in English: interface, CLI, docs, agent instructions, code
  comments.
- The CLI is `falconia`; keys start with `falconia_pat_`; the in-artifact SDK
  global is `Falconia`; organizations are called teams in the interface.
- The production frontend calls the API on its own origin by default, so one
  image works on any domain.

### Fixed
- Saving a multi-file bundle from the editor (or deploying only `code` over
  MCP) no longer drops every file but the entry document.
- Comment pins now appear when comments load before the artifact frame.

### Compatibility
- `sfa_pat_` keys, the `SFA` global, `SFA_*` variables, `~/.sfa` and the
  `/__sfa/sdk.js` path keep working.
- Migrations `008` and `009` run automatically; the old "shared" visibility
  becomes "Restricted" (same access as before).
