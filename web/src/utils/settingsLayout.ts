// Per-account visual preference for ProfilesPage (like cardLayout.ts/
// browseLayout.ts — same per-account storage, see users.settings_layout):
// 'classic' keeps today's multi-open <details> grid, 'remote' switches to a
// single-open-at-a-time accordion built for keyboard/TV-remote navigation
// (custom select/confirm/prompt instead of native ones). Defaults to
// 'remote' — the keyboard/pult-friendly view — since that's the primary
// target for this app; existing accounts with no stored value get it too.

export type SettingsLayout = 'classic' | 'remote'

export const SETTINGS_LAYOUTS: { id: SettingsLayout; label: string }[] = [
  { id: 'remote', label: 'Управление (для клавиатуры/пульта)' },
  { id: 'classic', label: 'Классический' },
]

const DEFAULT_LAYOUT: SettingsLayout = 'remote'

// `stored` is user.settings_layout from useAuth().
export function resolveSettingsLayout(stored: string | null | undefined): SettingsLayout {
  return stored === 'classic' ? 'classic' : DEFAULT_LAYOUT
}

export async function saveSettingsLayout(layout: SettingsLayout): Promise<boolean> {
  const res = await fetch('/api/me/interface', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ settings_layout: layout }),
  })
  return res.ok
}
