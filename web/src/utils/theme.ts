export type ThemeId = 'classic' | 'glass'

// Регистр доступных тем — единственное место, которое нужно править при
// добавлении новой (плюс сам override-блок :root[data-theme='...'] в themes.scss).
export const THEMES: { id: ThemeId; label: string }[] = [
  { id: 'classic', label: 'Классическая' },
  { id: 'glass', label: 'Стекло' },
]

const STORAGE_KEY = 'app_theme'
const DEFAULT_THEME: ThemeId = 'classic'

// Local cache only, not the source of truth — see resolveTheme below for
// that. Used for the pre-React paint (index.html's own inline script reads
// this same localStorage key directly, before any JS module loads) and as
// this device's last-known value until /api/me resolves the real per-account
// theme, so a fresh load applies *something* correct-looking instantly
// instead of flashing the default while waiting on the network.
export function getStoredTheme(): ThemeId {
  const v = localStorage.getItem(STORAGE_KEY)
  return THEMES.some(t => t.id === v) ? (v as ThemeId) : DEFAULT_THEME
}

// `stored` is user.theme from useAuth() — the actual per-account value
// (server, see users.theme), same pattern as resolveCardLayout/
// resolveBrowseLayout/resolveSettingsLayout.
export function resolveTheme(stored: string | null | undefined): ThemeId {
  return THEMES.some(t => t.id === stored) ? (stored as ThemeId) : DEFAULT_THEME
}

export function applyTheme(theme: ThemeId) {
  if (theme === DEFAULT_THEME) {
    document.documentElement.removeAttribute('data-theme')
  } else {
    document.documentElement.setAttribute('data-theme', theme)
  }
  // Kept in sync even though the server is now the source of truth — this is
  // what lets index.html's pre-paint script (and getStoredTheme above) apply
  // the right theme instantly on this device's next load, before /api/me has
  // a chance to resolve.
  localStorage.setItem(STORAGE_KEY, theme)
}

export async function saveTheme(theme: ThemeId): Promise<boolean> {
  const res = await fetch('/api/me/interface', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ theme }),
  })
  return res.ok
}
