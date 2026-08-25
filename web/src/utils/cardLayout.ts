// Per-device visual preference for CardDetailPage (like theme — not
// per-account, see project convention: bottom-nav config is per-account,
// theme/this are per-device).

export type CardLayout = 'hero' | 'classic'

export const CARD_LAYOUTS: { id: CardLayout; label: string }[] = [
  { id: 'hero', label: 'Hero (постер на весь экран)' },
  { id: 'classic', label: 'Классический' },
]

const STORAGE_KEY = 'card_layout'
const DEFAULT_LAYOUT: CardLayout = 'hero'

export function getStoredCardLayout(): CardLayout {
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'classic' ? 'classic' : DEFAULT_LAYOUT
}

export function setStoredCardLayout(layout: CardLayout) {
  localStorage.setItem(STORAGE_KEY, layout)
}
