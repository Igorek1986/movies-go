// Per-device visual preference for CatalogPage/MediaLibraryPage (like
// cardLayout.ts for CardDetailPage — not per-account).

export type BrowseLayout = 'hero' | 'classic'

export const BROWSE_LAYOUTS: { id: BrowseLayout; label: string }[] = [
  { id: 'hero', label: 'Hero (фон активной карточки)' },
  { id: 'classic', label: 'Классический' },
]

const STORAGE_KEY = 'browse_layout'
const DEFAULT_LAYOUT: BrowseLayout = 'hero'

export function getStoredBrowseLayout(): BrowseLayout {
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'classic' ? 'classic' : DEFAULT_LAYOUT
}

export function setStoredBrowseLayout(layout: BrowseLayout) {
  localStorage.setItem(STORAGE_KEY, layout)
}

// The hero view is a non-scrolling carousel driven by keyboard/mouse focus —
// there's no way to move focus onto a card by touch (no hover, no arrow
// keys), so a touch-primary device always gets Classic regardless of the
// saved preference. `hover: none` + `pointer: coarse` is the standard way to
// detect that (unlike a width check, it isn't fooled by a narrow desktop
// window or a large tablet).
function isTouchPrimary(): boolean {
  return typeof window !== 'undefined' && window.matchMedia?.('(hover: none), (pointer: coarse)').matches
}

export function getEffectiveBrowseLayout(): BrowseLayout {
  return isTouchPrimary() ? 'classic' : getStoredBrowseLayout()
}
