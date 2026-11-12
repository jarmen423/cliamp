// Keeps the app shell on the phone, so the installed app opens even when
// the machine it was installed from is off and another one plays.
// Network first: a running cliamp always serves its current app, and the
// cache is only the fallback. The API, art and info never touch the cache.
const CACHE = 'cliamp-shell-v5'
const SHELL = [
  './', 'index.html', 'app.css', 'manifest.webmanifest', 'icon.svg', 'icon-192.png', 'icon-512.png',
  'vendor/preact-htm.js',
  'core/api.js', 'core/conn.js', 'core/machines.js', 'core/store.js',
  'ui/app.js', 'ui/art.js', 'ui/artist.js', 'ui/hooks.js', 'ui/icons.js', 'ui/lists.js', 'ui/machines.js', 'ui/now.js', 'ui/openlink.js', 'ui/toast.js',
]

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()))
})

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (e) => {
  const req = e.request
  const url = new URL(req.url)
  if (req.method !== 'GET' || url.origin !== location.origin) return
  if (/^\/(v2|art\/|info)/.test(url.pathname)) return
  e.respondWith(
    fetch(req)
      .then((res) => {
        if (res.ok) {
          const copy = res.clone()
          caches.open(CACHE).then((c) => c.put(req, copy))
        }
        return res
      })
      .catch(() =>
        caches.match(req, { ignoreSearch: true }).then((hit) => hit || (req.mode === 'navigate' ? caches.match('./') : undefined)),
      ),
  )
})
