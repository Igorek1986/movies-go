import { useEffect, useLayoutEffect, useRef, useState, useCallback } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, scrollH, getGridCols, CAROUSEL_TRANSITION_MS } from '@/utils/scrollNav'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { getEffectiveBrowseLayout } from '@/utils/browseLayout'
import { BrowseHero, useHeroPreview } from '@/components/BrowseHero'
import styles from './MediaLibraryPage.module.scss'

interface LibraryItem {
  id: number
  media_type: string
  title: string
  name: string
  poster_path: string | null
  release_date: string
  first_air_date: string
}

// toMediaItem (Go) doesn't return card_id — every other catalog page builds it
// client-side from tmdb id + media_type, same convention here.
function cardIdOf(item: LibraryItem): string {
  return `${item.id}_${item.media_type}`
}

interface LibraryResponse {
  page: number
  total_pages: number
  total_results: number
  results: LibraryItem[]
}

type StatusKey = 'favorite' | 'continues' | 'watching' | 'completed' | 'planned' | 'stopped'

interface RowCache { items: LibraryItem[]; totalPages: number }

// Module-level, like CatalogPage's _cache — MediaLibraryPage remounts
// wholesale on every navigation (see the `key={location.key}` below), so
// without this, flipping back to an already-visited status in the carousel
// would always show "Загрузка…" and refetch instead of redisplaying instantly.
const _rowCache: Partial<Record<StatusKey, RowCache>> = {}
// Profile the cache belongs to — cleared on switch (see CatalogPage's
// _cache.profileKey for the same reasoning).
let _libProfileKey: string | null = null

// «Продолжить просмотр» — не subjective_statuses, а прогресс по таймкодам (карточки
// с прогрессом ниже watched_threshold), отдаётся отдельным эндпоинтом /continues
// (см. handleContinues в internal/api/content.go), не /media-library.
function libraryUrl(status: StatusKey, params: { token: string; profile_id: string; page: string; per_page: string }): string {
  if (status === 'continues') {
    const { token, profile_id, page, per_page } = params
    return `/continues?${new URLSearchParams({ token, profile_id, page, per_page })}`
  }
  return `/media-library?${new URLSearchParams({ ...params, status })}`
}

function itemYear(item: LibraryItem): string {
  return (item.release_date || item.first_air_date || '').slice(0, 4)
}

function Card({ item, onClick, onActivate, isHeroActive, compact }: {
  item: LibraryItem; onClick: () => void; onActivate?: () => void; isHeroActive?: boolean
  // Hero carousel: title/year are already shown in BrowseHero for the
  // focused card — Classic layout (no hero panel) keeps them.
  compact?: boolean
}) {
  const url = posterUrl(item.poster_path)
  const title = item.title || item.name
  return (
    <div
      className={`${styles.card}${isHeroActive ? ' ' + styles.cardHeroActive : ''}`}
      tabIndex={0}
      data-card
      onClick={onClick}
      onKeyDown={e => { if (e.key === 'Enter') onClick() }}
      onFocus={onActivate}
    >
      {url
        ? <img className={styles.poster} src={url} alt={title} loading="lazy" />
        : <div className={styles.posterPlaceholder}>Нет постера</div>
      }
      {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
      {!compact && (
        <div className={styles.cardBody}>
          <p className={styles.cardTitle}>{title}</p>
          <span className={styles.cardYear}>{itemYear(item)}</span>
        </div>
      )}
    </div>
  )
}

// ── Row: lazy-loaded on scroll into view, horizontal, "Все →" to expand ────────

function LibraryRow({ status, label, token, profileId, onExpand, onCardClick, onActivate, activeCardId, initialCache, onItemsLoaded, onEmpty, autoFocusIdx, hideHeader }: {
  status: StatusKey; label: string; token: string; profileId: string
  onExpand: (status: StatusKey) => void; onCardClick: (item: LibraryItem) => void
  onActivate?: (item: LibraryItem) => void
  activeCardId?: string | null
  initialCache?: RowCache
  onItemsLoaded?: (status: StatusKey, cache: RowCache) => void
  // Carousel mode (see CatalogPage's CategoryRow for the same pattern):
  // status turned out empty → advance; focus this card index once loaded.
  // Both undefined/unused in Classic layout.
  onEmpty?: () => void
  autoFocusIdx?: number
  // Carousel mode: the parent renders its own persistent title instead — see
  // CatalogPage's CategoryRow for why (avoids the title flickering in/out on
  // every auto-skip of an empty status).
  hideHeader?: boolean
}) {
  const [items, setItems] = useState<LibraryItem[] | null>(initialCache?.items ?? null)
  const [totalPages, setTotalPages] = useState(initialCache?.totalPages ?? 1)
  const rowRef = useRef<HTMLDivElement>(null)
  const rowInnerRef = useRef<HTMLDivElement>(null)
  const loadedRef = useRef(!!initialCache)
  const autoFocusAppliedRef = useRef(false)

  const loadItems = useCallback(() => {
    if (loadedRef.current || !token) return
    loadedRef.current = true
    fetch(libraryUrl(status, { token, profile_id: profileId, page: '1', per_page: '20' }))
      .then(r => r.ok ? r.json() : Promise.reject())
      .then((data: LibraryResponse) => {
        const results = data.results || []
        const tp = data.total_pages || 1
        setItems(results)
        setTotalPages(tp)
        onItemsLoaded?.(status, { items: results, totalPages: tp })
      })
      .catch(() => setItems([]))
  }, [status, token, profileId, onItemsLoaded])

  // Profile (or device) switch — the row already fired its one-shot fetch under the
  // old identity, so without this the loadedRef latch would keep it stuck showing
  // the previous profile's data forever.
  //
  // Guarded against firing on mount: a deps-effect always runs on the first
  // commit too, so this used to wipe every freshly mounted row's initialCache
  // back to null and refetch it — on every single carousel switch, not just an
  // actual profile change. That threw away the cache (a pointless round trip
  // each switch) and, worse, tore the just-focused card out of the DOM a frame
  // after the row's auto-focus had already latched itself as done, stranding
  // keyboard focus on <body>. CatalogPage never hit this: it has no such
  // effect, keying its rows on `${id}_${token}_${profileId}` instead so a
  // profile change remounts them outright.
  const rowIdentityRef = useRef(`${token}|${profileId}`)
  useEffect(() => {
    const identity = `${token}|${profileId}`
    if (rowIdentityRef.current === identity) return
    rowIdentityRef.current = identity
    loadedRef.current = false
    setItems(null)
    setTotalPages(1)
  }, [token, profileId])

  useEffect(() => {
    const el = rowRef.current
    if (!el) return
    const observer = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting) {
        observer.disconnect()
        loadItems()
      }
    }, { rootMargin: '300px' })
    observer.observe(el)
    return () => observer.disconnect()
  }, [loadItems])

  useEffect(() => {
    if (items !== null && items.length === 0) onEmpty?.()
  }, [items, onEmpty])

  useEffect(() => {
    // Back to loading (a real profile switch wipes the row) — re-arm, so the
    // restore runs again once the items return instead of being skipped by a
    // latch left over from before the wipe.
    if (items === null) { autoFocusAppliedRef.current = false; return }
    if (autoFocusIdx === undefined || autoFocusAppliedRef.current || !items.length) return
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
      // Don't steal focus from an active text input — see CatalogPage's
      // identical guard for why (the header search icon's floating bar
      // otherwise closes itself the instant it opens).
      const activeTag = document.activeElement?.tagName?.toLowerCase()
      if (activeTag === 'input' || activeTag === 'textarea') return
      const cards = rowInnerRef.current?.querySelectorAll<HTMLElement>('[data-card]')
      if (!cards?.length) return
      const target = cards[Math.min(idx, cards.length - 1)]
      target?.focus({ preventScroll: true })
      // See CatalogPage's identical CategoryRow effect — restoring focus
      // deep into the row (e.g. back from an expanded status via Backspace)
      // needs the row scrolled to it too, instantly (the row should already
      // be showing the right thing on arrival, not visibly scroll to it).
      if (target) scrollH(target, true)
    })
  }, [items, autoFocusIdx, onActivate])

  if (items !== null && items.length === 0) return null

  const hasMore = totalPages > 1

  return (
    <section ref={rowRef} className={styles.row}>
      {!hideHeader && (
      <div className={styles.rowHeader}>
        <h3 className={styles.rowTitle}>{label}</h3>
        {hasMore && (
          <button className={styles.rowMore} onClick={() => onExpand(status)}>Все →</button>
        )}
      </div>
      )}
      <div className={`${styles.rowScroll}${hideHeader ? ' ' + styles.rowScrollCompact : ''}`} data-row-scroll>
        <div
          ref={rowInnerRef}
          className={styles.rowInner}
          data-row-id={status}
          onKeyDown={e => {
            if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
            const cards = Array.from((e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[data-card]'))
            const idx = cards.indexOf(document.activeElement as HTMLElement)
            if (idx === -1) return
            e.preventDefault()
            if (e.key === 'ArrowRight') {
              if (idx === cards.length - 1) {
                // ArrowRight past the last real card opens the status
                // directly — see CatalogPage's identical CategoryRow.
                if (hasMore) onExpand(status)
              } else {
                const next = cards[idx + 1]
                // preventScroll: focusing an off-screen element natively
                // jumps it into view instantly, before our own smooth
                // scrollH/scrollV runs — without this every keyboard move
                // looked like a jump immediately followed by a correction.
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
          {items === null && <div className={styles.rowLoading}>Загрузка…</div>}
          {items?.map(item => (
            <div key={cardIdOf(item)} className={styles.rowCard}>
              <Card
                item={item}
                onClick={() => onCardClick(item)}
                onActivate={onActivate ? () => onActivate(item) : undefined}
                isHeroActive={activeCardId === cardIdOf(item)}
                compact={hideHeader}
              />
            </div>
          ))}
          {items !== null && hasMore && (
            <div className={styles.rowCard} key="expand-btn">
              <button className={styles.rowExpandBtn} onClick={() => onExpand(status)} tabIndex={-1}>Все →</button>
            </div>
          )}
        </div>
      </div>
    </section>
  )
}

// ── Expanded: full paginated grid for one status, infinite scroll ──────────────

function LibraryGrid({ status, token, profileId, onCardClick }: {
  status: StatusKey; token: string; profileId: string; onCardClick: (item: LibraryItem) => void
}) {
  const [items, setItems] = useState<LibraryItem[]>([])
  const [page, setPage] = useState(1)
  const [totalPages, setTotalPages] = useState(1)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const loadingRef = useRef(false)

  const load = useCallback((pg: number, reset: boolean) => {
    if (!token || loadingRef.current) return
    loadingRef.current = true
    setLoading(true)
    setError(false)
    fetch(libraryUrl(status, { token, profile_id: profileId, page: String(pg), per_page: '24' }))
      .then(r => r.ok ? r.json() : Promise.reject())
      .then((data: LibraryResponse) => {
        setItems(prev => reset ? (data.results || []) : [...prev, ...(data.results || [])])
        setTotalPages(data.total_pages || 1)
        setPage(pg)
      })
      .catch(() => setError(true))
      .finally(() => { loadingRef.current = false; setLoading(false) })
  }, [status, token, profileId])

  useEffect(() => { load(1, true) }, [load])

  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && !loadingRef.current && page < totalPages) load(page + 1, false)
    }, { rootMargin: '400px' })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [page, totalPages, load])

  return (
    <>
      {error && <p className={styles.errorMsg}>Не удалось загрузить</p>}
      {items.length > 0 && (
        <div className={styles.grid}>
          {items.map(item => <Card key={cardIdOf(item)} item={item} onClick={() => onCardClick(item)} />)}
        </div>
      )}
      {loading && items.length === 0 && <p className={styles.emptyMsg}>Загрузка…</p>}
      {!loading && items.length === 0 && !error && <p className={styles.emptyMsg}>Пока пусто</p>}
      {loading && items.length > 0 && <p className={styles.loadingMore}>Загрузка…</p>}
      <div ref={sentinelRef} className={styles.sentinel} />
    </>
  )
}

const STATUS_LABELS: Record<StatusKey, string> = {
  favorite: 'Избранное',
  continues: 'Продолжить просмотр',
  planned: 'Буду смотреть',
  watching: 'Смотрю',
  completed: 'Просмотрел',
  stopped: 'Брошено',
}
const ROW_ORDER: StatusKey[] = ['favorite', 'continues', 'planned', 'watching', 'completed', 'stopped']

export default function MediaLibraryPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { activeDevice, activeProfile, loaded } = useActiveProfile()

  const [expanded, setExpanded] = useState<StatusKey | null>(null)
  const lastRowFocusIdx = useRef<Map<string, number>>(new Map())

  // Search — same floating-bar model as CatalogPage, opened by the header's
  // search icon (see Layout.tsx's handleBottomSearch). Searches across the
  // whole library at once (favorite/watching/completed/stopped/continues via
  // /api/media-library/search — see its comment for why "planned" is
  // excluded), not just whichever status row is currently showing.
  const [searchValue, setSearchValue] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<LibraryItem[] | null>(null)
  const [searchLoading, setSearchLoading] = useState(false)
  const [searchHasMore, setSearchHasMore] = useState(false)
  const searchSentinelRef = useRef<HTMLDivElement>(null)
  const searchPageRef = useRef(1)
  const [searchOpen, setSearchOpen] = useState(false)
  const floatingInputRef = useRef<HTMLInputElement>(null)
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // See CatalogPage's identical field: whether a card in the grid currently
  // has keyboard focus, so the hero border (.cardHeroActive) disappears once
  // focus leaves for the top menu instead of staying stuck on the last card.
  const [cardGridFocused, setCardGridFocused] = useState(false)

  // Per-device (localStorage) — see BrowseLayoutSettings on /profiles.
  // Forced to Classic on touch devices regardless of the saved preference —
  // the hero carousel is driven by keyboard/mouse focus, which touch has no
  // equivalent for.
  const [layout] = useState(() => getEffectiveBrowseLayout())
  const hero = useHeroPreview<LibraryItem>()
  // Also marks the grid as focused here, not just via the onFocusIn listener
  // below — the row's own mount-time auto-focus effect calls this directly
  // to guarantee the hero banner populates even when the real .focus() call
  // it also makes doesn't stick (e.g. a mouse click on the nav link keeping
  // real DOM focus there); without this the banner would show correctly but
  // the card itself would never get its .cardHeroActive border.
  const handleActivate = useCallback((item: LibraryItem) => {
    hero.activate(item)
    setCardGridFocused(true)
  }, [hero.activate])

  // Hero layout: non-scrolling carousel, same model as CatalogPage — exactly
  // one status's row mounted at a time, pinned to the bottom via CSS, never
  // scrolled. ArrowUp/Down (or a swipe) swaps which status that is.
  const [activeStatusIndex, setActiveStatusIndex] = useState(0)
  // Drum-carousel row switch — see CatalogPage's identical `transition`
  // state for how the two-layer slide works.
  const [transition, setTransition] = useState<{ prevIndex: number; dir: 1 | -1 } | null>(null)
  const transitionTimerRef = useRef<number | null>(null)
  useEffect(() => () => { if (transitionTimerRef.current) window.clearTimeout(transitionTimerRef.current) }, [])
  // Which way we were actually headed when an empty status was hit — an
  // empty status reached via ArrowUp must keep skipping backward, not
  // forward: hard-coding +1 here used to override the real direction, so
  // going up onto a run of empty statuses (common in Моё — most people have
  // nothing in several of these) silently reversed course, which is what
  // read as focus "getting lost". Defaults to 1 for skipping past empty
  // leading statuses on first load, before any real switch has happened.
  const lastDirRef = useRef<1 | -1>(1)
  // Statuses that reported themselves empty, so the skip below can jump the
  // whole run of them at once instead of clamping onto one.
  const emptyStatusesRef = useRef<Set<StatusKey>>(new Set())

  const handleEmptyStatus = useCallback(() => {
    if (transitionTimerRef.current) { window.clearTimeout(transitionTimerRef.current); transitionTimerRef.current = null }
    setTransition(null)
    setActiveStatusIndex(idx => {
      emptyStatusesRef.current.add(ROW_ORDER[idx])
      // Walk to the first status not already known to be empty. Clamping to
      // the range end instead (the old Math.max/Math.min) landed right back
      // on this same empty status, which renders null — so the carousel sat
      // on a row with no cards in it at all and there was physically nothing
      // for focus to be on. Most statuses here are empty for most people
      // (unlike Catalog's categories), which is why only Моё showed this.
      const scan = (from: number, step: number) => {
        for (let i = from; i >= 0 && i < ROW_ORDER.length; i += step) {
          if (!emptyStatusesRef.current.has(ROW_ORDER[i])) return i
        }
        return -1
      }
      const dir = lastDirRef.current
      // Nothing left the way we were headed (ran off the end of a trailing
      // run of empty statuses) — fall back to the opposite direction rather
      // than stranding the carousel on an empty row.
      const ahead = scan(idx + dir, dir)
      if (ahead !== -1) return ahead
      const back = scan(idx - dir, -dir)
      return back !== -1 ? back : idx
    })
  }, [])

  const handleItemsLoaded = useCallback((status: StatusKey, cache: RowCache) => {
    _rowCache[status] = cache
  }, [])

  const switchStatus = useCallback((dir: 1 | -1) => {
    lastDirRef.current = dir
    setActiveStatusIndex(idx => {
      const next = idx + dir
      if (next < 0 || next >= ROW_ORDER.length) return idx
      setTransition({ prevIndex: idx, dir })
      if (transitionTimerRef.current) window.clearTimeout(transitionTimerRef.current)
      transitionTimerRef.current = window.setTimeout(() => {
        transitionTimerRef.current = null
        setTransition(null)
      }, CAROUSEL_TRANSITION_MS)
      return next
    })
  }, [])

  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''

  // Drop the cross-remount row cache when the active profile actually
  // changes — same reasoning as CatalogPage's _cache.profileKey check, run
  // synchronously during render for the same reason (LibraryRow reads
  // _rowCache[status] as initialCache in this same pass).
  const profileKey = activeProfile ? `${activeProfile.device_id}:${activeProfile.profile_id}` : null
  if (_libProfileKey !== null && _libProfileKey !== profileKey) {
    for (const k of Object.keys(_rowCache) as StatusKey[]) delete _rowCache[k]
  }
  _libProfileKey = profileKey

  function openCard(item: LibraryItem) {
    navigate(`/card/${cardIdOf(item)}`, { state: { backUrl: '/media-library' } })
  }

  // Layout.tsx's nav doesn't know about this page's local search state —
  // publish it as a body class instead, same idea as CatalogPage's identical
  // effect (Layout.module.scss mutes the current page's nav-link highlight
  // off this, and it's generic across pages, not Catalog-specific).
  useEffect(() => {
    document.body.classList.toggle('search-mode-active', searchQuery.length >= 3 && !expanded)
    return () => { document.body.classList.remove('search-mode-active') }
  }, [searchQuery, expanded])

  const loadSearchPage = useCallback((query: string, page: number, reset: boolean) => {
    setSearchLoading(true)
    fetch(`/api/media-library/search?token=${encodeURIComponent(token)}&profile_id=${encodeURIComponent(profileId)}&search=${encodeURIComponent(query)}&page=${page}`)
      .then(r => r.ok ? r.json() : { results: [], total_pages: 1 })
      .then((data: LibraryResponse) => {
        const rows = data.results || []
        setSearchResults(prev => reset ? rows : [...(prev ?? []), ...rows])
        setSearchHasMore((data.total_pages ?? 1) > page)
        searchPageRef.current = page
        setSearchLoading(false)
      })
      .catch(() => {
        if (reset) setSearchResults([])
        setSearchLoading(false)
      })
  }, [token, profileId])

  useEffect(() => {
    if (searchQuery.length < 3 || expanded) {
      setSearchResults(null)
      setSearchHasMore(false)
      return
    }
    loadSearchPage(searchQuery, 1, true)
  }, [searchQuery, expanded, loadSearchPage])

  // Infinite scroll for search results.
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

  // Clicking "Моё" while already on it — that click doesn't remount this
  // page, so nothing else resets search state for you.
  useEffect(() => {
    const onBack = () => {
      if (expanded) setExpanded(null)
      setSearchValue('')
      setSearchQuery('')
      setSearchOpen(false)
    }
    window.addEventListener('media-library:back', onBack)
    return () => window.removeEventListener('media-library:back', onBack)
  }, [expanded])

  // Header search icon (Layout.tsx's handleBottomSearch) — closes an
  // expanded status (search results can't show underneath one — see
  // showSearch's `!expanded` guard) without touching an already-in-progress
  // query, unlike media-library:back above.
  useEffect(() => {
    const onCloseExpanded = () => { if (expanded) setExpanded(null) }
    window.addEventListener('media-library:close-expanded', onCloseExpanded)
    return () => window.removeEventListener('media-library:close-expanded', onCloseExpanded)
  }, [expanded])

  useEffect(() => {
    function openSearch() { setSearchOpen(true) }
    window.addEventListener('media-library:focus-search', openSearch)
    return () => window.removeEventListener('media-library:focus-search', openSearch)
  }, [])

  useLayoutEffect(() => {
    if (searchOpen) floatingInputRef.current?.focus()
  }, [searchOpen])

  // Track last-focused card index per row, to restore position when navigating back to it.
  // Also tracks whether a card currently has focus at all — see
  // cardGridFocused below (mirrors CatalogPage's identical fix).
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

  // Same keyboard model as CatalogPage: arrows navigate within/between rows when
  // collapsed, and within the grid (with column-aware Up/Down) when expanded;
  // Backspace goes back a level.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const focused = document.activeElement as HTMLElement | null
      const tag = focused?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'textarea' || tag === 'select') return

      if ((e.key === 'Backspace' || (e.key === 'ArrowLeft' && e.altKey)) && expanded) {
        setExpanded(null)
        return
      }
      if (e.key === 'Backspace' && !expanded) {
        navigate(-1)
        return
      }

      // Escape while browsing search results (not just from the input
      // itself, which the floating bar's own onKeyDown already covers) —
      // clears the query and drops back to normal browsing.
      if (e.key === 'Escape' && !expanded && searchQuery.length >= 3) {
        e.preventDefault()
        setSearchValue('')
        setSearchQuery('')
        setSearchOpen(false)
        document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
        return
      }

      // Search results render the same flat .grid of cards as an expanded
      // status (not the row-carousel structure) — share the same grid
      // navigation instead of falling through to the row-based logic below.
      if (expanded || searchQuery.length >= 3) {
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
            // status, which has no search box to return to).
            if (!expanded && idx - cols < 0) {
              document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
              return
            }
            next = Math.max(idx - cols, 0)
          }
        }
        if (next !== -1 && next !== idx) {
          // preventScroll — see LibraryRow's identical fix above for why.
          cards[next].focus({ preventScroll: true })
          scrollV(cards[next])
        }
        return
      }

      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        if (!focused?.hasAttribute('data-card')) {
          // ArrowDown steps into the grid from anywhere (including the top
          // nav). ArrowUp only recovers when focus was genuinely lost — it
          // has to, or the page is stuck: with focus back on <body> nothing
          // handles the arrows and Left/Right fall through to the nav panel
          // instead of walking the row. But it must not fire while focus is
          // legitimately up in the nav, or Up would yank you back down.
          if (e.key === 'ArrowUp' && focused && focused !== document.body) return
          e.preventDefault()
          // Mid-switch the outgoing row is still mounted and comes first in
          // the DOM, so take the last row — always the incoming/active one —
          // rather than recovering onto a row that's on its way out.
          const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-row-id]'))
          const row = layout === 'hero' ? rows[rows.length - 1] : rows[0]
          const cards = Array.from(row?.querySelectorAll<HTMLElement>('[data-card]') ?? [])
          const savedIdx = row ? lastRowFocusIdx.current.get(row.dataset.rowId!) ?? 0 : 0
          const target = cards[Math.min(savedIdx, cards.length - 1)]
          target?.focus({ preventScroll: true })
          if (target) {
            scrollH(target, true)
            // Hero carousel: the active row is always already pinned to the
            // bottom via CSS, nothing to scroll into view.
            if (layout !== 'hero') scrollV(target)
          }
          return
        }

        e.preventDefault()

        if (layout === 'hero') {
          const dir = e.key === 'ArrowDown' ? 1 : -1
          if (dir < 0 && activeStatusIndex === 0) {
            // Same as CatalogPage: the search icon is the first item in the
            // top nav — bridge straight to it, not the active page link.
            document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
            return
          }
          switchStatus(dir)
          return
        }

        // Classic: scrollable list of every row — walk the DOM to find
        // whichever one is next/previous and focus into it.
        const rowInner = focused.closest('[data-row-id]') as HTMLElement | null
        if (!rowInner) return
        const allRows = Array.from(document.querySelectorAll<HTMLElement>('[data-row-id]'))
        const rowIdx = allRows.indexOf(rowInner)
        const targetRowIdx = e.key === 'ArrowDown' ? rowIdx + 1 : rowIdx - 1
        if (targetRowIdx < 0 || targetRowIdx >= allRows.length) return
        const targetRow = allRows[targetRowIdx]
        const targetRowId = targetRow.dataset.rowId!
        const savedIdx = lastRowFocusIdx.current.get(targetRowId) ?? 0
        const targetCards = Array.from(targetRow.querySelectorAll<HTMLElement>('[data-card]'))
        if (!targetCards.length) return
        const target = targetCards[Math.min(savedIdx, targetCards.length - 1)]
        target?.focus({ preventScroll: true })
        if (target) { scrollH(target); scrollV(target) }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [expanded, searchQuery, navigate, layout, activeStatusIndex, switchStatus])

  const showSearch = searchQuery.length >= 3 && !expanded
  // Hero carousel locks the page to the viewport (no scroll) — only while
  // actually showing the carousel itself, not the expanded status grid or
  // search results, which stay normal scrollable views.
  const carouselActive = layout === 'hero' && !expanded && !showSearch
  const activeStatus = ROW_ORDER[activeStatusIndex]

  return (
    <Layout>
      {/* location.key is unique per navigation — remounts everything below on
          every visit to this page, so a status changed elsewhere (card detail
          page) is never shown stale without a hard refresh. */}
      <div className={`${styles.page}${carouselActive ? ' ' + styles.pageLocked : ''}`} key={location.key}>
        <div className={styles.header}>
          {expanded
            ? <button className={styles.backBtn} onClick={() => setExpanded(null)}>← Назад</button>
            : <h1 className={styles.title}>Моё</h1>
          }
        </div>

        {!loaded ? null : expanded ? (
          <>
            <h2 className={styles.expandedTitle}>{STATUS_LABELS[expanded]}</h2>
            <LibraryGrid status={expanded} token={token} profileId={profileId} onCardClick={openCard} />
          </>
        ) : showSearch ? (
          <div>
            {searchResults !== null && searchResults.length === 0 && !searchLoading && (
              <div className={styles.emptyMsg}>Ничего не найдено</div>
            )}
            {searchResults !== null && searchResults.length > 0 && (
              <div className={styles.grid}>
                {searchResults.map(item => (
                  <Card key={cardIdOf(item)} item={item} onClick={() => openCard(item)} />
                ))}
              </div>
            )}
            {searchLoading && <div className={styles.loadingMore}>Поиск…</div>}
            <div ref={searchSentinelRef} className={styles.sentinel} />
          </div>
        ) : (
          <>
            {layout === 'hero' && (
              <BrowseHero
                item={hero.item}
                detail={hero.detail}
                onOpen={() => hero.item && openCard(hero.item)}
              />
            )}

            {carouselActive && activeStatus && (
              <div className={styles.carouselRail}>
                {/* Owned by the parent — see CatalogPage's CategoryRow for
                    why (avoids the title flickering on every auto-skip).
                    Dimmer neighbor labels above/below hint that ArrowUp/Down
                    reaches more statuses. */}
                <div className={styles.categoryTitleStack}>
                  <span className={styles.categoryTitleNeighbor}>{STATUS_LABELS[ROW_ORDER[activeStatusIndex - 1]] ?? ' '}</span>
                  <h3 className={styles.rowTitle}>{STATUS_LABELS[activeStatus]}</h3>
                  <span className={styles.categoryTitleNeighbor}>{STATUS_LABELS[ROW_ORDER[activeStatusIndex + 1]] ?? ' '}</span>
                </div>
                <div className={styles.carouselViewport}>
                  {/* Outgoing row — same key it had before the switch, so it
                      keeps reusing its already-fetched instance instead of
                      remounting/refetching just to animate away. */}
                  {transition && ROW_ORDER[transition.prevIndex] && (
                    <div className={`${styles.carouselLayerOut} ${transition.dir > 0 ? styles.carouselOutToTop : styles.carouselOutToBottom}`}>
                      <LibraryRow
                        key={ROW_ORDER[transition.prevIndex]}
                        status={ROW_ORDER[transition.prevIndex]}
                        label={STATUS_LABELS[ROW_ORDER[transition.prevIndex]]}
                        token={token}
                        profileId={profileId}
                        onExpand={setExpanded}
                        onCardClick={openCard}
                        initialCache={_rowCache[ROW_ORDER[transition.prevIndex]]}
                        onItemsLoaded={handleItemsLoaded}
                        hideHeader
                      />
                    </div>
                  )}
                  <div className={transition ? (transition.dir > 0 ? styles.carouselInFromBottom : styles.carouselInFromTop) : undefined}>
                    <LibraryRow
                      key={activeStatus}
                      status={activeStatus}
                      label={STATUS_LABELS[activeStatus]}
                      token={token}
                      profileId={profileId}
                      onExpand={setExpanded}
                      onCardClick={openCard}
                      onActivate={handleActivate}
                      activeCardId={cardGridFocused && hero.item ? cardIdOf(hero.item) : null}
                      initialCache={_rowCache[activeStatus]}
                      onItemsLoaded={handleItemsLoaded}
                      onEmpty={handleEmptyStatus}
                      autoFocusIdx={lastRowFocusIdx.current.get(activeStatus) ?? 0}
                      hideHeader
                    />
                  </div>
                </div>
              </div>
            )}

            {layout === 'classic' && (
              <div className={styles.rows}>
                {ROW_ORDER.map(status => (
                  <LibraryRow
                    key={status}
                    status={status}
                    label={STATUS_LABELS[status]}
                    token={token}
                    profileId={profileId}
                    onExpand={setExpanded}
                    onCardClick={openCard}
                    initialCache={_rowCache[status]}
                    onItemsLoaded={handleItemsLoaded}
                  />
                ))}
              </div>
            )}
          </>
        )}

        {!expanded && searchOpen && (
          <div
            className={styles.floatingBar}
            onBlur={e => {
              // Closes as soon as focus actually leaves the bar (Escape, the
              // ✕ button, or ArrowUp bridging back to the search icon below
              // all move focus elsewhere and land here).
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
                    // stopPropagation is required — without it the same event
                    // also reaches the window-level grid-nav listener above
                    // (search results share its Left/Right/Up/Down handling),
                    // which would then move focus again from wherever this
                    // just landed.
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
