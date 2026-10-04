import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

/**
 * In development the dev server sets the same isolation headers the backend
 * sends in production, so a difference in behavior does not first show up on
 * the first deploy.
 *
 * COEP is `credentialless`, not `require-corp`: artifacts come from another
 * origin and require-corp would refuse them, while credentialless keeps the
 * context isolated and lets them through without credentials.
 */
export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    headers: {
      'Cross-Origin-Opener-Policy': 'same-origin',
      'Cross-Origin-Embedder-Policy': 'credentialless',
      'X-Frame-Options': 'SAMEORIGIN',
    },
  },
  build: { sourcemap: false },
})
