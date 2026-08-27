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
