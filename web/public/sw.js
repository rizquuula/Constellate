// Constellate service worker — network-first, no precache.
//
// Rationale: Constellate is a live control plane. A stale cached shell could
// silently hide real fleet state, so online users must ALWAYS get the network
// response. The cache is only a last-resort offline fallback for static assets
// (the app shell / icons) that were already fetched successfully this session.
//
// Hard rule: never touch API or WebSocket traffic, and never cache anything but
// same-origin GET requests. Terminal I/O and REST calls must reach the network
// untouched.

const CACHE_NAME = 'constellate-v1'

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys()
      await Promise.all(
        names.filter((name) => name !== CACHE_NAME).map((name) => caches.delete(name))
      )
      await self.clients.claim()
    })()
  )
})

function isCacheable(request) {
  if (request.method !== 'GET') return false
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return false
  if (url.pathname.startsWith('/api/')) return false
  if (url.pathname.startsWith('/ws/')) return false
  return true
}

self.addEventListener('fetch', (event) => {
  if (!isCacheable(event.request)) return
  event.respondWith(
    (async () => {
      try {
        const response = await fetch(event.request)
        // Only cache successful responses — a cached 404/500 would be replayed
        // as the offline fallback. put() is fire-and-forget; a failure (e.g. a
        // 206 partial) must not break the live response.
        if (response.ok) {
          const cache = await caches.open(CACHE_NAME)
          cache.put(event.request, response.clone()).catch(() => {})
        }
        return response
      } catch (err) {
        const cached = await caches.match(event.request)
        if (cached) return cached
        throw err
      }
    })()
  )
})
