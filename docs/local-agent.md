# Use your subscription instead of an API key

People who code all day already pay for Claude Code, Codex or Gemini CLI. That
subscription is flat-rate; an API key is pay-per-use, and for daily work it
costs a lot more. This page explains how to put the subscription you already
have to work from Falconia's web interface — and why it works this way.

## In three commands

```bash
curl -fsSL https://falconia.example.com/api/install | sh
falconia login
cd ~/projects/my-project && falconia agent
```

Leave the terminal open, open **New artifact** in the web interface and write.
Requests run on your computer through your agent, and what it prints comes back
in real time.

## Why this and not a "Sign in with Claude" button

A button in the interface would be more convenient. It is not possible, and it
is not a matter of style.

Consumer subscriptions (Claude Pro/Max, ChatGPT, Gemini) authorize the use of
the vendors' official clients. Anthropic says so without room for
interpretation ([Legal and compliance](https://code.claude.com/docs/en/legal-and-compliance)):

> Anthropic does not permit third-party developers to offer Claude.ai login into
> their own applications, or to route requests through Free, Pro, or Max plan
> credentials on behalf of their users.

An application that tried would work until the first check, and the account
suspended would be that of whoever used it — a teammate, not the author of the
code.

The same page also says the opposite, and that is the part that matters:

> Nor does it prevent an end user from signing in to the unmodified Claude Code
> binary with their own Claude subscription.

So: the binary stays the official one, signed in by you, running on your
computer. The server receives the text of the request and sends back what the
agent prints. Credentials never cross it — it does not see them, store them, or
even know which subscription is paying for the work. That is also why one
Falconia instance works with everybody's subscription at the same time.

## What you need

One of:

- **Claude Code** — `claude`, signed in with your account;
- **Codex** — `codex`, signed in with `codex login`;
- **Gemini CLI** — `gemini`, signed in with your Google account;
- **any other harness**, through `--cmd`.

`falconia agent` picks the first one it finds. To choose:

```bash
falconia agent --runner codex
falconia agent --runner gemini
falconia agent --cmd 'my-agent --prompt {prompt}'
```

With `--cmd`, every printed line becomes text in the chat. If the harness
prints JSONL in Claude Code's, Codex's or Gemini CLI's format, it is recognized
automatically.

Whatever the agent publishes during a run is labeled with its name: the bridge
sets `FALCONIA_AGENT` in the agent's environment.

## Options

| | |
| --- | --- |
| `--dir <folder>` | where the agent works. Default: where you run the command. |
| `--runner claude\|codex\|gemini` | which engine to use. |
| `--cmd '<command> {prompt}'` | any other harness. |
| `--full-access` | let it run commands without asking. |
| `--once` | run a single request, then exit. Handy in scripts. |

### Permissions

By default the agent may edit files and commands are sandboxed:
`--permission-mode acceptEdits` for Claude Code, `-s workspace-write` for
Codex, `--approval-mode auto_edit` for Gemini CLI.

If a request hangs without output, it is almost always the agent waiting for an
approval nobody can give, since there is no interactive terminal on the other
end. `--full-access` removes the questions.

Think before using it: the agent will run commands on your computer, in the
folder you gave it, starting from text written in a chat. When in doubt, run
`falconia agent --dir ~/scratch/sandbox` in a folder you can throw away.

## Getting it to publish artifacts

The agent already has the CLI at hand, since it runs on the same machine. Ask
directly:

> Make a dashboard of the team's remaining vacation days and publish it to the
> `growth` space, visible to the whole team.

The agent writes the files and calls `falconia deploy`. When an artifact's
address appears in the output, the interface spots it and shows a button to
open it.

To teach it to do this well, add the snippet from [agents.md](agents.md#project-instructions)
to your project's `AGENTS.md`, or install the skill in `skill/SKILL.md` — for
Claude Code, copy it to `~/.claude/skills/falconia/`.

## The review loop, from the terminal

```bash
falconia comments growth/dashboard     # what people pointed out
# …the agent fixes and republishes…
falconia resolve <thread-id>
```

Or ask the same thing from the chat: "read the comments on growth/dashboard and
fix them".

## Other roads without metered cost

**A local model.** In *API key* → provider "Local model", point to Ollama, LM
Studio or vLLM. The default URL is Ollama's (`http://localhost:11434/v1`). It
costs nothing and works offline.

**A company key.** If your organization prefers to pay per use centrally, put
the key in *API key*: it stays in the browser of whoever enters it and is never
stored on the server.

## When the bridge does not connect

The interface shows *Connect your agent* until it sees one.

- The terminal running `falconia agent` must stay open.
- `falconia whoami` must answer with your name. If it says you are not signed
  in, run `falconia login` again.
- The bridge gives up on a single request after twenty minutes: a stuck agent
  does not hold the queue forever.
- If the backend restarts, the bridge notices on its next round and reconnects
  by itself. Connection state lives in memory on purpose.
