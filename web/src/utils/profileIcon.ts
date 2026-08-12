export const PROFILE_ICON_EXTS: Record<string, string> = {
  id1: 'png', id2: 'png', id3: 'png', id4: 'png', id5: 'png', id6: 'png', id7: 'png',
  id8: 'svg', id9: 'svg', id10: 'svg', id11: 'svg', id12: 'svg', id13: 'png',
  id14: 'svg', id15: 'svg', id16: 'svg', id17: 'svg', id18: 'svg',
}

export const PROFILE_ICON_IDS = Object.keys(PROFILE_ICON_EXTS)

export function profileIconSrc(id: string): string {
  const ext = PROFILE_ICON_EXTS[id] ?? 'svg'
  return `/static/profileIcons/${id}.${ext}`
}

export function profileInitials(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) return '?'
  const parts = trimmed.split(/\s+/)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return trimmed.slice(0, 2).toUpperCase()
}
