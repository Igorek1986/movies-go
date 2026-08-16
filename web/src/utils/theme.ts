export type ThemeId = 'classic' | 'glass'

// Регистр доступных тем — единственное место, которое нужно править при
// добавлении новой (плюс сам override-блок :root[data-theme='...'] в themes.scss).
export const THEMES: { id: ThemeId; label: string }[] = [
  { id: 'classic', label: 'Классическая' },
  { id: 'glass', label: 'Стекло' },
]

const STORAGE_KEY = 'app_theme'
const DEFAULT_THEME: ThemeId = 'classic'

export function getStoredTheme(): ThemeId {
  const v = localStorage.getItem(STORAGE_KEY)
  return THEMES.some(t => t.id === v) ? (v as ThemeId) : DEFAULT_THEME
}

export function applyTheme(theme: ThemeId) {
  if (theme === DEFAULT_THEME) {
    document.documentElement.removeAttribute('data-theme')
  } else {
    document.documentElement.setAttribute('data-theme', theme)
  }
  localStorage.setItem(STORAGE_KEY, theme)
}
