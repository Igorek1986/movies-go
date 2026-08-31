// Per-device visual preference for ProfilesPage (like cardLayout.ts/
// browseLayout.ts — not per-account): 'classic' keeps today's multi-open
// <details> grid untouched, 'remote' switches to a single-open-at-a-time
// accordion built for keyboard/TV-remote navigation (custom select/confirm/
// prompt instead of native ones). Defaults to 'classic' so existing users
// see no change until they opt in.

export type SettingsLayout = 'classic' | 'remote'

export const SETTINGS_LAYOUTS: { id: SettingsLayout; label: string }[] = [
  { id: 'classic', label: 'Классический' },
  { id: 'remote', label: 'Управление (для клавиатуры/пульта)' },
]

const STORAGE_KEY = 'settings_layout'
const DEFAULT_LAYOUT: SettingsLayout = 'classic'

export function getStoredSettingsLayout(): SettingsLayout {
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'remote' ? 'remote' : DEFAULT_LAYOUT
}

export function setStoredSettingsLayout(layout: SettingsLayout) {
  localStorage.setItem(STORAGE_KEY, layout)
}
