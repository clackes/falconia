/* Farewell service worker.
 *
 * Earlier versions of the application registered a service worker that cached
 * the PWA shell. It is no longer used: a cached shell pinned people to an old
 * version of the interface after every update.
 *
 * The file stays, and stays empty on purpose: whoever still has the old worker
 * registered receives this one, which uninstalls itself and clears the caches.
 * Without a file here, that worker would live forever.
 */
self.addEventListener('install', () => self.skipWaiting())

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys()
    await Promise.all(keys.map((k) => caches.delete(k)))
    await self.registration.unregister()
    const clients = await self.clients.matchAll({ type: 'window' })
    // The reload is needed: the tab open right now is still using responses
    // served by the worker that just removed itself.
    clients.forEach((c) => c.navigate(c.url))
  })())
})
