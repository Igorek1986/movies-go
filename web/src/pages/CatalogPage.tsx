import { useEffect, useLayoutEffect, useState, useCallback, useRef } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, getGridCols } from '@/utils/scrollNav'
import { takePendingFocusCatalogSearch } from '@/utils/catalogSearchFocus'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { getStoredBrowseLayout } from '@/utils/browseLayout'
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

// Scroll the horizontal row container so el is centered.
// CSS scroll-behavior: smooth on .rowScroll handles the animation.
function scrollH(el: HTMLElement) {
  const scroll = el.closest<HTMLElement>('[data-row-scroll]')
  if (!scroll) return
  const sr = scroll.getBoundingClientRect()
  const cr = el.getBoundingClientRect()
  const relCenter = cr.left - sr.left + scroll.scrollLeft + cr.width / 2
  scroll.scrollTo({ left: relCenter - scroll.clientWidth / 2 })
}

const LS_ROW_ORDER    = 'catalog_row_order'
const SS_SEARCH = 'catalog_search'

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
}

function MediaCard({ item, onClick, onActivate, isHeroActive }: CardProps) {
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
      onMouseEnter={onActivate}
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
  dragHandlers: {
    onDragStart: (e: React.DragEvent, id: string) => void
    onDragEnd: () => void
    onDragOver: (e: React.DragEvent, id: string) => void
    onDrop: (e: React.DragEvent) => void
  }
  initialCache?: RowCache
  onItemsLoaded: (id: string, cache: RowCache) => void
}

function CategoryRow({ category, token, profileId, onExpandCategory, onCardClick, onActivate, activeCardId, dragHandlers, initialCache, onItemsLoaded }: CategoryRowProps) {
  const [items, setItems] = useState<MediaItem[] | null>(initialCache?.items ?? null)
  const [totalPages, setTotalPages] = useState(initialCache?.totalPages ?? 1)
  const [error, setError] = useState(false)
  const rowRef = useRef<HTMLElement>(null)
  const rowInnerRef = useRef<HTMLDivElement>(null)
  const loadedRef = useRef(!!initialCache)

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

  useEffect(() => {
    if (items === null) return
    const el = rowInnerRef.current
    if (!el?.dataset.pendingFocus) return
    const savedIdx = parseInt(el.dataset.pendingFocus, 10) || 0
    delete el.dataset.pendingFocus
    requestAnimationFrame(() => {
      const cards = el.querySelectorAll<HTMLElement>('[data-card]')
      const target = cards[Math.min(savedIdx, cards.length - 1)]
      target?.focus()
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
      draggable
      onDragStart={e => dragHandlers.onDragStart(e, category.id)}
      onDragEnd={dragHandlers.onDragEnd}
      onDragOver={e => dragHandlers.onDragOver(e, category.id)}
      onDrop={dragHandlers.onDrop}
    >
      <div className={styles.rowHeader}>
        <div className={styles.rowHeaderLeft}>
          <span className={styles.dragHandle} title="Перетащить">⠿</span>
          <h3 className={styles.rowTitle}>{category.name}</h3>
        </div>
        {hasMore && (
          <button className={styles.rowMore} onClick={() => onExpandCategory(category.id)}>
            Все →
          </button>
        )}
      </div>
      <div className={styles.rowScroll} data-row-scroll>
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
                next?.focus()
                if (next) { scrollH(next); scrollV(next) }
              }
            } else {
              const prev = cards[idx - 1]
              prev?.focus()
              if (prev) { scrollH(prev); scrollV(prev) }
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

  // Show floating search bar when the search field scrolls above the nav (52px)
  useEffect(() => {
    function check() {
      const el = catSearchRef.current
      if (!el) return
      setSearchFloating(el.getBoundingClientRect().bottom < 52)
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
        target?.focus()
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
  const [layout] = useState(() => getStoredBrowseLayout())
  const hero = useHeroPreview<MediaItem>()
  const heroInitRef = useRef(false)
  // Rows lazy-load independently (IntersectionObserver) and can resolve in
  // any order — reading whichever happens to answer first made the initial
  // hero background essentially random. Scan categories in visual order
  // instead, stopping at the first one whose row cache isn't in yet, so the
  // result is always the first row's first item regardless of network timing.
  const categoriesRef = useRef(categories)
  categoriesRef.current = categories

  const tryInitHero = useCallback(() => {
    if (heroInitRef.current) return
    for (const cat of categoriesRef.current) {
      const cached = _cache.rows[cat.id]
      if (!cached) return // earlier-in-order row hasn't reported yet — wait for it
      if (cached.items.length > 0) {
        heroInitRef.current = true
        hero.activate(cached.items[0])
        // Actually focus the card (not just hero state) so it's visibly
        // marked as active, same as hovering/tabbing to it manually would —
        // preventScroll since it's already on-screen at the top.
        requestAnimationFrame(() => {
          document.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(cat.id)}"] [data-card]`)
            ?.focus({ preventScroll: true })
        })
        return
      }
      // else: this row loaded empty — keep scanning the next one
    }
  }, [hero.activate])

  // Warm SPA-cache revisit: rows with a cache hit skip their own fetch (see
  // CategoryRow's loadedRef) and never call onItemsLoaded again, so without
  // this the hero would just stay empty on a Backspace-back to /catalog.
  useEffect(() => { tryInitHero() }, [tryInitHero])

  const [mainSearchFloating, setMainSearchFloating] = useState(false)
  const mainSearchRef = useRef<HTMLDivElement>(null)
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const savedScrollRef = useRef(0)

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
    tryInitHero()
  }, [tryInitHero])
  const dragSrcRef = useRef<string | null>(null)
  const lastRowFocusIdx = useRef<Map<string, number>>(new Map())

  useEffect(() => {
    function onFocusIn(e: FocusEvent) {
      const el = e.target as HTMLElement
      if (!el.hasAttribute('data-card')) return
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

  useEffect(() => {
    const savedSearch = sessionStorage.getItem(SS_SEARCH) || ''
    if (savedSearch) {
      setSearchValue(savedSearch)
      if (savedSearch.length >= 3) setSearchQuery(savedSearch)
    }
  }, [])

  // Show floating search bar when the toolbar search scrolls above the nav (52px)
  useEffect(() => {
    function check() {
      if (expandedCategory) return
      const el = mainSearchRef.current
      if (!el) return
      setMainSearchFloating(el.getBoundingClientRect().bottom < 52)
    }
    window.addEventListener('scroll', check, { passive: true })
    return () => window.removeEventListener('scroll', check)
  }, [expandedCategory])

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
      sessionStorage.removeItem(SS_SEARCH)
      setSearchQuery('')
      setSearchResults(null)
      return
    }
    searchTimerRef.current = setTimeout(() => {
      const q = value.trim()
      if (q.length >= 3) {
        sessionStorage.setItem(SS_SEARCH, q)
        setSearchQuery(q)
      } else {
        sessionStorage.removeItem(SS_SEARCH)
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

      if (expandedCategory) {
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
          else if (e.key === 'ArrowUp') next = Math.max(idx - cols, 0)
        }
        if (next !== -1 && next !== idx) {
          cards[next].focus()
          scrollV(cards[next])
        }
        return
      }

      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        if (!focused?.hasAttribute('data-card')) {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            const first = document.querySelector<HTMLElement>('[data-card]')
            first?.focus()
            if (first) scrollV(first)
          }
          return
        }

        e.preventDefault()
        const rowInner = focused.closest('[data-row-id]') as HTMLElement | null
        if (!rowInner) return
        const allRows = Array.from(document.querySelectorAll<HTMLElement>('[data-row-id]'))
        const rowIdx = allRows.indexOf(rowInner)
        const targetRowIdx = e.key === 'ArrowDown' ? rowIdx + 1 : rowIdx - 1
        if (targetRowIdx < 0) {
          mainSearchRef.current?.querySelector('input')?.focus()
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
        target?.focus()
        if (target) { scrollH(target); scrollV(target) }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [expandedCategory, navigate])

  useEffect(() => {
    const onCatalogBack = () => { if (expandedCategory) handleBack() }
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
    function focusMainSearch() {
      mainSearchRef.current?.querySelector('input')?.focus()
    }
    if (takePendingFocusCatalogSearch()) focusMainSearch()
    window.addEventListener('catalog:focus-search', focusMainSearch)
    return () => window.removeEventListener('catalog:focus-search', focusMainSearch)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const stateName = (location.state as { catName?: string } | null)?.catName ?? null
  const expandedCat = categories.find(c => c.id === expandedCategory) ?? (
    expandedCategory && (expandedCategory.startsWith('actor_') || expandedCategory.startsWith('director_'))
      ? { id: expandedCategory, name: stateName ?? expandedCategory }
      : null
  )

  const showSearch = searchQuery.length >= 3 && !expandedCategory

  return (
    <Layout>
      <div className={styles.page}>
        {!expandedCat && (
          <div className={styles.toolbar}>
            <div className={styles.toolbarTop}>
              {hasCustomOrder && (
                <button className={styles.resetOrderBtn} onClick={resetRowOrder} title="Вернуть порядок по умолчанию">
                  Сбросить порядок
                </button>
              )}
              <div ref={mainSearchRef} className={styles.searchWrap}>
                <input
                  className={styles.searchInput}
                  placeholder="Поиск…"
                  value={searchValue}
                  onChange={e => handleSearchChange(e.target.value)}
                  onKeyDown={e => {
                    // Part of the site-wide keyboard-nav rollout: search sits
                    // between the top menu and the category grid — Up/Down
                    // move to whichever one is above/below it.
                    if (e.key === 'ArrowDown') {
                      e.preventDefault()
                      const first = document.querySelector<HTMLElement>('[data-card]')
                      first?.focus()
                      if (first) scrollV(first)
                    } else if (e.key === 'ArrowUp') {
                      e.preventDefault()
                      const topLink =
                        document.querySelector<HTMLElement>('[data-top-nav] a[aria-current="page"]') ??
                        document.querySelector<HTMLElement>('[data-top-nav] a')
                      topLink?.focus()
                    }
                  }}
                />
                {searchValue && (
                  <button className={styles.searchClear} onClick={() => handleSearchChange('')} title="Очистить">✕</button>
                )}
              </div>
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

        {!expandedCategory && !showSearch && (
          <div className={styles.rows}>
            {categories.map(cat => (
              <CategoryRow
                key={`${cat.id}_${token}_${profileId}`}
                category={cat}
                token={token}
                profileId={profileId}
                onExpandCategory={handleExpandCategory}
                onCardClick={handleCardClick}
                onActivate={layout === 'hero' ? hero.activate : undefined}
                activeCardId={layout === 'hero' && hero.item ? `${hero.item.id}_${hero.item.media_type}` : null}
                dragHandlers={{ onDragStart, onDragEnd, onDragOver, onDrop }}
                initialCache={_cache.rows[cat.id]}
                onItemsLoaded={handleItemsLoaded}
              />
            ))}
          </div>
        )}
        {!expandedCategory && mainSearchFloating && (
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
    </Layout>
  )
}
