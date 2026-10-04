import { useSession } from '../../lib/session'

/**
 * The preview of code not yet published.
 *
 * A published artifact is served from the content origin, but there is no
 * version to serve of code the author is still writing. The preview therefore
 * uses srcdoc, which still runs in an opaque origin thanks to the sandbox
 * without allow-same-origin: the work-in-progress code cannot touch the
 * writer's session, which is exactly the guarantee needed while pasting in
 * something a model generated.
 */

let shimPromise: Promise<string> | null = null

/** Downloads the shim from the content server, once. */
export function loadShim(contentBaseURL: string): Promise<string> {
  if (!shimPromise) {
    shimPromise = fetch(`${contentBaseURL}/__sfa/sdk.js`)
      .then((r) => (r.ok ? r.text() : ''))
      // Without the shim the preview still runs: it only loses persistence,
      // which hardly matters in a preview.
      .catch(() => '')
  }
  return shimPromise
}

export function useContentBase(): string {
  const { config } = useSession()
  return config?.content_base_url ?? ''
}

/** Builds the preview document, with the shim before everything else. */
export function buildPreview(shim: string, code: string): string {
  const preamble = shim ? `<script>${shim}</script>` : ''
  if (/<html[\s>]/i.test(code)) {
    if (/<head[\s>]/i.test(code)) {
      return code.replace(/<head([^>]*)>/i, (m) => `${m}${preamble}`)
    }
    return code.replace(/<html([^>]*)>/i, (m) => `${m}<head>${preamble}</head>`)
  }
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">${preamble}
</head><body>${code}</body></html>`
}

export const STARTER = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>New artifact</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      margin: 0; padding: 48px 20px;
      font: 16px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
      background: #fff; color: #111827;
    }
    main { max-width: 42rem; margin: 0 auto; }
    h1 { font-size: 2rem; margin: 0 0 .5em; }
  </style>
</head>
<body>
  <main>
    <h1>Hello</h1>
    <p>This artifact is yours. Write it however you like, or ask an agent to: it runs isolated, without network access.</p>
    <p>To keep data in the browser of whoever opens it:</p>
    <pre><code>Falconia.setSchemaVersion(1)
Falconia.onReady(({ state, save }) =&gt; {
  // state is what you saved last time, or null
  save({ seen: true })
})</code></pre>
  </main>
</body>
</html>
`
