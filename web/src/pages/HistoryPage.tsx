import { useEffect, useState, useRef, useLayoutEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, getGridCols } from '@/utils/scrollNav'
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

interface Device {
  id: number
  name: string
  token: string
}

interface Profile {
  device_id: number
  profile_id: string
  name: string
}

const SORT_OPTIONS = [
  { value: 'watched',       label: 'По дате просмотра' },
  { value: 'release',       label: 'По дате выхода' },
  { value: 'progress_asc',  label: 'По прогрессу ↑' },
  { value: 'progress_desc', label: 'По прогрессу ↓' },
]

const DEVICE_KEY  = 'active_device'
const PROFILE_KEY = 'active_profile'
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
    const dev  = JSON.parse(localStorage.getItem(DEVICE_KEY) || 'null')
    const prof = JSON.parse(localStorage.getItem(PROFILE_KEY) || 'null')
    const prefix = [dev?.id, prof?.profile_id].join('|') + '|'
    return _histCache.filterKey.startsWith(prefix) ? _histCache : null
  } catch { return null }
}

export default function HistoryPage() {
  const navigate = useNavigate()

  const [items, setItems]         = useState<HistoryItem[]>(() => getInitCacheIfValid()?.items ?? [])
  const [counts, setCounts]       = useState<HistoryCounts | null>(() => getInitCacheIfValid()?.counts ?? null)
  const [totalPages, setTotalPages] = useState(() => getInitCacheIfValid()?.totalPages ?? 1)
  const [loading, setLoading]     = useState(false)
  const [initialized, setInitialized] = useState(false)

  const [mediaType,   setMediaType]   = useState(() => getInitCacheIfValid()?.mediaType   ?? loadSavedFilter().mediaType)
  const [inProgress,  setInProgress]  = useState(() => getInitCacheIfValid()?.inProgress  ?? loadSavedFilter().inProgress)
  const [sort,        setSort]        = useState(() => getInitCacheIfValid()?.sort        ?? loadSavedFilter().sort)
  const [search,      setSearch]      = useState(() => getInitCacheIfValid()?.search      ?? '')
  const [searchInput, setSearchInput] = useState(() => getInitCacheIfValid()?.search      ?? '')

  const [devices,  setDevices]  = useState<Device[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [activeDevice, setActiveDevice] = useState<Device | null>(() => {
    try { return JSON.parse(localStorage.getItem(DEVICE_KEY) || 'null') } catch { return null }
  })
  const [activeProfile, setActiveProfile] = useState<Profile | null>(() => {
    try { return JSON.parse(localStorage.getItem(PROFILE_KEY) || 'null') } catch { return null }
  })

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

  // Show floating search bar when the search field scrolls above the nav (52px)
  useEffect(() => {
    function check() {
      const el = searchWrapRef.current
      if (!el) return
      setSearchFloating(el.getBoundingClientRect().bottom < 52)
    }
    window.addEventListener('scroll', check, { passive: true })
    return () => window.removeEventListener('scroll', check)
  }, [])

  // Load devices + profiles on mount
  useEffect(() => {
    async function load() {
      const devRes = await fetch('/api/devices')
      if (!devRes.ok) { setInitialized(true); return }
      const devList: Device[] = await devRes.json()
      setDevices(devList)

      const savedDev  = activeDevice
      const foundDev  = savedDev ? devList.find(d => d.id === savedDev.id) : null
      const currentDev = foundDev ?? devList[0] ?? null

      const allProfiles: Profile[] = []
      await Promise.all(devList.map(async d => {
        const pRes = await fetch(`/api/devices/${d.id}/profiles`)
        if (!pRes.ok) return
        const pd: { profiles: { profile_id: string; name: string }[] } = await pRes.json()
        for (const p of pd.profiles) {
          allProfiles.push({ device_id: d.id, profile_id: p.profile_id, name: p.name })
        }
      }))
      setProfiles(allProfiles)

      if (!currentDev) { setInitialized(true); return }
      if (!foundDev) selectDevice(currentDev, allProfiles)

      const devProfiles = allProfiles.filter(p => p.device_id === currentDev.id)
      const savedProf   = activeProfile
      const foundProf   = savedProf?.device_id === currentDev.id
        ? devProfiles.find(p => p.profile_id === savedProf.profile_id)
        : null
      if (!foundProf && devProfiles.length > 0) selectProfile(devProfiles[0])

      setInitialized(true)
    }
    load()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function selectDevice(d: Device, currentProfiles: Profile[]) {
    setActiveDevice(d)
    localStorage.setItem(DEVICE_KEY, JSON.stringify(d))
    const source = currentProfiles.length > 0 ? currentProfiles : profiles
    const devProfiles = source.filter(p => p.device_id === d.id)
    if (devProfiles.length > 0) {
      selectProfile(devProfiles[0])
    } else {
      setActiveProfile(null)
      localStorage.removeItem(PROFILE_KEY)
    }
  }

  function selectProfile(p: Profile) {
    setActiveProfile(p)
    localStorage.setItem(PROFILE_KEY, JSON.stringify(p))
  }

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
    setCounts(null)
    setTotalPages(1)
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

  // Keyboard navigation
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const tag = (document.activeElement as HTMLElement)?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'select' || tag === 'textarea') return

      const cards = Array.from(document.querySelectorAll<HTMLElement>('[data-hist-card]'))
      if (!cards.length) return

      const focused = document.activeElement as HTMLElement
      const idx = cards.indexOf(focused)

      if (e.key === 'Enter' && idx !== -1) {
        e.preventDefault()
        focused.click()
        return
      }

      if (e.key === 'Backspace') {
        navigate(-1)
        return
      }

      if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
      e.preventDefault()

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
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate])

  const visibleProfiles = profiles.filter(p => p.device_id === activeDevice?.id)

  const filterTabs = [
    { key: 'all',         label: 'Все',        count: counts?.all },
    { key: 'movie',       label: 'Фильмы',     count: counts?.movies },
    { key: 'tv',          label: 'Сериалы',    count: counts?.tv },
    { key: 'in_progress', label: 'В процессе', count: counts?.in_progress },
  ]

  return (
    <Layout>
      <div className={styles.page}>

        {/* ── Device + profile selector ── */}
        {devices.length > 0 && (
          <div className={styles.selectorBar}>
            <div className={styles.selectorRow}>
              <div className={styles.deviceTabs}>
                {devices.map(d => (
                  <button
                    key={d.id}
                    className={`${styles.deviceTab} ${activeDevice?.id === d.id ? styles.deviceTabActive : ''}`}
                    onClick={() => selectDevice(d, profiles)}
                  >
                    {d.name}
                  </button>
                ))}
              </div>
              {visibleProfiles.length > 0 && (
                <div className={styles.profileTabs}>
                  {visibleProfiles.map(p => (
                    <button
                      key={p.profile_id}
                      className={`${styles.profileTab} ${activeProfile?.profile_id === p.profile_id ? styles.profileTabActive : ''}`}
                      onClick={() => selectProfile(p)}
                    >
                      {p.name}
                    </button>
                  ))}
                </div>
              )}
            </div>

            <form className={styles.searchForm} onSubmit={handleSearch}>
              <div ref={searchWrapRef} className={styles.searchWrap}>
                <span className={styles.searchIcon}>🔍</span>
                <input
                  ref={searchRef}
                  className={styles.searchInput}
                  placeholder="Поиск…"
                  value={searchInput}
                  onChange={e => handleSearchInput(e.target.value)}
                />
              </div>
            </form>
          </div>
        )}

        {/* ── Filter tabs + sort ── */}
        {counts !== null && (
          <div className={styles.controlsBar}>
            <div className={styles.filterTabs}>
              {filterTabs.map(t => (
                <button
                  key={t.key}
                  className={`${styles.filterTab} ${activeFilterKey() === t.key ? styles.filterTabActive : ''}`}
                  onClick={() => handleFilterTab(t.key === 'all' ? '' : t.key)}
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
            >
              {SORT_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
        )}

        {/* ── States ── */}
        {!initialized && <div className={styles.empty}>Загрузка…</div>}

        {initialized && !activeProfile && (
          <div className={styles.empty}>Выберите устройство и профиль</div>
        )}

        {initialized && activeProfile && !loading && items.length === 0 && (
          <div className={styles.empty}>История пуста</div>
        )}

        {/* ── Grid ── */}
        {items.length > 0 && (
          <div className={styles.grid}>
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
              <input
                className={styles.floatingInput}
                placeholder="Поиск…"
                value={searchInput}
                onChange={e => handleSearchInput(e.target.value)}
              />
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
