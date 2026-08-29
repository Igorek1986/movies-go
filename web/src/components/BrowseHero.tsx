import { useCallback, useEffect, useRef, useState } from 'react'
import { posterUrl, tmdbUrl } from '@/utils/poster'
import { statusLabel } from '@/utils/mediaFormat'
import styles from './BrowseHero.module.scss'

// Lightweight fields already present in CatalogPage's MediaItem and
// MediaLibraryPage's LibraryItem — both satisfy this structurally, no
// change needed to either interface.
export interface HeroLiteItem {
  id: number
  media_type: string
  title?: string
  name?: string
  poster_path: string | null
  vote_average?: number
  release_date?: string
  first_air_date?: string
  certification_ru?: string
  release_quality?: string
}

interface HeroGenre { id: number; name: string }

// Subset of CardDetailPage's CardDetail — only what the hero renders.
export interface HeroDetail {
  backdrop_path: string
  overview: string
  genres: HeroGenre[]
  status: string
  number_of_seasons: number
  number_of_episodes: number
  age_rating: number
  certification_ru: string
  vote_average: number
  year: string
}

const DEBOUNCE_MS = 200

// Debounced, cached fetch of full card detail for whichever item currently
// has focus/hover in a row below — the list endpoints (MediaItem/LibraryItem)
// don't carry backdrop/overview/genres/status, so the hero upgrades from the
// lite item to a GET /api/media-card/{cardId} result once it settles.
export function useHeroPreview<T extends HeroLiteItem>() {
  const [item, setItem] = useState<T | null>(null)
  const [detail, setDetail] = useState<HeroDetail | null>(null)
  const activeCardIdRef = useRef<string | null>(null)
  const cacheRef = useRef(new Map<string, HeroDetail>())
  const timerRef = useRef<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const activate = useCallback((next: T) => {
    const cardId = `${next.id}_${next.media_type}`
    if (activeCardIdRef.current === cardId) return
    activeCardIdRef.current = cardId
    setItem(next)

    const cached = cacheRef.current.get(cardId)
    if (cached) { setDetail(cached); return }
    setDetail(null)

    if (timerRef.current) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => {
      abortRef.current?.abort()
      const ac = new AbortController()
      abortRef.current = ac
      fetch(`/api/media-card/${cardId}`, { signal: ac.signal })
        .then(r => r.ok ? r.json() : Promise.reject())
        .then((d: HeroDetail) => {
          cacheRef.current.set(cardId, d)
          if (activeCardIdRef.current === cardId) setDetail(d)
        })
        .catch(() => {})
    }, DEBOUNCE_MS)
  }, [])

  useEffect(() => () => {
    if (timerRef.current) window.clearTimeout(timerRef.current)
    abortRef.current?.abort()
  }, [])

  return { item, detail, activate }
}

function heroYear(item: HeroLiteItem, detail: HeroDetail | null): string {
  if (detail?.year) return detail.year
  return (item.release_date || item.first_air_date || '').slice(0, 4)
}

// Like int.js's backgroundLast: keep showing whatever's currently on screen
// until the next image has actually finished loading, instead of flashing a
// stretched poster every time a card is still fetching its real backdrop.
// The poster fallback only ever appears when there's truly nothing better —
// the very first activation (nothing shown yet) or a card confirmed (detail
// resolved) to have no backdrop at all.
function useCrossfadeBg(item: HeroLiteItem | null, backdropSrc: string | null, detailResolved: boolean) {
  const [bg, setBg] = useState<{ src: string; isPoster: boolean } | null>(null)
  const targetRef = useRef<string | null>(null)

  useEffect(() => {
    if (!item) return
    const posterSrc = posterUrl(item.poster_path)

    if (backdropSrc) {
      if (targetRef.current === backdropSrc) return
      targetRef.current = backdropSrc
      const img = new Image()
      img.onload = () => { if (targetRef.current === backdropSrc) setBg({ src: backdropSrc, isPoster: false }) }
      img.src = backdropSrc
      return
    }

    if (detailResolved) {
      // Confirmed: this card has no backdrop — fall back to its poster.
      if (!posterSrc) {
        // Neither backdrop nor poster exists — clear the display instead of
        // leaving the previous card's image up, which would misleadingly
        // suggest it still belongs to whatever's now focused.
        if (targetRef.current !== null) { targetRef.current = null; setBg(null) }
        return
      }
      if (targetRef.current === posterSrc) return
      targetRef.current = posterSrc
      const img = new Image()
      img.onload = () => { if (targetRef.current === posterSrc) setBg({ src: posterSrc, isPoster: true }) }
      img.src = posterSrc
      return
    }

    // Still fetching this card's detail — keep whatever's already displayed.
    // Only show a placeholder if this is the very first activation ever.
    setBg(prev => {
      if (prev) return prev
      targetRef.current = posterSrc
      return posterSrc ? { src: posterSrc, isPoster: true } : null
    })
  }, [item, backdropSrc, detailResolved])

  return bg
}

export function BrowseHero({ item, detail, onOpen }: {
  item: HeroLiteItem | null
  detail: HeroDetail | null
  onOpen: () => void
}) {
  const backdropSrc = item && detail?.backdrop_path ? tmdbUrl(detail.backdrop_path, 'w1280') : null
  const bg = useCrossfadeBg(item, backdropSrc, detail !== null)

  if (!item) return null

  const isTV = item.media_type === 'tv'
  const title = item.title || item.name || ''
  const rating = detail?.vote_average ?? item.vote_average ?? 0
  const certification = detail?.certification_ru || item.certification_ru
  const year = heroYear(item, detail)
  const genres = detail?.genres ?? []
  const status = detail?.status ? statusLabel(detail.status) : ''

  const tags: string[] = []
  if (year) tags.push(year)
  if (certification) tags.push(certification.endsWith('+') ? certification : certification + '+')
  if (isTV && detail?.number_of_seasons) tags.push(`Сезонов ${detail.number_of_seasons}`)
  if (isTV && detail?.number_of_episodes) tags.push(`Эпизодов ${detail.number_of_episodes}`)
  if (detail?.age_rating) tags.push(`${detail.age_rating}+`)
  if (status) tags.push(status)
  if (item.release_quality) tags.push(item.release_quality)

  return (
    <>
      {/* Fixed full-viewport backdrop — sits behind everything (rows included)
          so it stays visible no matter how far the page scrolls, like the
          int.js Lampa plugin's full-screen background. */}
      <div className={styles.heroBg} aria-hidden>
        {bg && (
          <img
            src={bg.src}
            alt=""
            className={bg.isPoster ? styles.bgPoster : undefined}
          />
        )}
      </div>
      <div className={styles.hero} onClick={onOpen}>
        <div className={styles.content}>
          <h2 className={styles.title}>{title}</h2>
          <div className={styles.tags}>
            {isTV && <span className={styles.tagType}>Сериал</span>}
            {tags.map((t, i) => <span key={i} className={styles.tag}>{t}</span>)}
            {rating > 0 && <span className={styles.tagRating}>★ {rating.toFixed(1)} TMDB</span>}
          </div>
          {genres.length > 0 && (
            <div className={styles.genres}>
              {genres.map(g => <span key={g.id} className={styles.genre}>{g.name}</span>)}
            </div>
          )}
          {detail?.overview && <p className={styles.descr}>{detail.overview}</p>}
        </div>
      </div>
    </>
  )
}
