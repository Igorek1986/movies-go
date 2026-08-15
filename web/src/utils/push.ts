// Web Push subscribe/unsubscribe helpers — used by the notification toggle on
// the calendar page. One browser subscription per device+profile.

export function isPushSupported(): boolean {
  return 'serviceWorker' in navigator && 'PushManager' in window
}

async function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!isPushSupported()) return null
  try {
    return await navigator.serviceWorker.register('/sw.js')
  } catch {
    return null
  }
}

// Push subscription keys are raw bytes but transported as URL-safe base64.
function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padding = '='.repeat((4 - (base64.length % 4)) % 4)
  const b64 = (base64 + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(b64)
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
  return bytes
}

// Whether *this* browser/PWA install currently has an active push subscription —
// checked locally (registration + subscription object), not via the server's
// "does any subscription exist for this profile" — those can disagree after a
// reinstall: the old install's row lingers in the DB with no local subscription
// to match it, which used to make the toggle show "on" while unsubscribing was
// a silent no-op (nothing local to unsubscribe from).
export async function getPushStatus(): Promise<boolean> {
  if (!isPushSupported()) return false
  if (Notification.permission !== 'granted') return false
  try {
    const reg = await navigator.serviceWorker.getRegistration()
    const sub = await reg?.pushManager.getSubscription()
    return !!sub
  } catch {
    return false
  }
}

export async function subscribeToPush(token: string, profileId: string): Promise<boolean> {
  if (!isPushSupported()) return false
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') return false

  const reg = await registerServiceWorker()
  if (!reg) return false

  const keyRes = await fetch('/push/vapid-key')
  if (!keyRes.ok) return false
  const { key } = await keyRes.json()
  if (!key) return false

  let sub = await reg.pushManager.getSubscription()
  if (!sub) {
    sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(key),
    })
  }

  const json = sub.toJSON()
  const params = new URLSearchParams({ token, profile_id: profileId })
  const res = await fetch(`/push/subscribe?${params}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ endpoint: json.endpoint, keys: json.keys }),
  })
  return res.ok
}

export async function unsubscribeFromPush(token: string): Promise<boolean> {
  if (!isPushSupported()) return false
  const reg = await navigator.serviceWorker.getRegistration()
  const sub = await reg?.pushManager.getSubscription()
  if (!sub) return true

  const endpoint = sub.endpoint
  await sub.unsubscribe()

  const params = new URLSearchParams({ token })
  const res = await fetch(`/push/unsubscribe?${params}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ endpoint }),
  })
  return res.ok
}
