// Per-account visual preference for CardDetailPage — stored server-side
// (users.card_layout, see /api/me and handleSaveInterfacePrefs), same as
// bottom_nav_keys/position, so it follows the account across devices.

export type CardLayout = 'hero' | 'classic'

export const CARD_LAYOUTS: { id: CardLayout; label: string }[] = [
  { id: 'hero', label: 'Hero (постер на весь экран)' },
  { id: 'classic', label: 'Классический' },
]

const DEFAULT_LAYOUT: CardLayout = 'hero'

// `stored` is user.card_layout from useAuth().
export function resolveCardLayout(stored: string | null | undefined): CardLayout {
  return stored === 'classic' ? 'classic' : DEFAULT_LAYOUT
}

export async function saveCardLayout(layout: CardLayout): Promise<boolean> {
  const res = await fetch('/api/me/interface', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ card_layout: layout }),
  })
  return res.ok
}
