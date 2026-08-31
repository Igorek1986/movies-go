// Per-account visual preference for CatalogPage/MediaLibraryPage — stored
// server-side (users.browse_layout, see /api/me and
// handleSaveInterfacePrefs), same as cardLayout.ts.

export type BrowseLayout = 'hero' | 'classic'

export const BROWSE_LAYOUTS: { id: BrowseLayout; label: string }[] = [
  { id: 'hero', label: 'Hero (фон активной карточки)' },
  { id: 'classic', label: 'Классический' },
]

const DEFAULT_LAYOUT: BrowseLayout = 'hero'

// `stored` is user.browse_layout from useAuth().
export function resolveBrowseLayout(stored: string | null | undefined): BrowseLayout {
  return stored === 'classic' ? 'classic' : DEFAULT_LAYOUT
}

export async function saveBrowseLayout(layout: BrowseLayout): Promise<boolean> {
  const res = await fetch('/api/me/interface', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ browse_layout: layout }),
  })
  return res.ok
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

// `stored` is user.browse_layout from useAuth().
export function getEffectiveBrowseLayout(stored: string | null | undefined): BrowseLayout {
  return isTouchPrimary() ? 'classic' : resolveBrowseLayout(stored)
}
