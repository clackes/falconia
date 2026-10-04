---
name: falconia
description: Publish HTML pages, dashboards, reports and mini-apps to Falconia, the team's shared home for artifacts, with a stable link, versions and comments. Use it when asked to build something visual to share with people, to update or continue an artifact (even one another AI agent made), or to read and address the comments people left on it.
---

# Publishing to Falconia

Falconia hosts artifacts made by any AI agent. An artifact you create here can
be continued tomorrow by Codex, Gemini CLI or Claude Code — and you can continue
theirs. Everything goes through the `falconia` CLI (or, if it is configured, the
Falconia MCP tools: same operations, same names).

If the CLI is not installed:

```bash
curl -fsSL "$FALCONIA_API_URL/api/install" | sh   # then: falconia login
```

## Publish

```bash
falconia deploy ./folder --title "Sales dashboard" -m "first version"
```

It accepts a single file or a folder and returns an address that **never
changes**: later versions pile up behind the same link, so whoever already has
it does not need a new one. The folder remembers its artifact (`.falconia.json`),
so deploying it again updates the same artifact.

Options that matter:

| | |
| --- | --- |
| `--space <slug>` | where to publish. `falconia spaces` lists destinations. |
| `--slug <slug>` | the identifier in the URL. **Reuse it to update.** |
| `--visibility` | `private` (default), `org` (the team), `link` (anyone with the link) |
| `-m, --message` | what changed |
| `--open` | print only the URL |

General access is private by default. Switch to `org` or `link` only when asked
to share with the team or with "anyone"; `falconia share` changes it later.

## Continue an existing artifact

Whoever made it, with whatever agent:

```bash
falconia pull growth/dashboard        # or the URL you were given, even a /p/ link
# edit the files in ./dashboard
falconia deploy dashboard -m "added the Q4 column"
```

## Address the comments

This is why the tool exists. After people looked at what you published:

```bash
falconia comments growth/dashboard
```

For every open thread you get the file, the element's selector, the quoted text
and the messages. The selector tells you **where** to act: use it.

Then fix, republish to the same place, and close the thread:

```bash
falconia deploy ./dashboard -m "fixed the numbers"
falconia resolve <thread-id>
falconia reply <thread-id> "Fixed in version 3."
```

Resolve a thread only after actually publishing the fix. A thread closed with
nothing changed is worse than an open one, because its author stops watching.

## How to write the artifact

A **complete, self-contained** HTML document. No network requests, no scripts
from CDNs: inline everything, because the instance may live on a network
without internet access.

It runs in an opaque-origin iframe: no cookies, no `window.parent`. To keep
data, use the global `Falconia` object, injected before your code (`SFA` is an
older alias):

```html
<script>
  Falconia.setSchemaVersion(1)
  Falconia.onReady(({ state, save }) => {
    const data = state ?? { items: [] }
    render(data)
    // save(data) whenever something changes
  })
</script>
```

If you change the shape of the data, bump the schema version and register the
transformation, or people already using the artifact lose what was inside:

```html
<script>
  Falconia.setSchemaVersion(2)
  Falconia.registerMigration(1, (old) => ({ ...old, read: false }))
</script>
```

For several files, a folder with `index.html` at the root and relative paths.
A `.md` file works too: the server renders it.

Make it good: it must work on a phone screen, read well in light and dark mode,
and be keyboard-navigable. People will open it, not developers.

## When someone drives you from the browser

When someone runs `falconia agent`, what they write in the web interface comes
to you. You work in the folder the bridge was started from, and what you print
goes back to the chat.

Two practical consequences: print the artifact's address after publishing,
because the interface turns it into a button; and do not count on asking the
user questions mid-task, because there is no interactive terminal on the other
end.

## Other commands

```bash
falconia whoami          # who you are, on which instance
falconia spaces          # where you can publish
falconia list            # what already exists, and how many comments wait
falconia share <ref> --access link   # make it viewable by anyone with the link
```

## When NOT to use it

For the code of the project you are working on: that belongs in the repository.
Falconia is for things people look at — a report, a dashboard, a prototype, a
page to read.
