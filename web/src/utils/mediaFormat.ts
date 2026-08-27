// Shared formatting helpers for TMDB-derived card fields — used by
// CardDetailPage and BrowseHero so both show identical labels.

export function qualityLabel(q: number) {
  if (q >= 300) return '4K'; if (q >= 200) return '1080p'; if (q >= 100) return '720p'; return 'SD'
}

export function runtimeLabel(m: number) {
  if (!m) return ''; const h = Math.floor(m / 60); const r = m % 60
  if (!h) return `${r} мин`; return r ? `${h} ч ${r} мин` : `${h} ч`
}

// Same mapping as plugins/status.js's addStatusBadge, for consistent wording
// between the Lampa plugin and the web UI.
export function statusLabel(status: string): string {
  switch (status.toLowerCase()) {
    case 'ended': return 'Завершён'
    case 'on hiatus':
    case 'paused': return 'Пауза'
    case 'canceled': return 'Отменен'
    case 'returning series':
    case 'airing':
    case 'in production': return 'В эфире'
    default: return ''
  }
}
