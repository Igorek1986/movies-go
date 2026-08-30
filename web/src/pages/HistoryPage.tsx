import { useEffect, useState, useRef, useLayoutEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, getGridCols, NAV_H } from '@/utils/scrollNav'
import { useActiveProfile, ACTIVE_PROFILE_STORAGE_KEY, type Device, type Profile } from '@/contexts/ActiveProfileContext'
import styles from './HistoryPage.module.scss'

interface HistoryItem {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  poster_path: string | null
  year: string
  last_watched: string
  max_percent: number
  progress: number
  watched_items: number
  total_episodes: number
  is_complete: boolean
}

interface HistoryCounts {
  all: number
  movies: number
  tv: number
  in_progress: number
}

interface HistoryResponse {
  page: number
  total_pages: number
  total_results: number
  counts: HistoryCounts
  results: HistoryItem[]
}

const SORT_OPTIONS = [
  { value: 'watched',       label: 'По дате просмотра' },
  { value: 'release',       label: 'По дате выхода' },
  { value: 'progress_asc',  label: 'По прогрессу ↑' },
  { value: 'progress_desc', label: 'По прогрессу ↓' },
]

const FILTER_KEY  = 'history_filter'

function loadSavedFilter(): { mediaType: string; inProgress: boolean; sort: string } {
  try {
    const s = localStorage.getItem(FILTER_KEY)
    if (s) return JSON.parse(s)
  } catch {}
  return { mediaType: '', inProgress: false, sort: 'watched' }
}

function saveFilter(mediaType: string, inProgress: boolean, sort: string) {
  localStorage.setItem(FILTER_KEY, JSON.stringify({ mediaType, inProgress, sort }))
}

interface HistCache {
  filterKey: string
  mediaType: string
  inProgress: boolean
  sort: string
  search: string
  items: HistoryItem[]
  totalPages: number
  counts: HistoryCounts | null
  page: number
  scrollY: number
}

let _histCache: HistCache | null = null

function buildFilterKey(devId: number | undefined, profId: string | undefined, mt: string, ip: boolean, st: string, sr: string): string {
  return [devId, profId, mt, ip ? '1' : '0', st, sr].join('|')
}

// Returns cache if device+profile match (any filter) — called synchronously in useState initialisers
function getInitCacheIfValid(): HistCache | null {
  if (!_histCache) return null
  try {
    const key = JSON.parse(localStorage.getItem(ACTIVE_PROFILE_STORAGE_KEY) || 'null')
    const prefix = [key?.device_id, key?.profile_id].join('|') + '|'
    return _histCache.filterKey.startsWith(prefix) ? _histCache : null
  } catch { return null }
}

export default function HistoryPage() {
  const navigate = useNavigate()
  const { activeDevice, activeProfile, loaded } = useActiveProfile()

  const [items, setItems]         = useState<HistoryItem[]>(() => getInitCacheIfValid()?.items ?? [])
  const [counts, setCounts]       = useState<HistoryCounts | null>(() => getInitCacheIfValid()?.counts ?? null)
  const [totalPages, setTotalPages] = useState(() => getInitCacheIfValid()?.totalPages ?? 1)
  const [loading, setLoading]     = useState(false)

  const [mediaType,   setMediaType]   = useState(() => getInitCacheIfValid()?.mediaType   ?? loadSavedFilter().mediaType)
  const [inProgress,  setInProgress]  = useState(() => getInitCacheIfValid()?.inProgress  ?? loadSavedFilter().inProgress)
  const [sort,        setSort]        = useState(() => getInitCacheIfValid()?.sort        ?? loadSavedFilter().sort)
  const [search,      setSearch]      = useState(() => getInitCacheIfValid()?.search      ?? '')
  const [searchInput, setSearchInput] = useState(() => getInitCacheIfValid()?.search      ?? '')

  const [searchFloating, setSearchFloating] = useState(false)
  const sentinelRef    = useRef<HTMLDivElement>(null)
  const searchRef      = useRef<HTMLInputElement>(null)
  const searchWrapRef  = useRef<HTMLDivElement>(null)
  const pageRef        = useRef(0)
  const loadingRef     = useRef(false)
  const filterKeyRef   = useRef('')
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Initialise refs from cache synchronously — prevents the filter effect from re-fetching
  useLayoutEffect(() => {
    const cached = getInitCacheIfValid()
    if (!cached) return
    filterKeyRef.current = cached.filterKey
    pageRef.current      = cached.page
  }, [])

  // Save scroll position continuously
  useEffect(() => {
    const onScroll = () => { if (_histCache) _histCache.scrollY = window.scrollY }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  // Restore scroll on mount — same pattern as CatalogPage.
  // Items come from the lazy useState initialiser so the DOM is already populated.
  useEffect(() => {
    const cached = getInitCacheIfValid()
    const hash   = window.location.hash.slice(1)

    if (cached && cached.scrollY > 0) {
      window.scrollTo({ top: cached.scrollY, behavior: 'instant' })
      if (hash) window.history.replaceState(null, '', window.location.pathname)
      return
    }

    if (!hash) return
    let cancelled = false
    let attempts  = 0
    const poll = () => {
      if (cancelled) return
      const el = document.getElementById(hash)
      if (el) {
        window.history.replaceState(null, '', window.location.pathname)
        el.scrollIntoView({ behavior: 'instant' as ScrollBehavior, block: 'center' })
        return
      }
      if (++attempts < 180) requestAnimationFrame(poll)
    }
    requestAnimationFrame(poll)
    return () => { cancelled = true }
  }, [])

  // Show floating search bar when the search field scrolls above the nav
  useEffect(() => {
    function check() {
      const el = searchWrapRef.current
      if (!el) return
      setSearchFloating(el.getBoundingClientRect().bottom < NAV_H)
    }
    window.addEventListener('scroll', check, { passive: true })
    return () => window.removeEventListener('scroll', check)
  }, [])

  function handleFilterTab(type: string) {
    if (type === 'in_progress') {
      setInProgress(true)
      setMediaType('')
      saveFilter('', true, sort)
    } else {
      setInProgress(false)
      setMediaType(type)
      saveFilter(type, false, sort)
    }
  }

  function activeFilterKey() {
    if (inProgress) return 'in_progress'
    return mediaType || 'all'
  }

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
    setSearch(searchInput.trim())
  }

  function handleSearchInput(value: string) {
    setSearchInput(value)
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current)
    if (value.length === 0) {
      setSearch('')
      return
    }
    searchTimerRef.current = setTimeout(() => {
      if (value.trim().length >= 2) setSearch(value.trim())
    }, 400)
  }

  // Fetch a single page; appends or resets based on `pg === 1`
  async function doFetch(pg: number, filterKey: string, dev: Device, prof: Profile, mt: string, ip: boolean, st: string, sr: string) {
    if (loadingRef.current) return
    loadingRef.current = true
    setLoading(true)

    const params = new URLSearchParams({
      page:       String(pg),
      per_page:   '24',
      device_id:  String(dev.id),
      profile_id: prof.profile_id,
      sort:       st,
    })
    if (mt) params.set('media_type',  mt)
    if (ip) params.set('in_progress', '1')
    if (sr) params.set('search',      sr)

    try {
      const r = await fetch(`/api/web/history?${params}`)
      if (!r.ok) throw new Error()
      const d: HistoryResponse = await r.json()

      if (filterKeyRef.current !== filterKey) return // filter changed during fetch

      setItems(prev => {
        const next = pg === 1 ? d.results : [...prev, ...d.results]
        _histCache = {
          filterKey,
          mediaType:  mt,
          inProgress: ip,
          sort:       st,
          search:     sr,
          items:      next,
          totalPages: d.total_pages,
          counts:     d.counts,
          page:       pg,
          scrollY:    _histCache?.scrollY ?? 0,
        }
        return next
      })
      setCounts(d.counts)
      setTotalPages(d.total_pages)
      pageRef.current = pg
    } catch {}
    finally {
      setLoading(false)
      loadingRef.current = false
    }
  }

  // When filters change → reset + fetch page 1
  useEffect(() => {
    if (!activeDevice || !activeProfile) return
    const filterKey = buildFilterKey(activeDevice.id, activeProfile.profile_id, mediaType, inProgress, sort, search)
    if (filterKeyRef.current === filterKey) return // already loaded (e.g. restored from cache)
    filterKeyRef.current = filterKey
    pageRef.current = 0
    setItems([])
    setTotalPages(1)
    // Not setCounts(null) — the filter tabs only render while counts isn't
    // null, so clearing it here unmounted the very button the user just
    // picked (or pressed Enter on) for the length of the refetch, dropping
    // keyboard focus to <body>. Stale counts stay on screen a moment
    // instead, which is harmless (they refresh a beat later) and keeps
    // focus right where it was.
    doFetch(1, filterKey, activeDevice, activeProfile, mediaType, inProgress, sort, search)
  }, [activeDevice, activeProfile, mediaType, inProgress, sort, search]) // eslint-disable-line

  // Infinite scroll sentinel
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel || !activeDevice || !activeProfile) return

    const dev = activeDevice
    const prof = activeProfile
    const mt   = mediaType
    const ip   = inProgress
    const st   = sort
    const sr   = search
    const fk   = filterKeyRef.current

    const observer = new IntersectionObserver(entries => {
      if (!entries[0].isIntersecting) return
      if (loadingRef.current) return
      if (pageRef.current >= totalPages) return
      doFetch(pageRef.current + 1, fk, dev, prof, mt, ip, st, sr)
    }, { rootMargin: '300px' })

    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [totalPages, activeDevice, activeProfile, mediaType, inProgress, sort, search]) // eslint-disable-line

  function handleCardClick(item: HistoryItem) {
    navigate(`/card/${item.card_id}`, { state: { backUrl: `/history#${item.card_id}` } })
  }

  // Keyboard navigation. Three keyboard-reachable zones stacked top to
  // bottom: search input → filter tabs → card grid (the sort/media-type
  // <select>s are deliberately left out of the arrow ring — same convention
  // as the header's theme select — native browsers already drive their
  // value with Up/Down while focused, which would fight with using arrows to
  // move focus instead). ArrowUp/Down bridges between adjacent zones so the
  // grid isn't a keyboard dead end; Left/Right cycle within a zone.
  useEffect(() => {
    function focusSearch(): HTMLElement | null {
      // Prefer the floating bar's input when it's the one actually on
      // screen — the inline one it mirrors may be scrolled out of view.
      const el = document.querySelector<HTMLElement>('[data-hist-search-floating]')
        ?? document.querySelector<HTMLElement>('[data-hist-search]')
      el?.focus()
      return el
    }

    function onKeyDown(e: KeyboardEvent) {
      const focused = document.activeElement as HTMLElement | null
      const tag = focused?.tagName?.toLowerCase()

      if (tag === 'input' || tag === 'select' || tag === 'textarea') {
        // Bridge out of the search field (Up into the nav, Down into the
        // page's own controls) and out of the sort select (Up back to the
        // filter tabs). Enter/Space opens the sort select's own dropdown
        // explicitly via showPicker() — a plain focused <select> normally
        // opens on Enter/Space and cycles its value with Up/Down on its own,
        // but that native behavior turned out not to fire reliably here, so
        // Enter is wired by hand instead of being left to chance; once the
        // dropdown is actually open, Up/Down/Enter inside it are the
        // browser's own native popup, untouched.
        const inSearch = focused?.hasAttribute('data-hist-search') || focused?.hasAttribute('data-hist-search-floating')
        const inSort = focused?.hasAttribute('data-hist-sort')
        if (e.key === 'ArrowDown' && inSearch) {
          e.preventDefault()
          const target = document.querySelector<HTMLElement>('[data-hist-filter]') ?? document.querySelector<HTMLElement>('[data-hist-card]')
          target?.focus({ preventScroll: true })
        } else if (e.key === 'ArrowUp' && inSearch) {
          e.preventDefault()
          document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')?.focus()
        } else if (e.key === 'ArrowUp' && inSort) {
          e.preventDefault()
          document.querySelector<HTMLElement>('[data-hist-filter]')?.focus()
        } else if (e.key === 'ArrowDown' && inSort) {
          // Into the grid, not left to native behavior: on a
          // focused-but-closed <select>, this browser opens the dropdown on
          // plain ArrowDown too (not just cycle the value), which fought
          // with Enter being the deliberate way to open it below — so this
          // is blocked with preventDefault and repurposed as the bridge
          // onward instead of just doing nothing.
          e.preventDefault()
          const first = document.querySelector<HTMLElement>('[data-hist-card]')
          first?.focus({ preventScroll: true })
          if (first) scrollV(first)
        } else if ((e.key === 'Enter' || e.key === ' ') && inSort) {
          e.preventDefault()
          ;(focused as HTMLSelectElement).showPicker?.()
        }
        return
      }

      if (e.key === 'Backspace') {
        navigate(-1)
        return
      }

      // Filter tab ring.
      if (focused?.hasAttribute('data-hist-filter')) {
        if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
        e.preventDefault()
        if (e.key === 'ArrowUp') { focusSearch(); return }
        if (e.key === 'ArrowDown') {
          // The sort select, not straight to the grid — otherwise arrow
          // navigation could never reach it at all, only Tab could.
          const sortEl = document.querySelector<HTMLElement>('[data-hist-sort]')
          sortEl?.focus()
          return
        }
        const tabs = Array.from(document.querySelectorAll<HTMLElement>('[data-hist-filter]'))
        const idx = tabs.indexOf(focused)
        if (e.key === 'ArrowLeft') tabs[Math.max(idx - 1, 0)]?.focus()
        else tabs[Math.min(idx + 1, tabs.length - 1)]?.focus()
        return
      }

      const cards = Array.from(document.querySelectorAll<HTMLElement>('[data-hist-card]'))
      const idx = cards.indexOf(focused as HTMLElement)

      if (e.key === 'Enter' && idx !== -1) {
        e.preventDefault()
        focused!.click()
        return
      }

      if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return

      if (idx === -1) {
        // Focus is somewhere outside the page's own controls (the nav, or
        // nothing at all) — only ArrowDown auto-enters, same convention as
        // Catalog/Моё, so Left/Right/Up don't fight with the nav's own use
        // of those keys.
        if (e.key !== 'ArrowDown') return
        e.preventDefault()
        const searchEl = document.querySelector<HTMLElement>('[data-hist-search]')
        const target = searchEl ?? document.querySelector<HTMLElement>('[data-hist-filter]') ?? cards[0]
        if (!target) return
        target.focus({ preventScroll: true })
        if (target === cards[0]) scrollV(cards[0])
        return
      }

      if (!cards.length) return
      e.preventDefault()
      const cols = getGridCols(cards)

      if (e.key === 'ArrowUp' && idx - cols < 0) {
        // Top row — bridge up to the sort select (the nearest zone above
        // the grid), or the filter tabs / search if those aren't rendered,
        // instead of clamping in place.
        const target = document.querySelector<HTMLElement>('[data-hist-sort]') ?? document.querySelector<HTMLElement>('[data-hist-filter]')
        if (target) target.focus()
        else focusSearch()
        return
      }

      let next = idx
      if (e.key === 'ArrowRight') next = Math.min(idx + 1, cards.length - 1)
      else if (e.key === 'ArrowLeft') next = Math.max(idx - 1, 0)
      else if (e.key === 'ArrowDown') next = Math.min(idx + cols, cards.length - 1)
      else if (e.key === 'ArrowUp') next = idx - cols

      if (next !== idx) {
        // preventScroll: focusing an off-screen element natively jumps it
        // into view instantly, before our own smooth scrollV below runs —
        // same fix as Catalog/MediaLibraryPage's grid nav.
        cards[next].focus({ preventScroll: true })
        scrollV(cards[next])
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate])

  const filterTabs = [
    { key: 'all',         label: 'Все',        count: counts?.all },
    { key: 'movie',       label: 'Фильмы',     count: counts?.movies },
    { key: 'tv',          label: 'Сериалы',    count: counts?.tv },
    { key: 'in_progress', label: 'В процессе', count: counts?.in_progress },
  ]

  return (
    <Layout>
      <div className={styles.page}>

        {/* ── Search ── */}
        {loaded && activeDevice && (
          <div className={styles.selectorBar}>
            <form className={styles.searchForm} onSubmit={handleSearch}>
              <div ref={searchWrapRef} className={styles.searchWrap}>
                <span className={styles.searchIcon}>🔍</span>
                <input
                  ref={searchRef}
                  className={styles.searchInput}
                  placeholder="Поиск…"
                  value={searchInput}
                  onChange={e => handleSearchInput(e.target.value)}
                  data-hist-search
                />
                {searchInput && (
                  <button className={styles.searchClear} onClick={() => handleSearchInput('')} title="Очистить">✕</button>
                )}
              </div>
            </form>
          </div>
        )}

        {/* ── Filter tabs + sort ── */}
        {counts !== null && (
          // data-row-id: opts this whole bar out of Layout's site-wide
          // Left/Right → side-panel behavior (see Layout.tsx's onKeyDown) —
          // without it, arrowing across the filter buttons or reaching the
          // selects summoned that panel instead of moving focus here.
          <div className={styles.controlsBar} data-row-id="history-controls">
            <div className={styles.filterTabs}>
              {filterTabs.map(t => (
                <button
                  key={t.key}
                  className={`${styles.filterTab} ${activeFilterKey() === t.key ? styles.filterTabActive : ''}`}
                  onClick={() => handleFilterTab(t.key === 'all' ? '' : t.key)}
                  data-hist-filter
                >
                  {t.label}{t.count !== undefined ? ` (${t.count})` : ''}
                </button>
              ))}
            </div>
            <select
              className={styles.filterSelect}
              value={activeFilterKey()}
              onChange={e => handleFilterTab(e.target.value === 'all' ? '' : e.target.value)}
            >
              {filterTabs.map(t => (
                <option key={t.key} value={t.key}>
                  {t.label}{t.count !== undefined ? ` (${t.count})` : ''}
                </option>
              ))}
            </select>
            <select
              className={styles.sortSelect}
              value={sort}
              onChange={e => { setSort(e.target.value); saveFilter(mediaType, inProgress, e.target.value) }}
              data-hist-sort
            >
              {SORT_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
        )}

        {/* ── States ── */}
        {!loaded && <div className={styles.empty}>Загрузка…</div>}

        {loaded && !activeProfile && (
          <div className={styles.empty}>Выберите профиль в шапке сайта</div>
        )}

        {loaded && activeProfile && !loading && items.length === 0 && (
          <div className={styles.empty}>История пуста</div>
        )}

        {/* ── Grid ── */}
        {items.length > 0 && (
          // data-row-id: same reasoning as the controls bar above — the grid
          // owns Left/Right/Up/Down itself (see the keydown effect below),
          // so it must opt out of Layout's site-wide side-panel behavior too.
          <div className={styles.grid} data-row-id="history-grid">
            {items.map(item => {
              const url = posterUrl(item.poster_path)
              return (
                <div key={item.card_id} id={item.card_id} className={styles.card} tabIndex={0} data-hist-card onClick={() => handleCardClick(item)} onKeyDown={e => { if (e.key === 'Enter') handleCardClick(item) }}>
                  {url ? (
                    <img className={styles.poster} src={url} alt={item.title} loading="lazy" />
                  ) : (
                    <div className={styles.posterPlaceholder}>Нет постера</div>
                  )}
                  {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
                  <div className={styles.cardBody}>
                    <p className={styles.cardTitle}>{item.title}</p>
                    {item.progress > 0 && (
                      <div className={styles.progress}>
                        <div
                          className={styles.progressBar}
                          style={{ width: `${Math.min(item.progress, 100)}%` }}
                        />
                      </div>
                    )}
                    <div className={styles.cardMeta}>
                      <span>{item.year}</span>
                      {item.progress > 0 && (
                        <span className={item.is_complete ? styles.complete : ''}>
                          {Math.round(item.progress)}%
                        </span>
                      )}
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}

        {loading && items.length === 0 && <div className={styles.empty}>Загрузка…</div>}
        {loading && items.length > 0  && <div className={styles.loadingMore}>Загрузка…</div>}

        <div ref={sentinelRef} className={styles.sentinel} />

        {searchFloating && (
          <div className={styles.floatingBar}>
            <div className={styles.floatingBarInner}>
              <span className={styles.floatingIcon}>🔍</span>
              <div className={styles.searchWrap} style={{flex: 1}}>
                <input
                  className={styles.floatingInput}
                  placeholder="Поиск…"
                  value={searchInput}
                  onChange={e => handleSearchInput(e.target.value)}
                  data-hist-search-floating
                />
                {searchInput && (
                  <button className={styles.searchClear} onClick={() => handleSearchInput('')} title="Очистить">✕</button>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
