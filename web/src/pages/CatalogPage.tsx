import { useEffect, useLayoutEffect, useState, useCallback, useRef } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, scrollH, getGridCols, CAROUSEL_TRANSITION_MS, NAV_H } from '@/utils/scrollNav'
import { takePendingFocusCatalogSearch } from '@/utils/catalogSearchFocus'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { getEffectiveBrowseLayout } from '@/utils/browseLayout'
import { BrowseHero, useHeroPreview } from '@/components/BrowseHero'
import styles from './CatalogPage.module.scss'

interface MediaItem {
  id: number
  media_type: string
  title: string
  name?: string
  poster_path: string | null
  vote_average: number
  release_date: string
  first_air_date: string
  release_quality: string
  certification_ru?: string
  certification_us?: string
  category_name?: string
  year?: string
  unwatched_count?: number
  watched_count?: number
  aired_count?: number
  next_episode?: string
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


const LS_ROW_ORDER    = 'catalog_row_order'

// Module-level cache — survives SPA navigation, resets on full page reload.
interface RowCache { items: MediaItem[]; totalPages: number }
interface CatViewCache { id: string; items: MediaItem[]; totalPages: number; currentPage: number; scrollY: number }
const _cache = {
  categories: [] as Category[],
  rows: {} as Record<string, RowCache>,
  scrollY: 0,
  catView: null as CatViewCache | null,
  // Profile the cached rows/catView belong to — module-level (not a ref) because
  // a profile switch made while CatalogPage is unmounted (e.g. from the admin
  // panel) must still be detected on remount; a component-local ref would reset
  // to "unknown" on every mount and miss changes that happened while away.
  profileKey: null as string | null,
}

export function invalidateCatalogCache() {
  _cache.categories = []
  _cache.rows = {}
  _cache.catView = null
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

function shuffleArray<T>(arr: T[]): T[] {
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

// Shuffle genre_* and actor_* rows in place; keep other categories in their original positions.
function randomizeGenres(categories: Category[]): Category[] {
  const shuffleIds = new Set(categories.filter(c => c.id.startsWith('genre_') || c.id.startsWith('actor_') || c.id.startsWith('director_')).map(c => c.id))
  const shuffled = shuffleArray(categories.filter(c => shuffleIds.has(c.id)))
  let gi = 0
  return categories.map(c => (shuffleIds.has(c.id) ? shuffled[gi++] : c))
}

function applyRowOrder(categories: Category[]): Category[] {
  try {
    const saved: string[] = JSON.parse(localStorage.getItem(LS_ROW_ORDER) || '[]')
    if (!saved.length) return categories
    const map = Object.fromEntries(categories.map(c => [c.id, c]))
    const ordered = saved.filter(id => map[id]).map(id => map[id])
    const rest = categories.filter(c => !saved.includes(c.id))
    return [...ordered, ...rest]
  } catch {
    return categories
  }
}

function saveRowOrder(ids: string[]) {
  try { localStorage.setItem(LS_ROW_ORDER, JSON.stringify(ids)) } catch {}
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
        {!!item.unwatched_count && (
          <span className={styles.unwatchedBadge}>{item.unwatched_count}</span>
        )}
        {item.next_episode && (
          <span className={styles.nextEpBadge}>{item.next_episode}</span>
        )}
        {!!item.aired_count && (
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

function CategoryRow({ category, token, profileId, onExpandCategory, onCardClick, onActivate, activeCardId, dragHandlers, initialCache, onItemsLoaded, onEmpty, autoFocusIdx, hideHeader }: CategoryRowProps) {
  const [items, setItems] = useState<MediaItem[] | null>(initialCache?.items ?? null)
  const [totalPages, setTotalPages] = useState(initialCache?.totalPages ?? 1)
  const [error, setError] = useState(false)
  const rowRef = useRef<HTMLElement>(null)
  const rowInnerRef = useRef<HTMLDivElement>(null)
  const loadedRef = useRef(!!initialCache)
  const autoFocusAppliedRef = useRef(false)

  const loadItems = useCallback(async () => {
    if (loadedRef.current) return
    loadedRef.current = true
    try {
      const params = new URLSearchParams({ per_page: '20', page: '1' })
      if (token && profileId != null) {
        params.set('token', token)
        params.set('profile_id', profileId)
      }
      const res = await fetch(`/${encodeURIComponent(category.id)}?${params}`)
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const data: CatalogResponse = await res.json()
      const results = data.results || []
      const tp = data.total_pages || 1
      setTotalPages(tp)
      setItems(results)
      onItemsLoaded(category.id, { items: results, totalPages: tp })
    } catch {
      setError(true)
    }
  }, [category.id, token, profileId, onItemsLoaded])

  // Carousel mode: this category has nothing to show — tell the parent to
  // advance instead of leaving a blank screen (Classic layout doesn't pass
  // onEmpty; it just relies on the render-time `return null` below).
  useEffect(() => {
    if (items !== null && items.length === 0) onEmpty?.()
  }, [items, onEmpty])

  useEffect(() => {
    if (autoFocusIdx === undefined || autoFocusAppliedRef.current || !items?.length) return
    autoFocusAppliedRef.current = true
    requestAnimationFrame(() => {
      const cards = rowInnerRef.current?.querySelectorAll<HTMLElement>('[data-card]')
      if (!cards?.length) return
      cards[Math.min(autoFocusIdx, cards.length - 1)]?.focus({ preventScroll: true })
    })
  }, [items, autoFocusIdx])

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
      <div className={`${styles.rowScroll}${hideHeader ? ' ' + styles.rowScrollCompact : ''}`} data-row-scroll>
        <div
          ref={rowInnerRef}
          className={styles.rowInner}
          data-row-id={category.id}
          onKeyDown={e => {
            if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
            const cards = Array.from(
              (e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[data-card]')
            )
            const idx = cards.indexOf(document.activeElement as HTMLElement)
            if (idx === -1) return
            e.preventDefault()
            if (e.key === 'ArrowRight') {
              if (idx === cards.length - 1) {
                if (hasMore) onExpandCategory(category.id, items?.length ?? 0)
              } else {
                const next = cards[idx + 1]
                // preventScroll — see the pendingFocus effect above for why;
                // also matters here since this move is horizontal-only (no
                // scrollV call), so a native vertical auto-scroll would go
                // uncorrected instead of just landing early.
                next?.focus({ preventScroll: true })
                // Horizontal-only move within the same row — its vertical position
                // doesn't change, so no scrollV here (it would force-recenter the
                // page, yanking the hero banner out of view for no reason).
                if (next) scrollH(next)
              }
            } else {
              const prev = cards[idx - 1]
              prev?.focus({ preventScroll: true })
              if (prev) scrollH(prev)
            }
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
                onClick={() => onExpandCategory(category.id)}
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
  onBack: () => void
  onCardClick: (item: MediaItem) => void
  focusAfterIdx?: number
}

function CategoryView({ category, token, profileId, onBack, onCardClick, focusAfterIdx }: CategoryViewProps) {
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
  const loadedRef = useRef(!!cached)
  const prevSearchRef = useRef('')
  const prevTokenRef = useRef(token)
  const prevProfileRef = useRef(profileId)

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

  // Restore scroll when returning with cached items
  useLayoutEffect(() => {
    if (cached && cached.scrollY > 0) window.scrollTo({ top: cached.scrollY, behavior: 'instant' })
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
      }
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
        if (_cache.catView?.id === category.id) _cache.catView.items = next
        return next
      })
      if (pg === 1 && results.length === 0) setEmpty(true)
      else setEmpty(false)
    } catch {
    } finally {
      loadingRef.current = false
      setLoading(false)
    }
  }, [category.id, token, profileId])

  useEffect(() => {
    const searchChanged = searchQuery !== prevSearchRef.current
    const tokenChanged = token !== prevTokenRef.current
    const profileChanged = profileId !== prevProfileRef.current
    prevSearchRef.current = searchQuery
    prevTokenRef.current = token
    prevProfileRef.current = profileId
    if (!searchChanged && !tokenChanged && !profileChanged && loadedRef.current) return
    if (tokenChanged || profileChanged) {
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
  }, [searchQuery, loadPage]) // eslint-disable-line react-hooks/exhaustive-deps

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
  const [categories, setCategories] = useState<Category[]>(() => applyRowOrder(_cache.categories))
  const [hasCustomOrder, setHasCustomOrder] = useState(() => !!localStorage.getItem(LS_ROW_ORDER))
  const [expandedCategory, setExpandedCategory] = useState<string | null>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.get('cat')
  })
  const [expandedFocusIdx, setExpandedFocusIdx] = useState<number | undefined>(undefined)
  const [searchValue, setSearchValue] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<MediaItem[] | null>(null)
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchHasMore, setSearchHasMore] = useState(false)
  const searchSentinelRef = useRef<HTMLDivElement>(null)
  const searchPageRef = useRef(1)

  const { activeDevice, activeProfile } = useActiveProfile()

  // Per-device (localStorage) — see BrowseLayoutSettings on /profiles. Read
  // fresh on every mount, same convention as CardDetailPage's cardLayout.
  // Forced to Classic on touch devices regardless of the saved preference —
  // the hero carousel is driven by keyboard/mouse focus, which touch has no
  // equivalent for.
  const [layout] = useState(() => getEffectiveBrowseLayout())
  const hero = useHeroPreview<MediaItem>()
  const handleActivate = useCallback((item: MediaItem) => hero.activate(item), [hero.activate])

  // Hero layout: non-scrolling carousel (Lampa TV-style) — exactly one
  // category's row is mounted at a time, always pinned to the bottom via CSS
  // (see .carouselRail), never revealed by scrolling. ArrowUp/Down (or a
  // swipe) just swaps which category that is; the row itself doesn't need to
  // be scrolled into place at all, so there's no scroll-position bookkeeping
  // here — CategoryRow's own onFocus→onActivate wiring drives the hero from
  // whichever card ends up focused, same as any other navigation.
  const [activeCategoryIndex, setActiveCategoryIndex] = useState(0)
  // Drum-carousel row switch — both the outgoing (prevIndex) and incoming
  // (activeCategoryIndex) rows render at once, sliding together the same
  // direction (see .carouselViewport/CAROUSEL_TRANSITION_MS), for as long as
  // this is non-null; then the outgoing one is dropped.
  const [transition, setTransition] = useState<{ prevIndex: number; dir: 1 | -1 } | null>(null)
  const transitionTimerRef = useRef<number | null>(null)
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
  // If ?cat= is in URL — expand that category and rAF-poll for the hash card.
  // If cache is warm — content renders synchronously, scroll restores immediately.
  // Otherwise fall back to hash-based scroll for the first visit.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const catParam = params.get('cat')
    const hash = window.location.hash.slice(1)

    if (catParam) {
      // expandedCategory is already set from the URL initial state — just clean up the URL.
      window.history.replaceState(null, '', window.location.pathname)
      // CategoryView restores its own scroll from _cache.catView.
      // Fall back to hash polling only when there is no cache (first visit).
      if (_cache.catView?.id === catParam) return
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
  const lastRowFocusIdx = useRef<Map<string, number>>(new Map())
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
      if (idx >= 0) lastRowFocusIdx.current.set(rowId, idx)
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
  if (_cache.profileKey !== null && _cache.profileKey !== profileKey) {
    _cache.rows = {}
    _cache.catView = null
  }
  _cache.profileKey = profileKey

  useEffect(() => {
    if (_cache.categories.length > 0) {
      setCategories(applyRowOrder(_cache.categories))
      return
    }
    async function loadCategories() {
      try {
        const res = await fetch('/api/categories')
        if (!res.ok) return
        const cats: Category[] = await res.json()
        // "Непросмотренные" — личная подборка (сериалы с невыпущенным новым эпизодом),
        // не идёт через общий /api/categories (его же читает np.js для Lampa).
        const withUnwatched: Category[] = [{ id: 'unwatched', name: 'Непросмотренные' }, ...cats]
        const randomized = randomizeGenres(withUnwatched)
        _cache.categories = randomized
        setCategories(applyRowOrder(randomized))
      } catch {}
    }
    loadCategories()
  }, [])

  const loadSearchPage = useCallback((query: string, page: number, reset: boolean) => {
    setSearchLoading(true)
    fetch(`/api/search?q=${encodeURIComponent(query)}&page=${page}`)
      .then(r => r.ok ? r.json() : { results: [], total_pages: 1 })
      .then(data => {
        const rows: MediaItem[] = data.results || []
        setSearchResults(prev => reset ? rows : [...(prev ?? []), ...rows])
        setSearchHasMore((data.total_pages ?? 1) > page)
        searchPageRef.current = page
        setSearchLoading(false)
      })
      .catch(() => {
        if (reset) setSearchResults([])
        setSearchLoading(false)
      })
  }, [])

  useEffect(() => {
    if (searchQuery.length < 3 || expandedCategory) {
      setSearchResults(null)
      setSearchHasMore(false)
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
    const backUrl = expandedCategory
      ? `/catalog?cat=${encodeURIComponent(expandedCategory)}#${cardId}`
      : `/catalog#${cardId}`
    navigate(`/card/${cardId}`, { state: { backUrl } })
  }

  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''

  function onDragStart(_e: React.DragEvent, id: string) {
    dragSrcRef.current = id
  }

  function onDragEnd() {
    const ids = categories.map(c => c.id)
    saveRowOrder(ids)
    setHasCustomOrder(true)
    dragSrcRef.current = null
  }

  function resetRowOrder() {
    try { localStorage.removeItem(LS_ROW_ORDER) } catch {}
    setCategories(_cache.categories)
    setHasCustomOrder(false)
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
        document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
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
              document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
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
            // The search icon is the first item in the top nav — bridge
            // straight to it (not the active page link) so ArrowUp always
            // lands on whatever's leftmost, matching the visual order.
            document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
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
          document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
          return
        }
        if (targetRowIdx >= allRows.length) return
        const targetRow = allRows[targetRowIdx]
        const targetRowId = targetRow.dataset.rowId!
        const savedIdx = lastRowFocusIdx.current.get(targetRowId) ?? 0
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

  const showSearch = searchQuery.length >= 3 && !expandedCategory

  // Hero carousel locks the page to the viewport (no scroll) — only while
  // actually showing the carousel itself, not the expanded/"Все →" grid or
  // search results, which stay normal scrollable views.
  const carouselActive = layout === 'hero' && !expandedCategory && !showSearch

  return (
    <Layout>
      <div className={`${styles.page}${carouselActive ? ' ' + styles.pageLocked : ''}`}>
        {!expandedCat && hasCustomOrder && layout === 'classic' && (
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
            onBack={handleBack}
            onCardClick={handleCardClick}
            focusAfterIdx={expandedFocusIdx}
          />
        )}

        {!expandedCategory && showSearch && (
          <div>
            {searchResults !== null && searchResults.length === 0 && !searchLoading && (
              <div className={styles.empty}>Ничего не найдено</div>
            )}
            {searchResults !== null && searchResults.length > 0 && (
              <div className={styles.grid}>
                {searchResults.map(item => {
                  const cardId = `${item.id}_${item.media_type}`
                  return (
                    <MediaCard key={cardId} item={item} onClick={() => handleCardClick(item)} />
                  )
                })}
              </div>
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
                  onExpandCategory={handleExpandCategory}
                  onCardClick={handleCardClick}
                  onActivate={handleActivate}
                  activeCardId={cardGridFocused && hero.item ? `${hero.item.id}_${hero.item.media_type}` : null}
                  initialCache={_cache.rows[categories[activeCategoryIndex].id]}
                  onItemsLoaded={handleItemsLoaded}
                  onEmpty={handleEmptyCategory}
                  autoFocusIdx={lastRowFocusIdx.current.get(categories[activeCategoryIndex].id) ?? 0}
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
                      document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
                    }
                  }}
                />
                {searchValue && (
                  <button className={styles.searchClear} onClick={() => handleSearchChange('')} title="Очистить">✕</button>
                )}
              </div>
              <button className={styles.floatingClose} onClick={() => setSearchOpen(false)} title="Закрыть">✕</button>
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
