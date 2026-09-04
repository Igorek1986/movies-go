const TMDB_BASE = 'https://image.tmdb.org/t/p'

let _imgProxy = ''

export function setImgProxy(url: string) {
  _imgProxy = url.replace(/\/$/, '')
}

// Build a TMDB image URL, routing through imgproxy if configured.
// path may be a bare path (/abc.jpg) or a full URL (http://image.tmdb.org/...).
export function tmdbUrl(path: string | null | undefined, size = 'w300'): string | null {
  if (!path) return null

  if (path.startsWith('http://') || path.startsWith('https://')) {
    // media_cards.backdrop_path/poster_path is saved as a full URL with
    // whatever size the admin's backdrop_size/poster_size setting picked at
    // scrape time baked in (can be 'original' — the full, unresized TMDB
    // file, several MB) — always swap in the size THIS call site actually
    // wants instead of keeping that one, or every image everywhere
    // (including small row thumbnails) would download at that baked-in
    // size regardless of what's actually being rendered.
    const m = path.match(/\/t\/p\/[^/]+(\/[^/]+)$/)
    const filePath = m?.[1]
    if (_imgProxy) {
      if (filePath) return `${_imgProxy}/t/p/${size}${filePath}`
      // Fallback for a URL that doesn't match TMDB's /t/p/<size>/<file> shape.
      const rel = path.replace(/^https?:\/\/[^/]+\//, '')
      return `${_imgProxy}/${rel}`
    }
    if (filePath) return `${TMDB_BASE}/${size}${filePath}`
    return path.replace(/^http:\/\//, 'https://')
  }

  // Bare path like /59LSkgXgvRNSlKCA1xwMd0ElXqj.jpg
  if (_imgProxy) {
    return `${_imgProxy}/t/p/${size}${path}`
  }
  return `${TMDB_BASE}/${size}${path}`
}

// Alias with default size — use for poster thumbnails.
// w500 covers catalog cards (~185px) at 2x+ retina and mobile 2-column layout (~300px) with headroom.
export function posterUrl(path: string | null | undefined, size = 'w500'): string | null {
  return tmdbUrl(path, size)
}

