/// <reference lib="webworker" />
/* eslint-disable no-restricted-globals */

// Auto-replaced by scripts/gen-sw.js during build. The literal default keeps
// the file runnable when read directly (e.g. in dev, where it is unused).
const BUILD_VERSION = '__BUILD_VERSION__'
const SHELL_CACHE = `vote-shell-${BUILD_VERSION}`
const ASSET_CACHE = `vote-asset-${BUILD_VERSION}`

// Same-origin navigation requests: HTML documents.
// We store the canonical shell URL as a string and match caches by URL
// rather than constructing a Request with mode: 'navigate' — the spec
// reserves that mode for user-agent-initiated navigations and rejects
// programmatic construction in some engines.
const SHELL_URL = '/formateur/'

// Asset extensions we treat as static build outputs.
const STATIC_ASSET_EXTS = new Set([
  '.js',
  '.mjs',
  '.css',
  '.woff',
  '.woff2',
  '.png',
  '.jpg',
  '.jpeg',
  '.svg',
  '.webmanifest',
  '.ico'
])

self.addEventListener('install', (event) => {
  // Pre-cache the formateur shell so the very first offline load works
  // immediately after install. Hashed build assets are picked up lazily
  // by the fetch handler (stale-while-revalidate).
  event.waitUntil(
    (async () => {
      const cache = await caches.open(SHELL_CACHE)
      await Promise.allSettled([
        cache.add(new Request(SHELL_URL, { cache: 'reload' })),
        cache.add(new Request('/manifest.webmanifest', { cache: 'reload' })),
        cache.add(new Request('/icons/icon-192.png', { cache: 'reload' })),
        cache.add(new Request('/icons/icon-512.png', { cache: 'reload' }))
      ])
      await self.skipWaiting()
    })()
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys()
      await Promise.all(
        keys
          .filter((key) => !key.endsWith(BUILD_VERSION))
          .map((key) => caches.delete(key))
      )
      await self.clients.claim()
    })()
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request

  // Only handle GET; bypass everything else (POST, WS upgrade, etc.)
  if (req.method !== 'GET') return

  const url = new URL(req.url)

  // Same-origin only. WS, cross-origin fonts/CDNs go straight to network.
  if (url.origin !== self.location.origin) return

  // WebSocket upgrades never reach the cache.
  if (req.headers.get('upgrade') === 'websocket') return

  // Skip Vite dev server paths — should never happen in prod but be safe.
  if (url.pathname.startsWith('/@') || url.pathname.includes('/__vite')) return

  // HTML navigations: network-first, fall back to cached shell when offline.
  if (req.mode === 'navigate') {
    event.respondWith(networkFirstNavigation(req))
    return
  }

  // Static assets: stale-while-revalidate.
  const ext = url.pathname.slice(url.pathname.lastIndexOf('.'))
  if (STATIC_ASSET_EXTS.has(ext)) {
    event.respondWith(staleWhileRevalidate(req))
  }

  // Anything else: let the browser handle it.
})

async function networkFirstNavigation(req) {
  const cache = await caches.open(SHELL_CACHE)
  try {
    const fresh = await fetch(req)
    // Only cache successful, basic-type (HTML) responses.
    if (fresh && fresh.ok && fresh.type === 'basic') {
      cache.put(SHELL_URL, fresh.clone())
    }
    return fresh
  } catch (err) {
    // Offline — try the exact request first, then the canonical shell URL.
    const cachedExact = await cache.match(req)
    if (cachedExact) return cachedExact
    const cachedShell = await cache.match(SHELL_URL)
    if (cachedShell) return cachedShell
    throw err
  }
}

async function staleWhileRevalidate(req) {
  const cache = await caches.open(ASSET_CACHE)
  const cached = await cache.match(req)
  const networkFetch = fetch(req)
    .then((res) => {
      if (res && res.ok && res.type === 'basic') {
        cache.put(req, res.clone())
      }
      return res
    })
    .catch(() => null)

  // Return cached immediately if available; otherwise wait for network.
  return cached || (await networkFetch)
}

// Allow main thread to force activation of a waiting SW (update flow).
self.addEventListener('message', (event) => {
  if (event.data === 'SKIP_WAITING') {
    self.skipWaiting()
  }
})
