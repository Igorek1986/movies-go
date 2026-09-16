// Web Push service worker. Only handles push display and click routing — no
// asset caching/offline support here, that's a separate concern.

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', event => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('push', event => {
  let data = {}
  try { data = event.data ? event.data.json() : {} } catch {}

  // Multiple new episodes across different shows in one check (see
  // push_notify.go) arrive as ONE push message carrying a "notifications"
  // array, so the device buzzes/alerts once instead of once per show — this
  // fans it back out into one stacked notification card per show. Each gets
  // its own tag (the card url) so re-checks replace that show's card instead
  // of piling up duplicates, and every card after the first is silent so
  // only the first one triggers the audible/haptic alert.
  if (Array.isArray(data.notifications) && data.notifications.length) {
    event.waitUntil(
      Promise.all(data.notifications.map((n, i) =>
        self.registration.showNotification(n.title || 'Movies API', {
          body: n.body || '',
          icon: '/web-app-manifest-192x192.png',
          badge: '/web-app-manifest-192x192.png',
          tag: n.url || undefined,
          silent: i > 0,
          data: { url: n.url || '/' },
        })
      ))
    )
    return
  }

  const title = data.title || 'Movies API'
  const options = {
    body: data.body || '',
    icon: '/web-app-manifest-192x192.png',
    badge: '/web-app-manifest-192x192.png',
    data: { url: data.url || '/' },
  }

  event.waitUntil(self.registration.showNotification(title, options))
})

self.addEventListener('notificationclick', event => {
  event.notification.close()
  const url = (event.notification.data && event.notification.data.url) || '/'

  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(clientsList => {
      for (const client of clientsList) {
        if ('focus' in client) {
          client.navigate(url)
          return client.focus()
        }
      }
      if (self.clients.openWindow) return self.clients.openWindow(url)
    })
  )
})
