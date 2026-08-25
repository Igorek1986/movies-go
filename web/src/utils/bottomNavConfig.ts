// Mobile bottom bar (components/BottomNav) is user-configurable — which
// buttons show and in what order. Stored per-account on the server
// (users.bottom_nav_keys), not localStorage — same preference everywhere the
// user logs in, unlike e.g. theme which is deliberately per-device.

export interface BottomNavOption {
  key: string
  label: string
  to: string | null // null = action button (e.g. "Назад"), not a route
}

// Registry of everything the bar CAN show — order here is the default order
// and the order options are listed in the settings UI. Keep in sync with
// validBottomNavKeys in internal/api/auth.go.
export const BOTTOM_NAV_OPTIONS: BottomNavOption[] = [
  { key: 'back',     label: 'Назад',     to: null },
  { key: 'home',     label: 'Главная',   to: '/catalog' },
  { key: 'calendar', label: 'Календарь', to: '/calendar' },
  { key: 'mine',     label: 'Моё',       to: '/media-library' },
  { key: 'history',  label: 'История',   to: '/history' },
  { key: 'sessions', label: 'Сессии',    to: '/sessions' },
]

export const DEFAULT_KEYS = ['back', 'home', 'mine', 'history']
export const MIN_ITEMS = 2
export const MAX_ITEMS = 5

// `stored` is user.bottom_nav_keys from useAuth() — null/undefined/invalid
// falls back to the default set.
export function resolveBottomNavKeys(stored: string[] | null | undefined): string[] {
  if (!stored) return DEFAULT_KEYS
  const valid = stored.filter(k => BOTTOM_NAV_OPTIONS.some(o => o.key === k))
  return valid.length >= MIN_ITEMS ? valid : DEFAULT_KEYS
}

// Saves to the server and notifies any already-mounted Layout to pick up the
// change immediately (Layout gets its own useAuth() snapshot on mount, which
// won't otherwise refresh until the next navigation/remount).
export async function saveBottomNavKeys(keys: string[]): Promise<boolean> {
  const res = await fetch('/api/me/bottom-nav', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ keys }),
  })
  if (res.ok) {
    window.dispatchEvent(new CustomEvent<string[]>('bottomnav:update', { detail: keys }))
  }
  return res.ok
}
