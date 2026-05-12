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
    if (_imgProxy) {
      // Strip https://image.tmdb.org/ → t/p/w300/abc.jpg
      const rel = path.replace(/^https?:\/\/[^/]+\//, '')
      return `${_imgProxy}/${rel}`
    }
    return path.replace(/^http:\/\//, 'https://')
  }

  // Bare path like /59LSkgXgvRNSlKCA1xwMd0ElXqj.jpg
  if (_imgProxy) {
    return `${_imgProxy}/t/p/${size}${path}`
  }
  return `${TMDB_BASE}/${size}${path}`
}

// Alias with default size — use for poster thumbnails.
// w342 covers catalog cards (~185px) at 2x retina and mobile 2-column layout (~300px).
export function posterUrl(path: string | null | undefined, size = 'w342'): string | null {
  return tmdbUrl(path, size)
}

