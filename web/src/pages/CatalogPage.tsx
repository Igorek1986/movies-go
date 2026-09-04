import { useEffect, useLayoutEffect, useState, useCallback, useRef } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, scrollH, getGridCols, CAROUSEL_TRANSITION_MS, CARD_WHEEL_COOLDOWN_MS, CATEGORY_WHEEL_COOLDOWN_MS, NAV_H, focusTopNavActive, shouldThrottleKeyRepeat } from '@/utils/scrollNav'
import { takePendingFocusCatalogSearch } from '@/utils/catalogSearchFocus'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { useAuth } from '@/hooks/useAuth'
import { subscribeLiveSync } from '@/hooks/useLiveSync'
import { useHideWatchedFilter, applyHideWatchedParams } from '@/hooks/useHideWatchedFilter'
import { useUnwatchedSort } from '@/hooks/useUnwatchedSort'
import { useMenuOrder } from '@/hooks/useMenuOrder'
import { fetchCatalogCategories, applyMenuOrder, shuffleArray, isCollectionsBlockMember, collapseCollectionsBlock } from '@/utils/catalogCategories'
import { getEffectiveBrowseLayout } from '@/utils/browseLayout'
import { BrowseHero, useHeroPreview, getCachedHeroDetail } from '@/components/BrowseHero'
import styles from './CatalogPage.module.scss'

interface MediaItem {
  id: number
  media_type: string
  title: string
  name?: string
  poster_path: string | null
  backdrop_path?: string | null
  vote_average: number
  release_date: string
  first_air_date: string
  release_quality: string
  certification_ru?: string
  certification_us?: string
  // Already present in the list endpoint's own response (toMediaItem) — read
  // by BrowseHero as an instant fallback so the hero doesn't have to wait on
  // its own separate /api/media-card detail fetch to show them (see its
  // rawBackdrop-adjacent comment there).
  overview?: string
  status?: string
  number_of_seasons?: number
  category_name?: string
  year?: string
  unwatched_count?: number
  watched_count?: number
  aired_count?: number
  next_episode?: string
  // Only set on results from the TMDB search fallback (see loadSearchPage) —
  // the card isn't in our catalog (no torrents), so the badge tells the user
  // this came straight from TMDB rather than something we can actually stream.
  tmdb_only?: boolean
}

interface CatalogResponse {
  total_pages: number
  total_results: number
  results: MediaItem[]
}

interface Category {
  id: string
  name: string
}


// Module-level cache — survives SPA navigation, resets on full page reload.
// stale (see invalidateAllCatalogRows/invalidateUnwatchedRow): the row still
// renders these items immediately on remount — a plain SPA back-nav stays a
// soft, instant return instead of flashing "Загрузка…" — but CategoryRow
// treats itself as not-yet-loaded and quietly refetches once its own
// IntersectionObserver fires, swapping in the corrected list in place.
interface RowCache { items: MediaItem[]; totalPages: number; stale?: boolean }
interface CatViewCache { id: string; items: MediaItem[]; totalPages: number; currentPage: number; scrollY: number; stale?: boolean }
const _cache = {
  // Raw list from fetchCatalogCategories (API + synthetic "unwatched"), before
  // numparser_menu_sort/numparser_menu_hide are applied — kept separately from
  // `categories` below so a settings change can recompute display order
  // without re-fetching or re-shuffling.
  rawCategories: [] as Category[],
  // Genre/actor/director pool shuffled once per fetch (see applyMenuOrder) —
  // re-applying order/hidden on a settings change must reuse this, not
  // reshuffle, or toggling an unrelated category's visibility would jumble
  // every genre row's position along with it.
  shuffledBlock: [] as Category[],
  // Last computed display list (rawCategories + shuffledBlock with order/
  // hidden applied) — what the categories useState initializer below reads
  // synchronously on an SPA remount, before the settings-driven effect has a
  // chance to run.
  categories: [] as Category[],
  rows: {} as Record<string, RowCache>,
  scrollY: 0,
  catView: null as CatViewCache | null,
  // Which category is currently expanded (classic layout's "full grid" view)
  // and the id of the card last clicked out of it — module-level for the
  // same reason as everything else here: expanding a category never pushes
  // its own history entry (only the ?cat= query param embedded in the
  // backUrl passed to the card detail page's own back button does), so a
  // plain browser back button/gesture lands on the bare /catalog URL with no
  // ?cat= at all, and a component-local expandedCategory read only from that
  // URL forgot the expanded view entirely — landing back on the top-level
  // category list instead of the grid you'd drilled into.
  expandedCategory: null as string | null,
  lastFocusCardId: null as string | null,
  // Profile the cached rows/catView belong to — module-level (not a ref) because
  // a profile switch made while CatalogPage is unmounted (e.g. from the admin
  // panel) must still be detected on remount; a component-local ref would reset
  // to "unknown" on every mount and miss changes that happened while away.
  profileKey: null as string | null,
  // Per-row last-focused card index — module-level for the same reason as
  // everything else here: opening a card's own detail page and coming back
  // is a real route change, fully unmounting/remounting CatalogPage, so a
  // component-local ref forgot this and the hero carousel's autoFocusIdx
  // fell back to 0, landing back on the row's first card instead of
  // whichever one had actually been focused.
  lastRowFocusIdx: new Map<string, number>(),
  // Hero carousel: which category/row was active — same reasoning as
  // lastRowFocusIdx above (component state alone forgot this across a
  // detail-page round trip, snapping back to the first row every time).
  activeCategoryIndex: 0,
  // Search box text + debounced query — same detail-page round-trip problem
  // as everything else here: component state alone forgot what was typed,
  // so clicking into a card from search results and coming back landed on
  // the plain Каталог view instead of the search results you'd left.
  searchValue: '' as string,
  searchQuery: '' as string,
  // The search results themselves + where pagination/TMDB "load more" had
  // gotten to — without this, remounting after a detail-page round trip
  // re-fetched from page 1 (see the skip-once-on-mount logic around
  // searchResults' effect below) and the restored scroll position (generic
  // _cache.scrollY) had nothing tall enough to scroll to yet, since the grid
  // was momentarily empty again.
  searchResults: null as MediaItem[] | null,
  searchHasMore: false,
  searchPage: 1,
  searchLocalRows: [] as MediaItem[],
  searchTmdbResults: [] as MediaItem[],
  searchTmdbHasMore: false,
  searchTmdbLimit: 12,
}

export function invalidateCatalogCache() {
  _cache.rawCategories = []
  _cache.shuffledBlock = []
  _cache.categories = []
  _cache.rows = {}
  _cache.catView = null
}

// Called after a status change on the card detail page (Смотрю/Брошено/…) —
// that flips server-side which shows /unwatched returns, but this module's
// cache is what a plain SPA back-nav reads instead of a fresh fetch, so
// without this the row keeps showing whatever it had before the change
// until a hard reload.
export function invalidateUnwatchedRow() {
  const row = _cache.rows['unwatched']
  if (row) row.stale = true
  if (_cache.catView?.id === 'unwatched') _cache.catView.stale = true
}

// Called after a local timecode/status change that might cross the
// hide_watched threshold (see useHideWatchedFilter) — a card can newly need
// hiding (or showing again, if a timecode got reset) in ANY category row, not
// just "Непросмотренные", and there's no client-side index of which cached
// rows a given card_id appears in. Only called from CardDetailPage's own
// local actions (a handful of clicks per session), never from the WS
// listener in useLiveSync — 'timecode' messages arrive there roughly every
// 15s during active playback elsewhere (see np.js's SYNC_THROTTLE_MS), and
// wiping every row on each tick would be wasteful.
//
// Marks stale rather than deleting (see RowCache.stale) — deleting outright
// made every single row (and the expanded grid, via CatViewCache.stale) flash
// "Загрузка…" on the very next SPA back-nav, even though only whichever row
// actually contains this one card needed a recheck.
export function invalidateAllCatalogRows() {
  for (const row of Object.values(_cache.rows)) row.stale = true
  if (_cache.catView) _cache.catView.stale = true
}

function getItemTitle(item: MediaItem): string {
  return item.title || item.name || ''
}

function getItemYear(item: MediaItem): string {
  return (item.release_date || item.first_air_date || '').slice(0, 4)
}

const US_TO_RU: Record<string, string> = {
  'G': '0+', 'TV-G': '0+', 'TV-Y': '0+',
  'PG': '6+', 'TV-Y7': '6+', 'TV-PG': '6+',
  'PG-13': '12+', 'TV-14': '12+',
  'R': '16+',
  'NC-17': '18+', 'TV-MA': '18+',
}

function getCertification(item: MediaItem): string {
  if (item.certification_ru) return item.certification_ru
  if (item.certification_us) return US_TO_RU[item.certification_us] || ''
  return ''
}

interface CardProps {
  item: MediaItem
  onClick: () => void
  onActivate?: () => void
  isHeroActive?: boolean
  // Hero carousel: the focused card's title/year/rating/cert/quality are
  // already shown up in BrowseHero, so repeating them under every poster in
  // the row is pure duplication — Classic layout (no hero panel) keeps them.
  compact?: boolean
}

function MediaCard({ item, onClick, onActivate, isHeroActive, compact }: CardProps) {
  const url = posterUrl(item.poster_path)
  const title = getItemTitle(item)
  const year = getItemYear(item)
  const cert = getCertification(item)
  return (
    <div
      className={`${styles.card}${isHeroActive ? ' ' + styles.cardHeroActive : ''}`}
      onClick={onClick}
      tabIndex={0}
      data-card
      onKeyDown={e => { if (e.key === 'Enter') onClick() }}
      onFocus={onActivate}
    >
      <div className={styles.posterWrap}>
        {url
          ? <img className={styles.poster} src={url} alt={title} loading="lazy" />
          : <div className={styles.posterPlaceholder}>{title || 'Нет постера'}</div>
        }
        {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
        {item.tmdb_only && <span className={styles.tmdbBadge}>TMDB</span>}
        {!!item.unwatched_count && (
          <span className={styles.unwatchedBadge}>{item.unwatched_count}</span>
        )}
        {item.next_episode && (
          <span className={styles.nextEpBadge}>{item.next_episode}</span>
        )}
        {/* Hero mode (compact) already shows this same progress in the hero
            banner itself (BrowseHero's own .episodeProgress, for whichever
            card is currently focused) — repeating it on every thumbnail too
            was redundant there, unlike Classic where the card is the only
            place it's shown at all. */}
        {!compact && !!item.aired_count && (
          <>
            <span className={styles.progressLabel}>{item.watched_count ?? 0}/{item.aired_count}</span>
            <div className={styles.progressTrack}>
              <div
                className={styles.progressFill}
                style={{ width: `${Math.min(100, ((item.watched_count ?? 0) / item.aired_count) * 100)}%` }}
              />
            </div>
          </>
        )}
      </div>
      {!compact && (
      <div className={styles.cardBody}>
        <p className={styles.cardTitle}>{title}</p>
        <div className={styles.cardMeta}>
          <span>{year || ' '}</span>
          {item.vote_average > 0 && <span>★ {item.vote_average.toFixed(1)}</span>}
          {cert && <span className={`${styles.cert} ${styles[`cert_${cert.replace(/[^a-zA-Z0-9]/g, '_')}`] || ''}`}>{cert}</span>}
        </div>
        <div className={styles.cardMeta}>
          <span className={styles.quality}>{item.release_quality || ' '}</span>
        </div>
      </div>
      )}
    </div>
  )
}

interface CategoryRowProps {
  category: Category
  token: string
  profileId: string
  // См. useHideWatchedFilter — та же per-профильная настройка, что и
  // numparser_hide_watched в np.js, шлётся в запрос теми же query-параметрами
  // (applyHideWatchedParams), которые уже понимает бэкенд (applyHideWatched).
  // hideWatchedLoaded гейтит самый первый фетч строки — иначе он мог уйти
  // раньше, чем настройка подтянулась с сервера (default false), и просмотренное
  // проскочило бы в уже отрисованный список до следующей полной перезагрузки.
  hideWatched: boolean
  hidePercent: number
  hideWatchedLoaded: boolean
  // См. useUnwatchedSort — та же per-профильная настройка, что
  // np_unwatched_sort_order в np_unwatched.js (Lampa). Применяется только к
  // категории "unwatched" (шлётся как ?sort=), другие категории её игнорируют.
  unwatchedSort: string
  unwatchedSortLoaded: boolean
  onExpandCategory: (id: string, focusAfterIdx?: number) => void
  onCardClick: (item: MediaItem) => void
  onActivate?: (item: MediaItem) => void
  activeCardId?: string | null
  // Omitted entirely in the hero-carousel view — only one row is ever visible
  // there, so there's nothing to drag onto. Still used in Classic layout.
  dragHandlers?: {
    onDragStart: (e: React.DragEvent, id: string) => void
    onDragEnd: () => void
    onDragOver: (e: React.DragEvent, id: string) => void
    onDrop: (e: React.DragEvent) => void
  }
  initialCache?: RowCache
  onItemsLoaded: (id: string, cache: RowCache) => void
  // Carousel mode: this category turned out to have 0 items — the parent
  // advances activeCategoryIndex instead of leaving an empty screen (in
  // Classic layout the row just quietly disappears from the scrollable list).
  onEmpty?: () => void
  // Carousel mode: focus this card index once items are available (whatever
  // was last focused in this category, or 0) — Classic layout never
  // auto-focuses on mount, so this stays undefined there.
  autoFocusIdx?: number
  // Carousel mode: the parent renders its own persistent title instead (see
  // CatalogPage's carouselActive branch) — otherwise, switching to a category
  // that turns out empty briefly showed its title before this component
  // returned null and the parent skipped to the next one, which read as the
  // title flickering in and out on every auto-skip.
  hideHeader?: boolean
}

function CategoryRow({ category, token, profileId, hideWatched, hidePercent, hideWatchedLoaded, unwatchedSort, unwatchedSortLoaded, onExpandCategory, onCardClick, onActivate, activeCardId, dragHandlers, initialCache, onItemsLoaded, onEmpty, autoFocusIdx, hideHeader }: CategoryRowProps) {
  const [items, setItems] = useState<MediaItem[] | null>(initialCache?.items ?? null)
  const [totalPages, setTotalPages] = useState(initialCache?.totalPages ?? 1)
  const [error, setError] = useState(false)
  const rowRef = useRef<HTMLElement>(null)
  const rowInnerRef = useRef<HTMLDivElement>(null)
  const rowScrollRef = useRef<HTMLDivElement>(null)
  // stale (see RowCache.stale): render the old cached items immediately (no
  // "Загрузка…" flash on SPA back-nav) but still treat this as not-loaded, so
  // the IntersectionObserver below fires a real, quiet refetch that swaps in
  // the corrected list once it resolves.
  const loadedRef = useRef(!!initialCache && !initialCache.stale)
  const autoFocusAppliedRef = useRef(false)
  // See shouldThrottleKeyRepeat's comment — throttles held-ArrowLeft/Right
  // auto-repeat so scrollH's smooth-scroll isn't retargeted mid-flight.
  const arrowRepeatRef = useRef(0)
  // Свежий items для обработчика WS-события ниже, не завязываясь на него как
  // на зависимость эффекта (иначе подписка пересоздавалась бы на каждую
  // загрузку строки).
  const itemsRef = useRef(items)
  itemsRef.current = items

  // Shared by the ArrowLeft/Right keydown handler below and the wheel
  // handler further down — moves focus to the next/previous card exactly
  // like pressing the arrow key would, rather than just nudging scrollLeft
  // (a mouse-wheel move over the cards should feel identical to using the
  // keyboard, hero preview activation included).
  function moveCardFocus(dir: 1 | -1) {
    const cards = Array.from(rowInnerRef.current?.querySelectorAll<HTMLElement>('[data-card]') ?? [])
    const idx = cards.indexOf(document.activeElement as HTMLElement)
    if (idx === -1) return
    if (dir === 1) {
      if (idx === cards.length - 1) {
        // See the keydown handler's identical comment — ArrowRight/wheel
        // forward past the last real card opens the category directly.
        if (totalPages > 1) onExpandCategory(category.id, items?.length ?? 0)
        return
      }
      const next = cards[idx + 1]
      next?.focus({ preventScroll: true })
      if (next) scrollH(next)
    } else {
      const prev = cards[idx - 1]
      prev?.focus({ preventScroll: true })
      if (prev) scrollH(prev)
    }
  }
  // moveCardFocus closes over items/totalPages, which change on every load —
  // a ref keeps the wheel effect below from needing to re-attach its
  // listener (and re-run its own setup/teardown) on every one of those.
  const moveCardFocusRef = useRef(moveCardFocus)
  moveCardFocusRef.current = moveCardFocus

  // Hero carousel only (hideHeader): the page itself is scroll-locked (see
  // carouselActive/.pageLocked), so a wheel over the card rail moves focus
  // between cards instead — same effect as ArrowLeft/ArrowRight. Always
  // preventDefault while hideHeader, even when there's nothing left to
  // move to — otherwise a wheel event over, say, a 3-card row with nothing
  // to focus past falls through to the browser's default scroll, which
  // .pageLocked's own overflow:hidden doesn't actually stop (it clips this
  // element, not the outer page). Not done in Classic layout, where the row
  // is a normal in-page-flow element and a vertical wheel over it should
  // keep scrolling the page like anywhere else. Plain addEventListener (not
  // JSX onWheel) so preventDefault actually works — React attaches onWheel
  // as a passive listener.
  useEffect(() => {
    if (!hideHeader) return
    const el = rowScrollRef.current
    if (!el) return
    let cooling = false
    function onWheel(e: WheelEvent) {
      e.preventDefault()
      if (cooling) return
      cooling = true
      window.setTimeout(() => { cooling = false }, CARD_WHEEL_COOLDOWN_MS)
      moveCardFocusRef.current(e.deltaY > 0 ? 1 : -1)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [hideHeader])

  // Shared by every path that can silently drop the currently-focused card
  // out of `items` (initial/background refetch below, the live WS 'status'
  // removal further down) — if that card had real DOM focus, removing it
  // without moving focus first sends it to <body>, which freezes the hero
  // banner/background on the now-gone card until the user manually moves
  // focus themselves.
  function focusNeighborForRemoved(removedIds: Set<string>) {
    const activeEl = document.activeElement as HTMLElement | null
    if (!activeEl) return
    const cardWrapper = activeEl.closest<HTMLElement>('[id]')
    if (!cardWrapper || !removedIds.has(cardWrapper.id)) return
    const siblings = Array.from(rowInnerRef.current?.querySelectorAll<HTMLElement>('[data-card]') ?? [])
    const activeIdx = siblings.indexOf(activeEl)
    if (activeIdx === -1) return
    const neighbor = activeIdx > 0 ? siblings[activeIdx - 1] : siblings[activeIdx + 1]
    neighbor?.focus({ preventScroll: true })
  }

  const loadItems = useCallback(async () => {
    if (loadedRef.current) return
    // "Непросмотренные" is per-profile — fetching it before the active
    // profile resolves (token still '') has no way to compute anything and
    // comes back with an empty result, which the onEmpty effect below reads
    // as "this category is genuinely empty" and silently skips the hero
    // carousel past it — landing on the wrong row on every fresh page load,
    // even though the row has real items once a profile is known. Wait for
    // a real token instead of guessing wrong; this row remounts anyway (its
    // key includes token/profileId) once the profile resolves, so this
    // just lets that remount make the one real fetch instead of wasting an
    // empty one first. Every other category doesn't need a profile at all,
    // so they're deliberately not gated here.
    if (category.id === 'unwatched' && !token) return
    if (!hideWatchedLoaded) return
    if (category.id === 'unwatched' && !unwatchedSortLoaded) return
    loadedRef.current = true
    try {
      const params = new URLSearchParams({ per_page: '20', page: '1' })
      if (token && profileId != null) {
        params.set('token', token)
        params.set('profile_id', profileId)
        applyHideWatchedParams(params, hideWatched, hidePercent)
      }
      if (category.id === 'unwatched') params.set('sort', unwatchedSort)
      const res = await fetch(`/${encodeURIComponent(category.id)}?${params}`)
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const data: CatalogResponse = await res.json()
      const results = data.results || []
      const tp = data.total_pages || 1
      const prevItems = itemsRef.current
      if (prevItems?.length) {
        const nextIds = new Set(results.map(it => `${it.id}_${it.media_type}`))
        const removedIds = new Set(prevItems.map(it => `${it.id}_${it.media_type}`).filter(id => !nextIds.has(id)))
        if (removedIds.size) focusNeighborForRemoved(removedIds)
      }
      setTotalPages(tp)
      setItems(results)
      onItemsLoaded(category.id, { items: results, totalPages: tp })
    } catch {
      setError(true)
    }
  }, [category.id, token, profileId, hideWatched, hidePercent, hideWatchedLoaded, unwatchedSort, unwatchedSortLoaded, onItemsLoaded])

  // Carousel mode: this category has nothing to show — tell the parent to
  // advance instead of leaving a blank screen (Classic layout doesn't pass
  // onEmpty; it just relies on the render-time `return null` below).
  useEffect(() => {
    if (items !== null && items.length === 0) onEmpty?.()
  }, [items, onEmpty])

  // Живое добавление/удаление в уже отрисованной строке «Непросмотренные» по
  // WS-статусу — источник не важен: Lampa, эта же вкладка или другая вкладка
  // веба, все идут через один и тот же канал. Зеркало insertCardIntoLine/
  // removeCardEverywhere в np_unwatched.js, через React state вместо DOM.
  useEffect(() => {
    if (category.id !== 'unwatched') return
    return subscribeLiveSync((msg) => {
      // Пересечение aired_cutoff на бэкенде: новый эпизод "появился" по дате
      // выхода, без предшествующего 'status'/'timecode' конкретной карточки —
      // добавить её адресно (как ниже) нечем, перезапрашиваем всю строку тем
      // же запросом, что и первичная загрузка (loadItems выше).
      if (msg.type === 'unwatched_stale') {
        if (!token || profileId == null) return
        const params = new URLSearchParams({ per_page: '20', page: '1', token, profile_id: profileId, sort: unwatchedSort })
        applyHideWatchedParams(params, hideWatched, hidePercent)
        fetch(`/unwatched?${params}`)
          .then(r => r.ok ? r.json() : null)
          .then((data: CatalogResponse | null) => {
            if (!data) return
            const results = data.results || []
            const tp = data.total_pages || 1
            const prevItems = itemsRef.current
            if (prevItems?.length) {
              const nextIds = new Set(results.map(it => `${it.id}_${it.media_type}`))
              const removedIds = new Set(prevItems.map(it => `${it.id}_${it.media_type}`).filter(id => !nextIds.has(id)))
              if (removedIds.size) focusNeighborForRemoved(removedIds)
            }
            setTotalPages(tp)
            setItems(results)
            onItemsLoaded(category.id, { items: results, totalPages: tp })
          })
          .catch(() => {})
        return
      }

      if (msg.type !== 'status' || !msg.card_id) return
      const current = itemsRef.current
      if (!current) return
      const idx = current.findIndex(item => `${item.id}_${item.media_type}` === msg.card_id)

      if (msg.status === 'watching') {
        if (idx !== -1) return // уже в списке
        // WS не несёт данные карточки (постер/название) — дотягиваем сами с
        // нашего же бэкенда (тот же эндпоинт, что открытие карточки на вебе).
        fetch(`/api/media-card/${encodeURIComponent(msg.card_id)}`)
          .then(r => r.ok ? r.json() : null)
          .then((data: { tmdb_id?: number; media_type?: string; title?: string; poster_path?: string | null; backdrop_path?: string | null; vote_average?: number; release_date?: string; first_air_date?: string; certification_ru?: string } | null) => {
            if (!data?.tmdb_id || !data.media_type) return
            const latest = itemsRef.current
            if (!latest || latest.some(item => `${item.id}_${item.media_type}` === msg.card_id)) return
            const item: MediaItem = {
              id: data.tmdb_id,
              media_type: data.media_type,
              title: data.title || '',
              name: data.media_type === 'tv' ? data.title : undefined,
              poster_path: data.poster_path ?? null,
              backdrop_path: data.backdrop_path,
              vote_average: data.vote_average ?? 0,
              release_date: data.release_date || '',
              first_air_date: data.first_air_date || '',
              release_quality: '',
              certification_ru: data.certification_ru,
            }
            const next = [item, ...latest]
            setItems(next)
            onItemsLoaded(category.id, { items: next, totalPages })
          })
          .catch(() => {})
        return
      }

      if (idx === -1) return
      // Если удаляемая карточка сейчас в фокусе — переносим фокус на соседнюю
      // ДО того, как React уберёт её из DOM (см. focusNeighborForRemoved).
      // Иначе фокус улетает на body, и герой-фон/подсветка карточки замирают
      // на уже удалённой карточке — тот же класс бага, что чинили в
      // np_unwatched.js для фокуса в Lampa, только здесь речь о нативном
      // DOM-фокусе браузера.
      focusNeighborForRemoved(new Set([msg.card_id]))

      const next = current.filter((_, i) => i !== idx)
      setItems(next)
      onItemsLoaded(category.id, { items: next, totalPages })
    })
  }, [category.id, totalPages, onItemsLoaded, token, profileId, hideWatched, hidePercent, unwatchedSort])

  useEffect(() => {
    if (autoFocusIdx === undefined || autoFocusAppliedRef.current || !items?.length) return
    autoFocusAppliedRef.current = true
    const idx = Math.min(autoFocusIdx, items.length - 1)
    // Activate the hero banner directly here, not only through the card's
    // own onFocus — a mouse click landing on a nav link keeps real DOM focus
    // there (or some other interaction beats our focus() call to the punch),
    // and when that happens onFocus never fires, so hero.item stayed null
    // and BrowseHero rendered nothing at all. This guarantees the banner
    // populates regardless of whether the focus() below actually lands.
    onActivate?.(items[idx])
    requestAnimationFrame(() => {
      // Don't steal focus from an active text input — this fires on every
      // fresh mount of the row, including the moment the header search icon
      // just opened the floating search bar (still mounted underneath until
      // a query actually narrows the view): without this check, the row's
      // own focus() below beat the search input's for it, which then read
      // as "the bar closed itself" once its onBlur saw focus leave for a
      // card the instant it opened.
      const activeTag = document.activeElement?.tagName?.toLowerCase()
      if (activeTag === 'input' || activeTag === 'textarea') return
      const cards = rowInnerRef.current?.querySelectorAll<HTMLElement>('[data-card]')
      if (!cards?.length) return
      const target = cards[Math.min(idx, cards.length - 1)]
      target?.focus({ preventScroll: true })
      // Restoring focus deep into the row (e.g. coming back from an expanded
      // category via Backspace, where autoFocusIdx can be far past what's
      // scrolled into view) needs the row scrolled to it too — Classic
      // layout's pendingFocus effect already does this; this is hero
      // carousel's equivalent path. Instant, not animated — this is the row
      // snapping to where it should already be on arrival, not a move the
      // user should watch happen.
      if (target) scrollH(target, true)
    })
  }, [items, autoFocusIdx, onActivate])

  useEffect(() => {
    if (items === null) return
    const el = rowInnerRef.current
    if (!el?.dataset.pendingFocus) return
    const savedIdx = parseInt(el.dataset.pendingFocus, 10) || 0
    delete el.dataset.pendingFocus
    requestAnimationFrame(() => {
      const cards = el.querySelectorAll<HTMLElement>('[data-card]')
      const target = cards[Math.min(savedIdx, cards.length - 1)]
      // preventScroll: focusing an off-screen element natively jumps it into
      // view instantly, before our own smooth scrollH/scrollV below even
      // runs — without this, every keyboard move looked like an abrupt jump
      // immediately followed by a smooth correction, not one smooth move.
      target?.focus({ preventScroll: true })
      // Classic layout only (carousel mode never sets pendingFocus — it has
      // its own autoFocusIdx path and nothing to scroll into view anyway).
      if (target) { scrollH(target); scrollV(target) }
    })
  }, [items])

  useEffect(() => {
    const el = rowRef.current
    if (!el) return
    const observer = new IntersectionObserver(
      entries => {
        if (entries[0].isIntersecting) {
          observer.disconnect()
          loadItems()
        }
      },
      { rootMargin: '300px' }
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [loadItems])

  if (items !== null && items.length === 0) return null

  const hasMore = totalPages > 1

  return (
    <section
      ref={rowRef}
      className={styles.row}
      data-cat-id={category.id}
      draggable={!!dragHandlers}
      onDragStart={dragHandlers ? e => dragHandlers.onDragStart(e, category.id) : undefined}
      onDragEnd={dragHandlers?.onDragEnd}
      onDragOver={dragHandlers ? e => dragHandlers.onDragOver(e, category.id) : undefined}
      onDrop={dragHandlers?.onDrop}
    >
      {!hideHeader && (
      <div className={styles.rowHeader}>
        <div className={styles.rowHeaderLeft}>
          {dragHandlers && <span className={styles.dragHandle} title="Перетащить">⠿</span>}
          <h3 className={styles.rowTitle}>{category.name}</h3>
        </div>
        {hasMore && (
          <button className={styles.rowMore} onClick={() => onExpandCategory(category.id)}>
            Все →
          </button>
        )}
      </div>
      )}
      <div ref={rowScrollRef} className={`${styles.rowScroll}${hideHeader ? ' ' + styles.rowScrollCompact : ''}`} data-row-scroll>
        <div
          ref={rowInnerRef}
          className={styles.rowInner}
          data-row-id={category.id}
          onKeyDown={e => {
            if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
            e.preventDefault()
            if (shouldThrottleKeyRepeat(e, arrowRepeatRef)) return
            moveCardFocus(e.key === 'ArrowRight' ? 1 : -1)
          }}
        >
          {items === null && !error && (
            <div className={styles.rowLoading}>Загрузка...</div>
          )}
          {error && <div className={styles.rowLoading}>Ошибка загрузки</div>}
          {items !== null && items.length === 0 && (
            <div className={styles.rowLoading}>Нет данных</div>
          )}
          {items !== null && items.map(item => {
            const cardId = `${item.id}_${item.media_type}`
            return (
              <div key={cardId} className={styles.rowCard} id={cardId}>
                <MediaCard
                  item={item}
                  onClick={() => onCardClick(item)}
                  onActivate={onActivate ? () => onActivate(item) : undefined}
                  isHeroActive={activeCardId === cardId}
                  compact={hideHeader}
                />
              </div>
            )
          })}
          {items !== null && hasMore && (
            <div className={styles.rowCard} key="expand-btn">
              <button
                className={styles.rowExpandBtn}
                onClick={() => onExpandCategory(category.id, items?.length ?? 0)}
                tabIndex={-1}
              >
                Все →
              </button>
            </div>
          )}
        </div>
      </div>
    </section>
  )
}

interface CategoryViewProps {
  category: Category
  token: string
  profileId: string
  hideWatched: boolean
  hidePercent: number
  hideWatchedLoaded: boolean
  unwatchedSort: string
  unwatchedSortLoaded: boolean
  onBack: () => void
  onCardClick: (item: MediaItem) => void
  focusAfterIdx?: number
}

function CategoryView({ category, token, profileId, hideWatched, hidePercent, hideWatchedLoaded, unwatchedSort, unwatchedSortLoaded, onBack, onCardClick, focusAfterIdx }: CategoryViewProps) {
  const cached = _cache.catView?.id === category.id ? _cache.catView : null

  const [items, setItemsRaw] = useState<MediaItem[]>(cached?.items ?? [])
  const pageRef = useRef(cached?.currentPage ?? 1)
  const [totalPages, setTotalPages] = useState(cached?.totalPages ?? 1)
  const totalPagesRef = useRef(cached?.totalPages ?? 1)
  const [loading, setLoading] = useState(false)
  const [searchValue, setSearchValue] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [empty, setEmpty] = useState(false)
  const [searchFloating, setSearchFloating] = useState(false)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const catSearchRef = useRef<HTMLDivElement>(null)
  const loadingRef = useRef(false)
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const focusAppliedRef = useRef(false)
  // stale (see CatViewCache.stale): same idea as CategoryRow's loadedRef —
  // render the cached grid immediately, but still trigger one quiet refetch.
  const loadedRef = useRef(!!cached && !cached.stale)
  const prevSearchRef = useRef('')
  const prevTokenRef = useRef(token)
  const prevProfileRef = useRef(profileId)
  const prevHideWatchedRef = useRef(hideWatched)
  const prevHidePercentRef = useRef(hidePercent)
  const prevUnwatchedSortRef = useRef(unwatchedSort)

  // Init cache entry on mount, save scroll position continuously
  useEffect(() => {
    if (!_cache.catView || _cache.catView.id !== category.id) {
      _cache.catView = { id: category.id, items: [], totalPages: 1, currentPage: 1, scrollY: 0 }
    }
    const save = () => { if (_cache.catView) _cache.catView.scrollY = Math.round(window.scrollY) }
    window.addEventListener('scroll', save, { passive: true })
    return () => window.removeEventListener('scroll', save)
  }, [category.id])

  // Show floating search bar when the search field scrolls above the nav
  useEffect(() => {
    function check() {
      const el = catSearchRef.current
      if (!el) return
      setSearchFloating(el.getBoundingClientRect().bottom < NAV_H)
    }
    window.addEventListener('scroll', check, { passive: true })
    return () => window.removeEventListener('scroll', check)
  }, [])

  // Restore scroll — and keyboard focus, on the exact card last clicked out
  // of this grid (see _cache.lastFocusCardId's comment) — when returning
  // with cached items. preventScroll: true since scrollY above (or the
  // browser's own scroll restoration) already puts the right area on
  // screen; without it, focus() re-scrolling to "nearest" could fight that.
  useLayoutEffect(() => {
    if (cached && cached.scrollY > 0) window.scrollTo({ top: cached.scrollY, behavior: 'instant' })
    if (cached && _cache.lastFocusCardId) {
      document.getElementById(_cache.lastFocusCardId)
        ?.querySelector<HTMLElement>('[data-card]')
        ?.focus({ preventScroll: true })
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const loadPage = useCallback(async (pg: number, sq: string, reset: boolean) => {
    if (loadingRef.current) return
    loadingRef.current = true
    setLoading(true)
    try {
      const params = new URLSearchParams({ per_page: '20', page: String(pg) })
      if (sq) params.set('search', sq)
      if (token && profileId != null) {
        params.set('token', token)
        params.set('profile_id', profileId)
        applyHideWatchedParams(params, hideWatched, hidePercent)
      }
      if (category.id === 'unwatched') params.set('sort', unwatchedSort)
      const res = await fetch(`/${encodeURIComponent(category.id)}?${params}`)
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const data: CatalogResponse = await res.json()
      const results = data.results || []
      const tp = data.total_pages || 1
      totalPagesRef.current = tp
      setTotalPages(tp)
      if (_cache.catView?.id === category.id) {
        _cache.catView.totalPages = tp
        _cache.catView.currentPage = pg
      }
      setItemsRaw(prev => {
        const next = reset ? results : [...prev, ...results]
        if (_cache.catView?.id === category.id) {
          _cache.catView.items = next
          _cache.catView.stale = false
        }
        return next
      })
      if (pg === 1 && results.length === 0) setEmpty(true)
      else setEmpty(false)
    } catch {
    } finally {
      loadingRef.current = false
      setLoading(false)
    }
  }, [category.id, token, profileId, hideWatched, hidePercent, unwatchedSort])

  useEffect(() => {
    if (!hideWatchedLoaded) return
    if (category.id === 'unwatched' && !unwatchedSortLoaded) return
    const searchChanged = searchQuery !== prevSearchRef.current
    const tokenChanged = token !== prevTokenRef.current
    const profileChanged = profileId !== prevProfileRef.current
    const hideWatchedChanged = hideWatched !== prevHideWatchedRef.current
    const hidePercentChanged = hidePercent !== prevHidePercentRef.current
    const sortChanged = category.id === 'unwatched' && unwatchedSort !== prevUnwatchedSortRef.current
    prevSearchRef.current = searchQuery
    prevTokenRef.current = token
    prevProfileRef.current = profileId
    prevHideWatchedRef.current = hideWatched
    prevHidePercentRef.current = hidePercent
    prevUnwatchedSortRef.current = unwatchedSort
    if (!searchChanged && !tokenChanged && !profileChanged && !hideWatchedChanged && !hidePercentChanged && !sortChanged && loadedRef.current) return
    if (tokenChanged || profileChanged || hideWatchedChanged || hidePercentChanged || sortChanged) {
      setItemsRaw([])
      setEmpty(false)
      if (_cache.catView?.id === category.id) {
        _cache.catView.items = []
        _cache.catView.currentPage = 1
        _cache.catView.scrollY = 0
      }
    }
    loadedRef.current = true
    pageRef.current = 1
    loadPage(1, searchQuery, true)
  }, [searchQuery, loadPage, hideWatched, hidePercent, hideWatchedLoaded, unwatchedSort, unwatchedSortLoaded, category.id]) // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-focus after N-th card loads (opened via keyboard ArrowRight on last row card)
  useEffect(() => {
    if (focusAfterIdx === undefined || focusAppliedRef.current) return
    if (items.length > focusAfterIdx) {
      focusAppliedRef.current = true
      requestAnimationFrame(() => {
        const cards = document.querySelectorAll<HTMLElement>('[data-card]')
        const target = cards[focusAfterIdx]
        target?.focus({ preventScroll: true })
        if (target) scrollV(target)
      })
    } else if (!loadingRef.current && pageRef.current < totalPagesRef.current) {
      const next = pageRef.current + 1
      pageRef.current = next
      loadPage(next, searchQuery, false)
    }
  }, [items, focusAfterIdx, searchQuery, loadPage])

  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver(
      entries => {
        if (entries[0].isIntersecting && !loadingRef.current) {
          if (pageRef.current < totalPages) {
            const next = pageRef.current + 1
            pageRef.current = next
            loadPage(next, searchQuery, false)
          }
        }
      },
      { rootMargin: '300px' }
    )
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [totalPages, searchQuery, loadPage])

  function handleSearchChange(value: string) {
    setSearchValue(value)
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
    searchTimerRef.current = setTimeout(() => {
      setSearchQuery(value.length >= 3 ? value.trim() : '')
    }, 400)
  }

  return (
    <div className={styles.categoryView}>
      <button className={styles.backBtn} onClick={onBack}>← Назад</button>
      <div className={styles.categoryHeader}>
        <h2 className={styles.categoryTitle}>{category.name}</h2>
        <div ref={catSearchRef} className={styles.searchWrap}>
          <input
            className={styles.searchInput}
            placeholder="Поиск…"
            value={searchValue}
            onChange={e => handleSearchChange(e.target.value)}
          />
          {searchValue && (
            <button className={styles.searchClear} onClick={() => handleSearchChange('')} title="Очистить">✕</button>
          )}
        </div>
      </div>
      {loading && items.length === 0 && <div className={styles.loading}>Загрузка…</div>}
      {!loading && empty && <div className={styles.empty}>Нет данных</div>}
      {items.length > 0 && (
        <div className={styles.grid}>
          {items.map(item => {
            const cardId = `${item.id}_${item.media_type}`
            return (
              <div key={cardId} id={cardId}>
                <MediaCard item={item} onClick={() => onCardClick(item)} />
              </div>
            )
          })}
        </div>
      )}
      {loading && items.length > 0 && <div className={styles.loading}>Загрузка…</div>}
      <div ref={sentinelRef} className={styles.sentinel} />
      {searchFloating && (
        <div className={styles.floatingBar}>
          <div className={styles.floatingBarInner}>
            <span className={styles.floatingIcon}>🔍</span>
            <div className={styles.searchWrap} style={{flex: 1}}>
              <input
                className={styles.floatingInput}
                placeholder="Поиск…"
                value={searchValue}
                onChange={e => handleSearchChange(e.target.value)}
              />
              {searchValue && (
                <button className={styles.searchClear} onClick={() => handleSearchChange('')} title="Очистить">✕</button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default function CatalogPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const [categories, setCategories] = useState<Category[]>(() => _cache.categories)
  const [expandedCategory, setExpandedCategory] = useState<string | null>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.get('cat') ?? _cache.expandedCategory
  })
  useEffect(() => { _cache.expandedCategory = expandedCategory }, [expandedCategory])
  const [expandedFocusIdx, setExpandedFocusIdx] = useState<number | undefined>(undefined)
  const [searchValue, setSearchValue] = useState(() => _cache.searchValue)
  const [searchQuery, setSearchQuery] = useState(() => _cache.searchQuery)
  useEffect(() => { _cache.searchValue = searchValue }, [searchValue])
  useEffect(() => { _cache.searchQuery = searchQuery }, [searchQuery])
  const [searchResults, setSearchResults] = useState<MediaItem[] | null>(() => _cache.searchResults)
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchHasMore, setSearchHasMore] = useState(() => _cache.searchHasMore)
  const searchSentinelRef = useRef<HTMLDivElement>(null)
  const searchPageRef = useRef(_cache.searchPage)
  // Guards the TMDB fallback fetch (see loadSearchPage) against a slow response
  // for an already-superseded query landing after the user has typed further.
  const searchQueryRef = useRef('')
  // TMDB fallback results — kept separate from searchResults (not merged in)
  // so "Показать ещё из TMDB" can replace just this slice with a bigger one
  // instead of finding/splicing it out of a combined array.
  const [tmdbResults, setTmdbResults] = useState<MediaItem[]>(() => _cache.searchTmdbResults)
  const [tmdbHasMore, setTmdbHasMore] = useState(() => _cache.searchTmdbHasMore)
  const [tmdbLoadingMore, setTmdbLoadingMore] = useState(false)
  const TMDB_PAGE_SIZE = 12
  const tmdbLimitRef = useRef(_cache.searchTmdbLimit)
  // The local /api/search page-1 rows for the current query, kept around so
  // "Показать ещё из TMDB" can re-dedupe against them without a network call.
  const localSearchRowsRef = useRef<MediaItem[]>(_cache.searchLocalRows)
  // First effect run after mount only: if we just restored a non-empty result
  // set for this exact query from cache (see the two useState initializers
  // above), don't let the searchQuery effect immediately refetch page 1 and
  // stomp over it — that's exactly the "scrolled through many cards, opened
  // one, came back at the top again" bug this cache exists to prevent.
  const skipInitialSearchFetchRef = useRef(_cache.searchQuery !== '' && _cache.searchResults !== null)
  useEffect(() => { _cache.searchResults = searchResults }, [searchResults])
  useEffect(() => { _cache.searchHasMore = searchHasMore }, [searchHasMore])
  useEffect(() => { _cache.searchTmdbResults = tmdbResults }, [tmdbResults])
  useEffect(() => { _cache.searchTmdbHasMore = tmdbHasMore }, [tmdbHasMore])

  const { activeDevice, activeProfile } = useActiveProfile()
  const { user } = useAuth()
  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''
  const { hideWatched, minProgress: hidePercent, hideWatchedLoaded } = useHideWatchedFilter(profileId)
  const { unwatchedSort, unwatchedSortLoaded } = useUnwatchedSort(profileId)
  const { order: menuOrder, setOrder: setMenuOrder, hidden: menuHidden, orderLoaded, hiddenLoaded } = useMenuOrder(profileId)

  // Per-account (server) — see BrowseLayoutSettings on /profiles. Read fresh
  // from `user` on every mount, same convention as CardDetailPage's
  // cardLayout. Forced to Classic on touch devices regardless of the saved
  // preference — the hero carousel is driven by keyboard/mouse focus, which
  // touch has no equivalent for.
  const layout = getEffectiveBrowseLayout(user?.browse_layout)
  const hero = useHeroPreview<MediaItem>('catalog')
  // Also marks the grid as focused here, not just via the onFocusIn listener
  // below — see MediaLibraryPage's identical handleActivate for why: the
  // row's own mount-time auto-focus effect calls this directly so the hero
  // banner populates even when the real .focus() call it also makes doesn't
  // stick, and without this the card's .cardHeroActive border would still
  // never show even though the banner did.
  const handleActivate = useCallback((item: MediaItem) => {
    hero.activate(item)
    setCardGridFocused(true)
  }, [hero.activate])

  // Hero layout: non-scrolling carousel (Lampa TV-style) — exactly one
  // category's row is mounted at a time, always pinned to the bottom via CSS
  // (see .carouselRail), never revealed by scrolling. ArrowUp/Down (or a
  // swipe) just swaps which category that is; the row itself doesn't need to
  // be scrolled into place at all, so there's no scroll-position bookkeeping
  // here — CategoryRow's own onFocus→onActivate wiring drives the hero from
  // whichever card ends up focused, same as any other navigation.
  // Initial value (and kept in sync below) from _cache — same reasoning as
  // _cache.lastRowFocusIdx: opening a card's own detail page and coming back
  // is a real route change that fully remounts CatalogPage, so plain
  // component state here forgot which category/row you'd switched to and
  // always landed back on the first one.
  const [activeCategoryIndex, setActiveCategoryIndex] = useState(() => _cache.activeCategoryIndex)
  useEffect(() => { _cache.activeCategoryIndex = activeCategoryIndex }, [activeCategoryIndex])
  // Категории теперь могут живо сжаться (скрыли строку в Порядок и
  // видимость категорий, см. useMenuOrder) — без этого activeCategoryIndex
  // мог указывать за пределы нового массива, и Hero-карусель просто гасла.
  useEffect(() => {
    if (categories.length > 0 && activeCategoryIndex >= categories.length) {
      setActiveCategoryIndex(categories.length - 1)
    }
  }, [categories, activeCategoryIndex])
  // Drum-carousel row switch — both the outgoing (prevIndex) and incoming
  // (activeCategoryIndex) rows render at once, sliding together the same
  // direction (see .carouselViewport/CAROUSEL_TRANSITION_MS), for as long as
  // this is non-null; then the outgoing one is dropped.
  const [transition, setTransition] = useState<{ prevIndex: number; dir: 1 | -1 } | null>(null)
  const transitionTimerRef = useRef<number | null>(null)
  const carouselPageRef = useRef<HTMLDivElement>(null)
  // See shouldThrottleKeyRepeat's comment — throttles held-arrow auto-repeat
  // in the expanded category/search grid so scrollV isn't retargeted mid-flight.
  const gridArrowRepeatRef = useRef(0)
  useEffect(() => () => { if (transitionTimerRef.current) window.clearTimeout(transitionTimerRef.current) }, [])
  const handleEmptyCategory = useCallback(() => {
    // This category has nothing to show — silently skip to the next one
    // instead of leaving a blank screen; no carousel animation for an
    // automatic correction like this (only real navigation animates).
    if (transitionTimerRef.current) { window.clearTimeout(transitionTimerRef.current); transitionTimerRef.current = null }
    setTransition(null)
    setActiveCategoryIndex(idx => Math.min(idx + 1, categories.length - 1))
  }, [categories.length])

  // Search lives entirely in the floating bar now (see .floatingBar below) —
  // no permanently-visible input in the page toolbar. That toolbar sits in
  // normal page flow, which the hero carousel's fixed full-viewport backdrop
  // (see BrowseHero's .heroBg) paints over — an always-open field there was
  // invisible in hero layout. The floating bar is position:fixed with a
  // z-index (100) well above .heroBg's (0), so it's never hidden regardless
  // of layout; the header's search icon (Layout.tsx) opens it from anywhere.
  const [searchOpen, setSearchOpen] = useState(false)
  const floatingInputRef = useRef<HTMLInputElement>(null)
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const savedScrollRef = useRef(0)

  // Layout.tsx's nav doesn't know about this page's local search state —
  // publish it as a body class instead of threading a prop/context through,
  // same idea as --app-footer-h. Layout.module.scss uses it to mute the
  // current page's nav-link highlight and color the search icon instead, so
  // the header doesn't show two different "active" things at once while
  // browsing search results. Tied to results actually showing (matches
  // showSearch below), not to searchOpen/the floating bar's own visibility —
  // that closes the instant focus leaves it for a result card (see its
  // onBlur), which is exactly when you start browsing results, so the
  // indicator would vanish right as it became most useful.
  useEffect(() => {
    document.body.classList.toggle('search-mode-active', searchQuery.length >= 3 && !expandedCategory)
    return () => { document.body.classList.remove('search-mode-active') }
  }, [searchQuery, expandedCategory])

  // Save scroll position to cache continuously.
  useEffect(() => {
    const save = () => { _cache.scrollY = Math.round(window.scrollY) }
    window.addEventListener('scroll', save, { passive: true })
    return () => window.removeEventListener('scroll', save)
  }, [])

  // Restore scroll on mount.
  // If a category is expanded (from the ?cat= URL param, or from
  // _cache.expandedCategory — see its comment) — CategoryView owns restoring
  // its own scroll/focus and this effect must stay out of the way entirely.
  // Otherwise fall back to hash-based scroll for the first visit.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const catParam = params.get('cat')
    const hash = window.location.hash.slice(1)
    // expandedCategory (component state) is what actually decides whether
    // CategoryView renders — catParam alone missed the cache-restored case
    // (browser back landing on a bare /catalog with no ?cat=), letting this
    // effect's own window.scrollTo(_cache.scrollY) below fire right after
    // CategoryView had already correctly restored its own scroll, snapping
    // the page to the top-level view's unrelated scroll position instead.
    const resolvedCat = catParam || expandedCategory

    if (resolvedCat) {
      if (catParam) window.history.replaceState(null, '', window.location.pathname)
      // CategoryView restores its own scroll from _cache.catView.
      // Fall back to hash polling only when there is no cache (first visit).
      if (_cache.catView?.id === resolvedCat) return
      if (!hash) return
      let cancelled = false
      let attempts = 0
      const poll = () => {
        if (cancelled) return
        const el = document.getElementById(hash)
        if (el) {
          el.scrollIntoView({ behavior: 'instant' as ScrollBehavior, block: 'center' })
          return
        }
        if (++attempts < 180) requestAnimationFrame(poll)
      }
      requestAnimationFrame(poll)
      return () => { cancelled = true }
    }

    // Hero carousel: the page doesn't scroll at all (see .pageLocked), and
    // CategoryRow's own autoFocusIdx (backed by _cache.lastRowFocusIdx) is
    // what puts focus back on the right card — a plain DOM scrollIntoView
    // here, with no idea about the carousel's transform/drum-slide layers or
    // its own horizontal scroll math, fought with that instead of doing
    // anything useful, landing the row on a half-scrolled position that
    // matched neither the hash card nor whichever one autoFocusIdx focused.
    // Search results render as a plain scrollable grid even under the hero
    // layout (showSearch forces carouselActive off), so this skip must not
    // apply while restoring back into a search — hence the query-length check.
    if (layout === 'hero' && searchQuery.length < 3) return

    if (_cache.categories.length > 0 && _cache.scrollY > 0) {
      window.scrollTo({ top: _cache.scrollY, behavior: 'instant' })
      if (window.location.hash) {
        window.history.replaceState(null, '', window.location.pathname + window.location.search)
      }
      return
    }
    if (!hash) return
    let cancelled = false
    let attempts = 0
    const poll = () => {
      if (cancelled) return
      const el = document.getElementById(hash)
      if (el) {
        window.history.replaceState(null, '', window.location.pathname + window.location.search)
        el.scrollIntoView({ behavior: 'instant' as ScrollBehavior, block: 'center', inline: 'center' })
        return
      }
      if (++attempts < 180) requestAnimationFrame(poll)
    }
    requestAnimationFrame(poll)
    return () => { cancelled = true }
  }, [])

  const handleItemsLoaded = useCallback((id: string, rowCache: RowCache) => {
    _cache.rows[id] = rowCache
  }, [])
  const dragSrcRef = useRef<string | null>(null)
  // Whether a card in the grid currently has keyboard focus — the hero
  // border (.cardHeroActive) only shows while true, so it disappears the
  // moment focus leaves for the top menu/search instead of staying stuck on
  // whichever card was focused last (hero.item itself keeps its value, so
  // the background stays as ambient art — only the "this card is selected"
  // border goes away).
  const [cardGridFocused, setCardGridFocused] = useState(false)

  useEffect(() => {
    function onFocusIn(e: FocusEvent) {
      const el = e.target as HTMLElement
      if (!el.hasAttribute('data-card')) {
        if (!el.closest('[data-row-id]')) setCardGridFocused(false)
        return
      }
      setCardGridFocused(true)
      const rowInner = el.closest<HTMLElement>('[data-row-id]')
      if (!rowInner) return
      const rowId = rowInner.dataset.rowId!
      const cards = Array.from(rowInner.querySelectorAll<HTMLElement>('[data-card]'))
      const idx = cards.indexOf(el)
      if (idx >= 0) _cache.lastRowFocusIdx.set(rowId, idx)
    }
    document.addEventListener('focusin', onFocusIn)
    return () => document.removeEventListener('focusin', onFocusIn)
  }, [])

  // Focus the floating input once it actually mounts (searchOpen just makes
  // it render — it doesn't exist in the DOM before that).
  useLayoutEffect(() => {
    if (searchOpen) floatingInputRef.current?.focus()
  }, [searchOpen])

  // Row cache is keyed by category id only (not profile) — drop it whenever the
  // active profile actually changes so CategoryRow doesn't flash stale items while
  // its key-driven remount re-fetches. Compared against _cache.profileKey (module-
  // level, not a ref) so a profile switch made while CatalogPage was unmounted
  // (e.g. from the admin panel) is still caught on remount — see _cache comment.
  // Must run synchronously during render (not in a useEffect): CategoryRow reads
  // _cache.rows[cat.id] as initialCache in this same render pass, before any
  // effect would get a chance to clear it.
  const profileKey = activeProfile ? `${activeProfile.device_id}:${activeProfile.profile_id}` : null
  // No `!== null` guard on the stored side — a profile-dependent row (only
  // "Непросмотренные" today) can fetch and cache an empty/wrong result
  // BEFORE activeProfile resolves for the first time (token/profileId are
  // still '' at that point), and that first transition is exactly
  // _cache.profileKey going from null to a real value. Skipping the clear
  // on that transition (the old behavior) left the poisoned empty cache
  // entry in place forever — the row's key-driven remount that follows
  // would just pick the bad cached result back up via initialCache instead
  // of refetching, which is what made it need a manual profile switch (a
  // guaranteed-non-null → different-non-null transition) to ever recover.
  if (_cache.profileKey !== profileKey) {
    _cache.rows = {}
    _cache.catView = null
  }
  _cache.profileKey = profileKey

  useEffect(() => {
    if (_cache.rawCategories.length > 0) return
    // Ждём реальных menuOrder/menuHidden, а не считаем сразу с дефолтными
    // ([]) — иначе первый рендер показывал строки в "естественном" порядке,
    // а через мгновение (как только настройка подгружалась с сервера)
    // список резко перекладывался/что-то исчезало — тот самый видимый
    // "флэш" при каждом открытии Каталога, если порядок/скрытие реально
    // настроены.
    if (!orderLoaded || !hiddenLoaded) return
    async function loadCategories() {
      // "Непросмотренные" — личная подборка (сериалы с невыпущенным новым
      // эпизодом), не идёт через общий /api/categories (его же читает np.js
      // для Lampa) — добавлена внутри fetchCatalogCategories.
      const cats = await fetchCatalogCategories()
      if (!cats.length) return
      _cache.rawCategories = cats
      _cache.shuffledBlock = shuffleArray(cats.filter(c => isCollectionsBlockMember(c.id)))
      const display = applyMenuOrder(cats, _cache.shuffledBlock, menuOrder, menuHidden)
      _cache.categories = display
      setCategories(display)
    }
    loadCategories()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderLoaded, hiddenLoaded])

  // Пересчитать порядок/видимость при изменении numparser_menu_sort/
  // numparser_menu_hide (после первой загрузки — см. эффект выше, который
  // сам применит текущие menuOrder/menuHidden при первом фетче) — не
  // реюфлит shuffledBlock (см. его комментарий), только перекладывает уже
  // отрисованные категории.
  useEffect(() => {
    if (!_cache.rawCategories.length) return
    const display = applyMenuOrder(_cache.rawCategories, _cache.shuffledBlock, menuOrder, menuHidden)
    _cache.categories = display
    setCategories(display)
  }, [menuOrder, menuHidden])

  // TMDB fallback (web-only — Lampa has its own search, see np.js): appended
  // after the local catalog results so a title we haven't parsed still turns
  // up, badged, with the option to track it (see CardDetailPage's status
  // buttons + /api/web/add-from-tmdb). Only run for a fresh query (page 1),
  // not on infinite-scroll pages of the local results — `limit` grows via
  // "Показать ещё из TMDB" (handleTmdbLoadMore) instead of true pagination,
  // since a bigger limit just re-slices the same already-fetched TMDB pool
  // server-side (see handleWebTMDBSearch), no extra TMDB calls needed here.
  const loadTmdbFallback = useCallback((query: string, localRows: MediaItem[], limit: number) => {
    const known = new Set(localRows.map(r => `${r.id}_${r.media_type}`))
    fetch(`/api/web/tmdb-search?q=${encodeURIComponent(query)}&limit=${limit}`)
      .then(r => r.ok ? r.json() : { results: [], has_more: false })
      .then(data => {
        if (searchQueryRef.current !== query) return // superseded by a newer query
        const extra: MediaItem[] = (data.results || []).filter(
          (r: MediaItem) => !known.has(`${r.id}_${r.media_type}`)
        )
        setTmdbResults(extra)
        setTmdbHasMore(!!data.has_more)
      })
      .catch(() => {})
      .finally(() => setTmdbLoadingMore(false))
  }, [])

  function handleTmdbLoadMore() {
    if (tmdbLoadingMore) return
    setTmdbLoadingMore(true)
    tmdbLimitRef.current += TMDB_PAGE_SIZE
    _cache.searchTmdbLimit = tmdbLimitRef.current
    loadTmdbFallback(searchQuery, localSearchRowsRef.current, tmdbLimitRef.current)
  }

  const loadSearchPage = useCallback((query: string, page: number, reset: boolean) => {
    setSearchLoading(true)
    fetch(`/api/search?q=${encodeURIComponent(query)}&page=${page}`)
      .then(r => r.ok ? r.json() : { results: [], total_pages: 1 })
      .then(data => {
        const rows: MediaItem[] = data.results || []
        setSearchResults(prev => reset ? rows : [...(prev ?? []), ...rows])
        setSearchHasMore((data.total_pages ?? 1) > page)
        searchPageRef.current = page
        _cache.searchPage = page
        setSearchLoading(false)
        if (reset) {
          localSearchRowsRef.current = rows
          _cache.searchLocalRows = rows
          tmdbLimitRef.current = TMDB_PAGE_SIZE
          _cache.searchTmdbLimit = TMDB_PAGE_SIZE
          loadTmdbFallback(query, rows, TMDB_PAGE_SIZE)
        }
      })
      .catch(() => {
        if (reset) setSearchResults([])
        setSearchLoading(false)
      })
  }, [loadTmdbFallback])

  useEffect(() => {
    searchQueryRef.current = searchQuery
    if (searchQuery.length < 3 || expandedCategory) {
      setSearchResults(null)
      setSearchHasMore(false)
      setTmdbResults([])
      setTmdbHasMore(false)
      return
    }
    if (skipInitialSearchFetchRef.current) {
      // Mounted with this exact query's results already restored from cache
      // (see the useState initializers above) — a normal fetch here would
      // reset straight back to page 1 and undo the whole point of caching them.
      skipInitialSearchFetchRef.current = false
      return
    }
    loadSearchPage(searchQuery, 1, true)
  }, [searchQuery, expandedCategory, loadSearchPage])

  // Infinite scroll for search results
  useEffect(() => {
    const sentinel = searchSentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && searchHasMore && !searchLoading) {
        loadSearchPage(searchQuery, searchPageRef.current + 1, false)
      }
    }, { rootMargin: '200px' })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [searchHasMore, searchLoading, searchQuery, loadSearchPage])

  function handleSearchChange(value: string) {
    setSearchValue(value)
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
    if (value.length === 0) {
      setSearchQuery('')
      setSearchResults(null)
      return
    }
    searchTimerRef.current = setTimeout(() => {
      const q = value.trim()
      if (q.length >= 3) {
        setSearchQuery(q)
      } else {
        setSearchQuery('')
        setSearchResults(null)
      }
    }, 400)
  }

  function handleExpandCategory(id: string, focusAfterIdx?: number) {
    savedScrollRef.current = window.scrollY
    if (_cache.catView?.id === id) _cache.catView.scrollY = 0
    setExpandedCategory(id)
    setExpandedFocusIdx(focusAfterIdx)
  }

  function handleBack() {
    // If we came from outside the catalog (e.g. admin), use browser history
    if (stateName) {
      navigate(-1)
      return
    }
    setExpandedCategory(null)
    const scrollY = savedScrollRef.current
    requestAnimationFrame(() => {
      window.scrollTo({ top: scrollY, behavior: 'instant' })
    })
  }

  function handleCardClick(item: MediaItem) {
    const cardId = `${item.id}_${item.media_type}`
    // Restored on return (see CategoryView's own restore effect) so the grid
    // regains keyboard focus on the exact card you left, not just the right
    // scroll position with nothing focused.
    _cache.lastFocusCardId = cardId
    const backUrl = expandedCategory
      ? `/catalog?cat=${encodeURIComponent(expandedCategory)}#${cardId}`
      : `/catalog#${cardId}`
    // Lets CardDetailPage paint its Hero background with this same image
    // immediately, instead of a blank "Загрузка…" screen until its own
    // fetch resolves — see the preview read there. Prefer the already-
    // resolved local detail (see getCachedHeroDetail's comment) over the
    // list item's own backdrop_path where available — for remote-sourced
    // categories (np_popular via popular_source_url) that field can be a
    // different TMDB backdrop than our own, which would otherwise flash
    // from that preview to the real one the instant the card loads.
    const cachedDetail = getCachedHeroDetail(cardId)
    navigate(`/card/${cardId}`, {
      state: { backUrl, preview: {
        poster_path: item.poster_path,
        backdrop_path: cachedDetail?.backdrop_path ?? item.backdrop_path ?? null,
      } },
    })
  }

  function onDragStart(_e: React.DragEvent, id: string) {
    dragSrcRef.current = id
  }

  function onDragEnd() {
    // genre_*/actor_*/director_* ids are ephemeral (re-randomized every
    // fetch, see isCollectionsBlockMember) — can't save them individually,
    // collapse the run to the one 'collections_block' placeholder np.js
    // itself uses (see applyMenuOrder for how that's expanded back).
    setMenuOrder(collapseCollectionsBlock(categories).map(c => c.id))
    dragSrcRef.current = null
  }

  function resetRowOrder() {
    setMenuOrder([])
  }

  function onDragOver(e: React.DragEvent, targetId: string) {
    e.preventDefault()
    const srcId = dragSrcRef.current
    if (!srcId || srcId === targetId) return
    setCategories(prev => {
      const srcIdx = prev.findIndex(c => c.id === srcId)
      const tgtIdx = prev.findIndex(c => c.id === targetId)
      if (srcIdx === -1 || tgtIdx === -1) return prev
      const next = [...prev]
      const [moved] = next.splice(srcIdx, 1)
      next.splice(tgtIdx, 0, moved)
      return next
    })
  }

  function onDrop(e: React.DragEvent) {
    e.preventDefault()
  }

  // Shared by ArrowUp/Down and the mobile swipe below — swaps which
  // category's row is shown in the hero carousel, with the drum-slide
  // transition for real navigation (as opposed to handleEmptyCategory's
  // silent skip).
  const switchCategory = useCallback((dir: 1 | -1) => {
    setActiveCategoryIndex(idx => {
      const next = idx + dir
      if (next < 0 || next >= categories.length) return idx
      setTransition({ prevIndex: idx, dir })
      if (transitionTimerRef.current) window.clearTimeout(transitionTimerRef.current)
      transitionTimerRef.current = window.setTimeout(() => {
        transitionTimerRef.current = null
        setTransition(null)
      }, CAROUSEL_TRANSITION_MS)
      return next
    })
  }, [categories.length])

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const focused = document.activeElement as HTMLElement | null
      const tag = focused?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'textarea' || tag === 'select') return

      if ((e.key === 'Backspace' || (e.key === 'ArrowLeft' && e.altKey)) && expandedCategory) {
        handleBack()
        return
      }
      if (e.key === 'Backspace' && !expandedCategory) {
        navigate(-1)
        return
      }

      // Escape while browsing search results (not just from the input
      // itself, which the floating bar's own onKeyDown already covers) —
      // clears the query and drops back to normal browsing.
      if (e.key === 'Escape' && !expandedCategory && searchQuery.length >= 3) {
        e.preventDefault()
        setSearchValue('')
        setSearchQuery('')
        setSearchOpen(false)
        focusTopNavActive()
        return
      }

      // Search results render the same flat .grid of cards as an expanded
      // category (not the row-carousel structure) — share the same grid
      // navigation instead of falling through to the row-based logic below,
      // which has nothing to walk here and left arrow keys doing nothing
      // once focus reached a search result card.
      if (expandedCategory || searchQuery.length >= 3) {
        if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
        e.preventDefault()
        if (shouldThrottleKeyRepeat(e, gridArrowRepeatRef)) return
        const cards = Array.from(document.querySelectorAll<HTMLElement>('[data-card]'))
        if (!cards.length) return
        const idx = cards.indexOf(focused as HTMLElement)
        let next = -1
        if (idx === -1) {
          next = 0
        } else {
          const cols = getGridCols(cards)
          if (e.key === 'ArrowRight') next = Math.min(idx + 1, cards.length - 1)
          else if (e.key === 'ArrowLeft') next = Math.max(idx - 1, 0)
          else if (e.key === 'ArrowDown') next = Math.min(idx + cols, cards.length - 1)
          else if (e.key === 'ArrowUp') {
            // Top row of search results — bridge straight back up to the
            // search icon in the nav (no such bridge for an expanded
            // category, which has no search box to return to).
            if (!expandedCategory && idx - cols < 0) {
              focusTopNavActive()
              return
            }
            next = Math.max(idx - cols, 0)
          }
        }
        if (next !== -1 && next !== idx) {
          // preventScroll — see the pendingFocus effect above for why.
          cards[next].focus({ preventScroll: true })
          scrollV(cards[next])
        }
        return
      }

      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        if (!focused?.hasAttribute('data-card')) {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            const first = document.querySelector<HTMLElement>('[data-card]')
            first?.focus({ preventScroll: true })
            // Hero carousel: the active row is always already pinned to the
            // bottom via CSS, nothing to scroll into view.
            if (layout !== 'hero' && first) scrollV(first)
          }
          return
        }

        e.preventDefault()

        if (layout === 'hero') {
          const dir = e.key === 'ArrowDown' ? 1 : -1
          if (dir < 0 && activeCategoryIndex === 0) {
            // Bridge up to the current page's own nav link (not the search
            // icon) — matches focusTopNavActive's own reasoning.
            focusTopNavActive()
            return
          }
          switchCategory(dir)
          return
        }

        // Classic: scrollable list of every row — walk the DOM to find
        // whichever one is next/previous and focus into it.
        const rowInner = focused.closest('[data-row-id]') as HTMLElement | null
        if (!rowInner) return
        const allRows = Array.from(document.querySelectorAll<HTMLElement>('[data-row-id]'))
        const rowIdx = allRows.indexOf(rowInner)
        const targetRowIdx = e.key === 'ArrowDown' ? rowIdx + 1 : rowIdx - 1
        if (targetRowIdx < 0) {
          focusTopNavActive()
          return
        }
        if (targetRowIdx >= allRows.length) return
        const targetRow = allRows[targetRowIdx]
        const targetRowId = targetRow.dataset.rowId!
        const savedIdx = _cache.lastRowFocusIdx.get(targetRowId) ?? 0
        const targetCards = Array.from(targetRow.querySelectorAll<HTMLElement>('[data-card]'))
        if (!targetCards.length) {
          scrollV(targetRow)
          targetRow.dataset.pendingFocus = String(savedIdx)
          return
        }
        const target = targetCards[Math.min(savedIdx, targetCards.length - 1)]
        target?.focus({ preventScroll: true })
        if (target) { scrollH(target); scrollV(target) }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [expandedCategory, searchQuery, navigate, layout, activeCategoryIndex, categories, switchCategory])

  const showSearch = searchQuery.length >= 3 && !expandedCategory

  // Hero carousel locks the page to the viewport (no scroll) — only while
  // actually showing the carousel itself, not the expanded/"Все →" grid or
  // search results, which stay normal scrollable views.
  const carouselActive = layout === 'hero' && !expandedCategory && !showSearch

  // Hero carousel: the page is scroll-locked (see carouselActive/.pageLocked
  // below), so a mouse wheel over it would otherwise do nothing — repurpose
  // it as ArrowUp/Down's equivalent (switch category) everywhere except over
  // the card rail itself, which converts the same wheel into horizontal
  // scroll instead (see CategoryRow's own wheel effect, data-row-scroll is
  // how this tells the two apart). One switch per wheel "gesture", not per
  // delta event — a trackpad swipe fires dozens of wheel events for what's
  // really one intent, same reasoning as CAROUSEL_TRANSITION_MS gating the
  // drum-slide transition itself.
  useEffect(() => {
    if (!carouselActive) return
    const el = carouselPageRef.current
    if (!el) return
    let cooling = false
    function onWheel(e: WheelEvent) {
      if ((e.target as HTMLElement).closest('[data-row-scroll]')) return
      e.preventDefault()
      if (cooling) return
      cooling = true
      window.setTimeout(() => { cooling = false }, CATEGORY_WHEEL_COOLDOWN_MS)
      switchCategory(e.deltaY > 0 ? 1 : -1)
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [carouselActive, switchCategory])

  useEffect(() => {
    const onCatalogBack = () => {
      if (expandedCategory) handleBack()
      // Clicking "Каталог" (or any other top-nav link/the bottom-nav
      // "Главная") while search results are showing should reset back to
      // normal browsing just like navigating anywhere else does — nothing
      // else clears search state for you when you're already on /catalog,
      // since that click doesn't actually remount this page.
      setSearchValue('')
      setSearchQuery('')
      setSearchOpen(false)
    }
    window.addEventListener('catalog:back', onCatalogBack)
    return () => window.removeEventListener('catalog:back', onCatalogBack)
  }, [expandedCategory]) // eslint-disable-line react-hooks/exhaustive-deps

  // Header search icon (Layout.tsx's handleBottomSearch) — closes an
  // expanded category (search results can't show underneath one) without
  // touching an already-in-progress query, unlike catalog:back above.
  useEffect(() => {
    const onCloseExpanded = () => { if (expandedCategory) handleBack() }
    window.addEventListener('catalog:close-expanded', onCloseExpanded)
    return () => window.removeEventListener('catalog:close-expanded', onCloseExpanded)
  }, [expandedCategory]) // eslint-disable-line react-hooks/exhaustive-deps

  // Bottom-nav "Поиск" — focus the main search input. Covers both being
  // triggered while already on /catalog (event, no remount) and navigating
  // here from another page (flag checked once on mount, since the click's
  // event dispatch happens before this component even exists in that case).
  // useLayoutEffect, not useEffect: Layout wraps the cross-page navigation in
  // flushSync so this whole mount (render+commit+layout effects) happens
  // synchronously inside the original click handler. That's required for
  // iOS Safari to treat the resulting focus() as part of the trusted user
  // gesture and actually raise the keyboard — a regular useEffect fires in a
  // separate task after the gesture window closes, so focus() would "work"
  // (activeElement changes) but the keyboard would stay closed.
  useLayoutEffect(() => {
    function openMainSearch() {
      setSearchOpen(true)
    }
    if (takePendingFocusCatalogSearch()) openMainSearch()
    window.addEventListener('catalog:focus-search', openMainSearch)
    return () => window.removeEventListener('catalog:focus-search', openMainSearch)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const stateName = (location.state as { catName?: string } | null)?.catName ?? null
  const expandedCat = categories.find(c => c.id === expandedCategory) ?? (
    expandedCategory && (expandedCategory.startsWith('actor_') || expandedCategory.startsWith('director_'))
      ? { id: expandedCategory, name: stateName ?? expandedCategory }
      : null
  )

  return (
    <Layout>
      <div ref={carouselPageRef} className={`${styles.page}${carouselActive ? ' ' + styles.pageLocked : ''}`}>
        {!expandedCat && menuOrder.length > 0 && layout === 'classic' && (
          <div className={styles.toolbar}>
            <div className={styles.toolbarTop}>
              <button className={styles.resetOrderBtn} onClick={resetRowOrder} title="Вернуть порядок по умолчанию">
                Сбросить порядок
              </button>
            </div>
          </div>
        )}

        {expandedCat && (
          <CategoryView
            category={expandedCat}
            token={token}
            profileId={profileId}
            hideWatched={hideWatched}
            hidePercent={hidePercent}
            hideWatchedLoaded={hideWatchedLoaded}
            unwatchedSort={unwatchedSort}
            unwatchedSortLoaded={unwatchedSortLoaded}
            onBack={handleBack}
            onCardClick={handleCardClick}
            focusAfterIdx={expandedFocusIdx}
          />
        )}

        {!expandedCategory && showSearch && (
          <div>
            {searchResults !== null && searchResults.length === 0 && tmdbResults.length === 0 && !searchLoading && (
              <div className={styles.empty}>Ничего не найдено</div>
            )}
            {searchResults !== null && (searchResults.length > 0 || tmdbResults.length > 0) && (
              <div className={styles.grid}>
                {[...searchResults, ...tmdbResults].map(item => {
                  const cardId = `${item.id}_${item.media_type}`
                  return (
                    <MediaCard key={cardId} item={item} onClick={() => handleCardClick(item)} />
                  )
                })}
              </div>
            )}
            {tmdbHasMore && (
              <button
                type="button"
                className={styles.tmdbLoadMore}
                onClick={handleTmdbLoadMore}
                disabled={tmdbLoadingMore}
              >
                {tmdbLoadingMore ? 'Загрузка…' : 'Показать ещё из TMDB'}
              </button>
            )}
            {searchLoading && <div className={styles.loading}>Поиск…</div>}
            <div ref={searchSentinelRef} className={styles.sentinel} />
          </div>
        )}

        {!expandedCategory && !showSearch && layout === 'hero' && (
          <BrowseHero
            item={hero.item}
            detail={hero.detail}
            onOpen={() => hero.item && handleCardClick(hero.item)}
          />
        )}

        {carouselActive && categories[activeCategoryIndex] && (
          <div className={styles.carouselRail}>
            {/* Owned by the parent, not CategoryRow itself — switching to a
                category that turns out empty briefly showed its title before
                CategoryRow returned null and handleEmptyCategory skipped to
                the next one, which read as the title flickering on every
                auto-skip. This one just always names whatever's targeted.
                The dimmer neighbor names above/below hint that ArrowUp/Down
                reaches more categories, without taking any horizontal space
                away from the row of cards below. */}
            <div className={styles.categoryTitleStack}>
              {/*  , not a plain space — a lone regular space is
                  collapsible whitespace, so browsers render that line at
                  zero height until real text lands in it, which made the
                  hero area's height (and everything anchored to its bottom)
                  visibly jump on every category change. A non-breaking space
                  keeps the line's height reserved even when empty. */}
              <span className={styles.categoryTitleNeighbor}>{categories[activeCategoryIndex - 1]?.name ?? ' '}</span>
              <h3 className={styles.rowTitle}>{categories[activeCategoryIndex].name}</h3>
              <span className={styles.categoryTitleNeighbor}>{categories[activeCategoryIndex + 1]?.name ?? ' '}</span>
            </div>
            <div className={styles.carouselViewport}>
              {/* Outgoing row — same key it had before the switch, so React
                  keeps reusing the already-fetched instance instead of
                  remounting/refetching it just to animate it away. */}
              {transition && categories[transition.prevIndex] && (
                <div className={`${styles.carouselLayerOut} ${transition.dir > 0 ? styles.carouselOutToTop : styles.carouselOutToBottom}`}>
                  <CategoryRow
                    key={`${categories[transition.prevIndex].id}_${token}_${profileId}`}
                    category={categories[transition.prevIndex]}
                    token={token}
                    profileId={profileId}
                    hideWatched={hideWatched}
                    hidePercent={hidePercent}
                    hideWatchedLoaded={hideWatchedLoaded}
                    unwatchedSort={unwatchedSort}
                    unwatchedSortLoaded={unwatchedSortLoaded}
                    onExpandCategory={handleExpandCategory}
                    onCardClick={handleCardClick}
                    initialCache={_cache.rows[categories[transition.prevIndex].id]}
                    onItemsLoaded={handleItemsLoaded}
                    hideHeader
                  />
                </div>
              )}
              <div className={transition ? (transition.dir > 0 ? styles.carouselInFromBottom : styles.carouselInFromTop) : undefined}>
                <CategoryRow
                  key={`${categories[activeCategoryIndex].id}_${token}_${profileId}`}
                  category={categories[activeCategoryIndex]}
                  token={token}
                  profileId={profileId}
                  hideWatched={hideWatched}
                  hidePercent={hidePercent}
                  hideWatchedLoaded={hideWatchedLoaded}
                  unwatchedSort={unwatchedSort}
                  unwatchedSortLoaded={unwatchedSortLoaded}
                  onExpandCategory={handleExpandCategory}
                  onCardClick={handleCardClick}
                  onActivate={handleActivate}
                  activeCardId={cardGridFocused && hero.item ? `${hero.item.id}_${hero.item.media_type}` : null}
                  initialCache={_cache.rows[categories[activeCategoryIndex].id]}
                  onItemsLoaded={handleItemsLoaded}
                  onEmpty={handleEmptyCategory}
                  autoFocusIdx={_cache.lastRowFocusIdx.get(categories[activeCategoryIndex].id) ?? 0}
                  hideHeader
                />
              </div>
            </div>
          </div>
        )}

        {!expandedCategory && !showSearch && layout === 'classic' && (
          <div className={styles.rows}>
            {categories.map(cat => (
              <CategoryRow
                key={`${cat.id}_${token}_${profileId}`}
                category={cat}
                token={token}
                profileId={profileId}
                hideWatched={hideWatched}
                hidePercent={hidePercent}
                hideWatchedLoaded={hideWatchedLoaded}
                unwatchedSort={unwatchedSort}
                unwatchedSortLoaded={unwatchedSortLoaded}
                onExpandCategory={handleExpandCategory}
                onCardClick={handleCardClick}
                dragHandlers={{ onDragStart, onDragEnd, onDragOver, onDrop }}
                initialCache={_cache.rows[cat.id]}
                onItemsLoaded={handleItemsLoaded}
              />
            ))}
          </div>
        )}
        {!expandedCategory && searchOpen && (
          <div
            className={styles.floatingBar}
            onBlur={e => {
              // Closes as soon as focus actually leaves the bar (Escape, the
              // ✕ button, or ArrowUp bridging back to the search icon below
              // all move focus elsewhere and land here) — not just on the
              // explicit affordances, so it never lingers open once you've
              // moved on.
              if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setSearchOpen(false)
            }}
          >
            <div className={styles.floatingBarInner}>
              <span className={styles.floatingIcon}>🔍</span>
              <div className={styles.searchWrap} style={{flex: 1}}>
                <input
                  ref={floatingInputRef}
                  className={styles.floatingInput}
                  placeholder="Поиск…"
                  value={searchValue}
                  onChange={e => handleSearchChange(e.target.value)}
                  onKeyDown={e => {
                    // Same site-wide keyboard-nav rollout as before this
                    // moved into the floating bar — Down bridges to the
                    // first card, Up/Escape bridge back to the search icon
                    // (onBlur above then closes the bar). stopPropagation is
                    // required: without it the same event also reaches the
                    // window-level grid-nav listener below (search results
                    // share its Left/Right/Up/Down handling), which then
                    // moves focus AGAIN from wherever we just landed —
                    // ArrowDown here would jump straight to the first card,
                    // then immediately a whole row further.
                    if (e.key === 'ArrowDown') {
                      e.preventDefault()
                      e.stopPropagation()
                      const first = document.querySelector<HTMLElement>('[data-card]')
                      first?.focus({ preventScroll: true })
                      if (layout !== 'hero' && first) scrollV(first)
                    } else if (e.key === 'ArrowUp' || e.key === 'Escape') {
                      e.preventDefault()
                      e.stopPropagation()
                      focusTopNavActive()
                    }
                  }}
                />
              </div>
              {/* One button, not clear+close side by side — a single ✕ that
                  both empties the field and exits search reads clearer than
                  making the user pick which of two identical glyphs to tap.
                  onMouseDown preventDefault is required, not decoration:
                  buttons don't take focus on tap in iOS Safari, so tapping
                  this one blurs the input straight to nothing — which fires
                  .floatingBar's own onBlur (searchOpen -> false) and unmounts
                  this button BEFORE the deferred click event it's waiting on
                  ever arrives, silently dropping onClick (closes but never
                  clears). Preventing mousedown's default stops the browser
                  from shifting focus away from the input at all, so onBlur
                  never fires from this tap and the click below still lands
                  on a button that still exists. */}
              <button
                className={styles.floatingClose}
                onMouseDown={e => e.preventDefault()}
                onClick={() => { setSearchValue(''); setSearchQuery(''); setSearchOpen(false) }}
                title="Закрыть"
              >✕</button>
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
