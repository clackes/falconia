import { loader } from '@monaco-editor/react'

const BASE = '/monaco'

/**
 * Monaco is served from this same origin, not from a CDN.
 *
 * The library's default is to download it from jsdelivr, which on a
 * self-hosted server without internet access means an editor that never opens
 * and no message explaining why. The files are copied to public/monaco/vs by
 * `npm run monaco`, which runs before every build and every dev start.
 *
 * The folder must be called exactly "vs": Monaco derives its workers' path by
 * removing the last segment of this value and appending "/vs" again.
 */
export function configureMonaco() {
  loader.config({ paths: { vs: `${BASE}/vs` } })

  // Language workers are born from a blob URL, and inside such a worker a
  // relative path has nothing to be relative to: the fetch fails and
  // completion silently disappears. So an absolute address is passed, built at
  // runtime because the instance can live on any domain.
  const origin = window.location.origin
  ;(window as any).MonacoEnvironment = {
    getWorkerUrl(_moduleId: string, _label: string) {
      const bootstrap = `
        self.MonacoEnvironment = { baseUrl: '${origin}${BASE}/' };
        importScripts('${origin}${BASE}/vs/base/worker/workerMain.js');
      `
      return URL.createObjectURL(new Blob([bootstrap], { type: 'text/javascript' }))
    },
  }
}
