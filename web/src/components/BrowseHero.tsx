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
  // The list endpoints (categories/media-library) already carry this — using
  // it lets the real backdrop show immediately on activation instead of
  // waiting on the full /api/media-card/{id} fetch below (detail) to resolve
  // first, which is what caused a poster flash on a card genuinely never
  // seen this session (see useCrossfadeBg's isPoster fallback).
  backdrop_path?: string | null
  vote_average?: number
  release_date?: string
  first_air_date?: string
  certification_ru?: string
  release_quality?: string
  // "Непросмотренные" only (see CatalogPage's MediaCard) — next unwatched
  // episode + watch progress for the show, shown here instead of/alongside
  // the card's own overlay badges.
  next_episode?: string
  watched_count?: number
  aired_count?: number
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

// Module-level, not component-scoped: CatalogPage/MediaLibraryPage fully
// unmount and remount on every navigation away and back (a card detail page
// round trip, switching between Каталог/Моё, even a fresh app open landing
// there) — a useRef-scoped cache would reset to empty on every single one of
// those, refetching detail for a card already seen this session and, while
// that fetch is in flight, falling back to the poster (see useCrossfadeBg
// below) — a portrait image cover-cropped into this wide hero box reads as a
// jarring "zoomed in" flash right before the real backdrop pops in a moment
// later. Keeping this alive across mounts makes a revisit show the correct
// backdrop immediately, no flash — only a card genuinely never seen this
// session still has to wait on the real fetch.
const _heroDetailCache = new Map<string, HeroDetail>()

// Last item activated per caller (CatalogPage passes 'catalog', MediaLibraryPage
// 'media-library') — module-level for the same reason as _heroDetailCache: a
// CardDetailPage round trip (or any full remount of whoever calls
// useHeroPreview) fully unmounts this hook's own item/detail state, so
// without this the hero necessarily renders nothing (BrowseHero's own
// `if (!item) return null`) for at least one commit, then fades a fresh copy
// of the exact same image back in — visibly reloading a backdrop that was
// already fully on-screen a moment ago on the card detail page it just came
// from. Seeding item/detail/bg/layers (see useCrossfadeBg/useBgLayers below)
// synchronously from this cache on mount skips both the blank gap and the
// fade for that one restored frame; anything genuinely new still goes
// through the normal fetch-then-crossfade path untouched.
const _lastHeroItemCache = new Map<string, HeroLiteItem>()

// Debounced, cached fetch of full card detail for whichever item currently
// has focus/hover in a row below — the list endpoints (MediaItem/LibraryItem)
// don't carry backdrop/overview/genres/status, so the hero upgrades from the
// lite item to a GET /api/media-card/{cardId} result once it settles.
export function useHeroPreview<T extends HeroLiteItem>(cacheKey: string) {
  const restored = _lastHeroItemCache.get(cacheKey) as T | undefined
  const [item, setItem] = useState<T | null>(restored ?? null)
  const [detail, setDetail] = useState<HeroDetail | null>(() => {
    if (!restored) return null
    return _heroDetailCache.get(`${restored.id}_${restored.media_type}`) ?? null
  })
  const activeCardIdRef = useRef<string | null>(restored ? `${restored.id}_${restored.media_type}` : null)
  const timerRef = useRef<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  const activate = useCallback((next: T) => {
    const cardId = `${next.id}_${next.media_type}`
    if (activeCardIdRef.current === cardId) return
    activeCardIdRef.current = cardId
    setItem(next)
    _lastHeroItemCache.set(cacheKey, next)

    const cached = _heroDetailCache.get(cardId)
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
          _heroDetailCache.set(cardId, d)
          if (activeCardIdRef.current === cardId) setDetail(d)
        })
        .catch(() => {})
    }, DEBOUNCE_MS)
  }, [cacheKey])

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
  // Seeded straight from backdropSrc, not null — when `item` itself was
  // restored from useHeroPreview's cache (see its comment), backdropSrc is
  // already resolvable on this very first render, and the browser almost
  // certainly still has the image decoded from CardDetailPage a moment ago.
  // Going through the normal empty-then-`new Image()`-then-onload path here
  // would still show a blank/placeholder frame first and only THEN reveal
  // an image that was, in reality, already fully visible on screen. Seeding
  // targetRef to match means the effect below finds nothing to do on mount
  // (its own `targetRef.current === backdropSrc` check short-circuits it) —
  // a genuinely new backdropSrc still goes through the real preload-then-
  // swap path untouched.
  const [bg, setBg] = useState<{ src: string; isPoster: boolean } | null>(
    () => backdropSrc ? { src: backdropSrc, isPoster: false } : null
  )
  const targetRef = useRef<string | null>(backdropSrc)

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

// Also passed inline as .bgLayer's transition-duration (see the style prop
// below) so the CSS fade and the "outgoing layer is fully hidden, safe to
// drop from the DOM" timer below are always the same number, not two
// independent literals that happen to match.
const CROSSFADE_MS = 500

interface BgLayer { key: number; src: string; isPoster: boolean }

// Renders bg (picked by useCrossfadeBg above) as a stack of <img> layers
// instead of swapping a single element's src — a plain src swap has no
// previous frame to fade FROM, so it just pops the instant the new image
// finishes loading. Each new src gets its own layer, mounted at opacity 0
// and bumped to "active" a tick later so the CSS opacity transition (see
// .bgLayer/.bgLayerActive) has something to animate from; only the newest
// layer ever carries the "active" class, so marking it active simultaneously
// un-marks whatever was previously active — the two layers' opacities cross
// in opposite directions (old 0.5→0, new 0→0.5) at the same time, which is
// exactly the dissolve a crossfade is meant to look like. The old layer is
// dropped from the DOM once its own fade-out has had time to finish.
function useBgLayers(bg: { src: string; isPoster: boolean } | null) {
  // Seeded already-active when `bg` itself arrives pre-seeded (see
  // useCrossfadeBg above) — otherwise this restored frame would still mount
  // at opacity 0 and animate up over CROSSFADE_MS, dissolving in an image
  // that was already fully opaque on screen a moment ago. key 0 is reserved
  // for exactly this seeded layer; keyRef starts at 0 too so the next real
  // new layer still gets 1, 2, 3… with no collision.
  const [layers, setLayers] = useState<BgLayer[]>(() => bg ? [{ key: 0, src: bg.src, isPoster: bg.isPoster }] : [])
  const [activeKey, setActiveKey] = useState<number | null>(() => bg ? 0 : null)
  const keyRef = useRef(0)

  useEffect(() => {
    if (!bg) { setLayers([]); setActiveKey(null); return }
    setLayers(prev => {
      if (prev.length && prev[prev.length - 1].src === bg.src) return prev
      return [...prev, { key: ++keyRef.current, src: bg.src, isPoster: bg.isPoster }]
    })
  }, [bg])

  useEffect(() => {
    const newest = layers[layers.length - 1]
    if (!newest || newest.key === activeKey) return
    const raf = requestAnimationFrame(() => setActiveKey(newest.key))
    return () => cancelAnimationFrame(raf)
  }, [layers, activeKey])

  useEffect(() => {
    if (layers.length <= 1 || activeKey === null) return
    const t = window.setTimeout(() => {
      setLayers(prev => prev.filter(l => l.key === activeKey))
    }, CROSSFADE_MS)
    return () => window.clearTimeout(t)
  }, [layers, activeKey])

  return { layers, activeKey }
}

export function BrowseHero({ item, detail, onOpen }: {
  item: HeroLiteItem | null
  detail: HeroDetail | null
  onOpen: () => void
}) {
  // Prefer the list item's own backdrop_path (already there the instant this
  // item is focused) over detail's — detail carries the same field, but only
  // once its own fetch resolves (see backdrop_path's comment on HeroLiteItem).
  const rawBackdrop = item?.backdrop_path || detail?.backdrop_path
  const backdropSrc = item && rawBackdrop ? tmdbUrl(rawBackdrop, 'w1280') : null
  const bg = useCrossfadeBg(item, backdropSrc, detail !== null)
  const { layers, activeKey } = useBgLayers(bg)

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

  // "Непросмотренные" progress — see MediaCard's identical unwatchedBadge/
  // nextEpBadge/progress overlay. Shown here too (not instead, for now — the
  // per-card version is the only place this is visible for a card that
  // ISN'T the currently focused one) so the two can be compared side by side.
  const aired = item.aired_count
  const watched = item.watched_count ?? 0
  const remaining = aired ? Math.max(0, aired - watched) : null
  const progressLabel = [item.next_episode, remaining !== null ? `осталось ${remaining}` : null]
    .filter(Boolean)
    .join(' · ')
  const progressPct = aired ? Math.min(100, (watched / aired) * 100) : 0

  return (
    <>
      {/* Fixed full-viewport backdrop — sits behind everything (rows included)
          so it stays visible no matter how far the page scrolls, like the
          int.js Lampa plugin's full-screen background. */}
      <div className={styles.heroBg} aria-hidden>
        {layers.map(l => (
          <img
            key={l.key}
            src={l.src}
            alt=""
            className={[
              styles.bgLayer,
              l.key === activeKey && styles.bgLayerActive,
              l.isPoster && styles.bgPoster,
            ].filter(Boolean).join(' ')}
            style={{ transitionDuration: `${CROSSFADE_MS}ms` }}
          />
        ))}
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
          {!!aired && (
            <div className={styles.episodeProgress}>
              {progressLabel && <span className={styles.episodeProgressLabel}>{progressLabel}</span>}
              <div className={styles.episodeProgressTrack}>
                <div className={styles.episodeProgressFill} style={{ width: `${progressPct}%` }} />
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
