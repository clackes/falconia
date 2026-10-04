# Security policy

Falconia hosts code written by AI models and runs it in people's browsers, so
its security model matters more than usual. Thank you for helping keep it
sound.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through
[GitHub security advisories](https://github.com/clackes/falconia/security/advisories/new).
Include what you found, how to reproduce it, and the impact you expect. You
should get an answer within a few days; fixes are released as soon as they are
ready, and you will be credited unless you prefer otherwise.

## What is in scope

The guarantees Falconia makes, and that a report would break:

- **Artifact isolation.** Artifacts are served from a separate origin
  (`CONTENT_BASE_URL`) inside a sandboxed iframe without `allow-same-origin`.
  Code in an artifact must not be able to read the viewer's session, call the
  API on their behalf, or read another artifact's data.
- **Access control.** An artifact is visible only according to its general
  access (restricted / team / anyone with the link) and its list of people.
  Lists never reveal the titles of artifacts the caller cannot open.
- **Share links.** `/p/<token>` links are unguessable, stop working when
  general access leaves "anyone with the link", and are invalidated by rotation.
- **API keys.** Keys can do less than their owner: they cannot delete
  artifacts, manage teams, or issue keys. Only bcrypt hashes are stored.
- **Uploads.** Blob hashes are verified against the bytes; bundle paths cannot
  escape the bundle root; content types are inferred from extensions, not
  trusted from uploaders.
- **Provider keys.** Keys for the built-in chat are never stored server-side.

## Out of scope

- What an artifact does inside its own sandbox (it is untrusted by design).
- The `agent` label on versions and comments: it is a self-reported display
  hint, documented as such, never used for authorization.
- Self-XSS, missing security headers without a demonstrated impact, and denial
  of service through volume.

## Supported versions

Security fixes go into the latest release. Run the latest version.
