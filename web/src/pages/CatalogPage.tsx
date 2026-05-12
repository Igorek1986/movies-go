import { useEffect, useLayoutEffect, useState, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
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
  category_name?: string
  year?: string
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

interface Device {
  id: number
  name: string
  token: string
}

interface Profile {
  device_id: number
  profile_id: string
  name: string
  icon?: string | null
}

const LS_ROW_ORDER    = 'catalog_row_order'
const LS_DEVICE_KEY   = 'active_device'
const LS_PROFILE_KEY  = 'active_profile'
const LS_PROFILES_KEY = 'cached_profiles'
const SS_SEARCH = 'catalog_search'

// Module-level cache — survives SPA navigation, resets on full page reload.
interface RowCache { items: MediaItem[]; totalPages: number }
interface CatViewCache { id: string; items: MediaItem[]; totalPages: number; currentPage: number; scrollY: number }
const _cache = {
  categories: [] as Category[],
  rows: {} as Record<string, RowCache>,
  scrollY: 0,
  catView: null as CatViewCache | null,
}

function getItemTitle(item: MediaItem): string {
  return item.title || item.name || ''
}

function getItemYear(item: MediaItem): string {
  return (item.release_date || item.first_air_date || '').slice(0, 4)
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


function loadDevice(): Device | null {
  try { return JSON.parse(localStorage.getItem(LS_DEVICE_KEY) || 'null') } catch { return null }
}
function saveDevice(d: Device | null) {
  try { d ? localStorage.setItem(LS_DEVICE_KEY, JSON.stringify(d)) : localStorage.removeItem(LS_DEVICE_KEY) } catch {}
}
function loadProfile(): Profile | null {
  try { return JSON.parse(localStorage.getItem(LS_PROFILE_KEY) || 'null') } catch { return null }
}
function saveProfile(p: Profile | null) {
  try { p ? localStorage.setItem(LS_PROFILE_KEY, JSON.stringify(p)) : localStorage.removeItem(LS_PROFILE_KEY) } catch {}
}
function loadProfiles(): Profile[] {
  try { return JSON.parse(localStorage.getItem(LS_PROFILES_KEY) || 'null') ?? [] } catch { return [] }
}
function saveProfiles(ps: Profile[]) {
  try { localStorage.setItem(LS_PROFILES_KEY, JSON.stringify(ps)) } catch {}
}

interface CardProps {
  item: MediaItem
  onClick: () => void
}

function MediaCard({ item, onClick }: CardProps) {
  const url = posterUrl(item.poster_path)
  const title = getItemTitle(item)
  const year = getItemYear(item)
  return (
    <div className={styles.card} onClick={onClick} tabIndex={0} data-card onKeyDown={e => { if (e.key === 'Enter') onClick() }}>
      {url
        ? <img className={styles.poster} src={url} alt={title} loading="lazy" />
        : <div className={styles.posterPlaceholder}>{title || 'Нет постера'}</div>
      }
      {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
      <div className={styles.cardBody}>
        <p className={styles.cardTitle}>{title}</p>
        <div className={styles.cardMeta}>
          {year && <span>{year}</span>}
          {item.vote_average > 0 && <span>★ {item.vote_average.toFixed(1)}</span>}
        </div>
        {item.release_quality && <span className={styles.quality}>{item.release_quality}</span>}
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
  dragHandlers: {
    onDragStart: (e: React.DragEvent, id: string) => void
    onDragEnd: () => void
    onDragOver: (e: React.DragEvent, id: string) => void
    onDrop: (e: React.DragEvent) => void
  }
  initialCache?: RowCache
  onItemsLoaded: (id: string, cache: RowCache) => void
}

function CategoryRow({ category, token, profileId, onExpandCategory, onCardClick, dragHandlers, initialCache, onItemsLoaded }: CategoryRowProps) {
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
      target?.scrollIntoView({ block: 'nearest' })
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
      <div className={styles.rowScroll}>
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
                cards[idx + 1]?.focus()
              }
            } else {
              cards[idx - 1]?.focus()
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
  devices?: Device[]
  profiles?: Profile[]
  activeDevice?: Device | null
  activeProfile?: Profile | null
  onSelectDevice?: (d: Device) => void
  onSelectProfile?: (p: Profile) => void
}

function CategoryView({ category, token, profileId, onBack, onCardClick, focusAfterIdx, devices, profiles, activeDevice, activeProfile, onSelectDevice, onSelectProfile }: CategoryViewProps) {
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
    if (cached && cached.scrollY > 0) window.scrollTo(0, cached.scrollY)
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
        target?.scrollIntoView({ block: 'nearest' })
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

  const catVisibleProfiles = (profiles ?? []).filter(p => p.device_id === activeDevice?.id)
  const catShowDeviceTabs = (devices ?? []).length > 1
  const catShowProfileTabs = catVisibleProfiles.length > 0

  return (
    <div className={styles.categoryView}>
      <button className={styles.backBtn} onClick={onBack}>← Назад</button>
      {(catShowDeviceTabs || catShowProfileTabs) && (
        <div className={styles.toolbar}>
          {catShowDeviceTabs && (
            <div className={styles.deviceTabs}>
              {devices!.map(d => (
                <button
                  key={d.id}
                  className={`${styles.deviceTab} ${activeDevice?.id === d.id ? styles.deviceTabActive : ''}`}
                  onClick={() => onSelectDevice?.(d)}
                >
                  {d.name}
                </button>
              ))}
            </div>
          )}
          {catShowProfileTabs && (
            <div className={styles.profileTabs}>
              {catVisibleProfiles.map(p => (
                <button
                  key={p.profile_id}
                  className={`${styles.tab} ${activeProfile?.profile_id === p.profile_id ? styles.tabActive : ''}`}
                  onClick={() => onSelectProfile?.(p)}
                >
                  {p.name}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
      <div className={styles.categoryHeader}>
        <h2 className={styles.categoryTitle}>{category.name}</h2>
        <div ref={catSearchRef} className={styles.searchWrap}>
          <input
            className={styles.searchInput}
            placeholder="Поиск…"
            value={searchValue}
            onChange={e => handleSearchChange(e.target.value)}
          />
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
            <input
              className={styles.floatingInput}
              placeholder="Поиск…"
              value={searchValue}
              onChange={e => handleSearchChange(e.target.value)}
            />
          </div>
        </div>
      )}
    </div>
  )
}

export default function CatalogPage() {
  const navigate = useNavigate()
  const [categories, setCategories] = useState<Category[]>(() => applyRowOrder(_cache.categories))
  const [expandedCategory, setExpandedCategory] = useState<string | null>(() => {
    const p = new URLSearchParams(window.location.search)
    return p.get('cat')
  })
  const [expandedFocusIdx, setExpandedFocusIdx] = useState<number | undefined>(undefined)
  const [searchValue, setSearchValue] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<MediaItem[] | null>(null)
  const [searchLoading, setSearchLoading] = useState(false)

  const [devices, setDevices] = useState<Device[]>([])
  const [profiles, setProfiles] = useState<Profile[]>(() => loadProfiles())
  const [activeDevice, setActiveDevice] = useState<Device | null>(() => loadDevice())
  const [activeProfile, setActiveProfile] = useState<Profile | null>(() => loadProfile())

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
      window.scrollTo(0, _cache.scrollY)
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

  useEffect(() => {
    async function loadDevicesAndProfiles() {
      try {
        const res = await fetch('/api/devices')
        if (!res.ok) return
        const devList: Device[] = await res.json()
        setDevices(devList)
        if (!devList.length) return

        const allProfiles: Profile[] = []
        await Promise.all(devList.map(async d => {
          try {
            const pRes = await fetch(`/api/devices/${d.id}/profiles`)
            if (!pRes.ok) return
            const pd: { profiles: { profile_id: string; name: string }[] } = await pRes.json()
            for (const p of pd.profiles) {
              allProfiles.push({ device_id: d.id, profile_id: p.profile_id, name: p.name })
            }
          } catch {}
        }))
        setProfiles(allProfiles)
        saveProfiles(allProfiles)

        const savedDev  = loadDevice()
        const savedProf = loadProfile()
        const chosenDevice = devList.find(d => d.id === savedDev?.id) ?? devList[0]
        setActiveDevice(chosenDevice)

        const devProfiles = allProfiles.filter(p => p.device_id === chosenDevice.id)
        const chosenProfile =
          devProfiles.find(p => p.profile_id === savedProf?.profile_id) ?? devProfiles[0] ?? null
        setActiveProfile(chosenProfile)

        saveDevice(chosenDevice)
        saveProfile(chosenProfile)
      } catch {}
    }
    loadDevicesAndProfiles()
  }, [])

  useEffect(() => {
    async function loadCategories() {
      try {
        const res = await fetch('/api/categories')
        if (!res.ok) return
        const cats: Category[] = await res.json()
        _cache.categories = cats
        setCategories(applyRowOrder(cats))
      } catch {}
    }
    loadCategories()
  }, [])

  useEffect(() => {
    if (searchQuery.length < 3) {
      setSearchResults(null)
      return
    }
    if (expandedCategory) return
    setSearchLoading(true)
    fetch(`/api/search?q=${encodeURIComponent(searchQuery)}`)
      .then(r => r.ok ? r.json() : { results: [] })
      .then(data => {
        setSearchResults(data.results || [])
        setSearchLoading(false)
      })
      .catch(() => {
        setSearchResults([])
        setSearchLoading(false)
      })
  }, [searchQuery, expandedCategory])

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
    setExpandedCategory(null)
    const scrollY = savedScrollRef.current
    requestAnimationFrame(() => {
      window.scrollTo(0, scrollY)
    })
  }

  function handleCardClick(item: MediaItem) {
    const cardId = `${item.id}_${item.media_type}`
    const backUrl = expandedCategory
      ? `/catalog?cat=${encodeURIComponent(expandedCategory)}#${cardId}`
      : `/catalog#${cardId}`
    navigate(`/card/${cardId}`, { state: { backUrl } })
  }

  function selectDevice(d: Device) {
    setActiveDevice(d)
    const devProfiles = profiles.filter(p => p.device_id === d.id)
    const firstProfile = devProfiles[0] ?? null
    setActiveProfile(firstProfile)
    saveDevice(d)
    saveProfile(firstProfile)
  }

  function selectProfile(p: Profile) {
    setActiveProfile(p)
    saveProfile(p)
  }

  const visibleProfiles = profiles.filter(p => p.device_id === activeDevice?.id)
  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''

  function onDragStart(_e: React.DragEvent, id: string) {
    dragSrcRef.current = id
  }

  function onDragEnd() {
    const ids = categories.map(c => c.id)
    saveRowOrder(ids)
    dragSrcRef.current = null
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

      if (expandedCategory) return

      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        if (!focused?.hasAttribute('data-card')) {
          if (e.key === 'ArrowDown') {
            e.preventDefault()
            const first = document.querySelector<HTMLElement>('[data-card]')
            first?.focus()
            first?.scrollIntoView({ block: 'nearest' })
          }
          return
        }

        e.preventDefault()
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
        if (!targetCards.length) {
          targetRow.scrollIntoView({ block: 'nearest' })
          targetRow.dataset.pendingFocus = String(savedIdx)
          return
        }
        const target = targetCards[Math.min(savedIdx, targetCards.length - 1)]
        target?.focus()
        target?.scrollIntoView({ block: 'nearest' })
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [expandedCategory, navigate])

  const expandedCat = categories.find(c => c.id === expandedCategory) ?? null

  const showSearch = searchQuery.length >= 3 && !expandedCategory

  return (
    <Layout>
      <div className={styles.page}>
        {!expandedCat && (
          <div className={styles.toolbar}>
            <div className={styles.toolbarTop}>
              {devices.length > 1 && (
                <div className={styles.deviceTabs}>
                  {devices.map(d => (
                    <button
                      key={d.id}
                      className={`${styles.deviceTab} ${activeDevice?.id === d.id ? styles.deviceTabActive : ''}`}
                      onClick={() => selectDevice(d)}
                    >
                      {d.name}
                    </button>
                  ))}
                </div>
              )}
              <div ref={mainSearchRef} className={styles.searchWrap}>
                <input
                  className={styles.searchInput}
                  placeholder="Поиск…"
                  value={searchValue}
                  onChange={e => handleSearchChange(e.target.value)}
                />
              </div>
            </div>
            {visibleProfiles.length > 0 && (
              <div className={styles.profileTabs}>
                {visibleProfiles.map(p => (
                  <button
                    key={p.profile_id}
                    className={`${styles.tab} ${activeProfile?.profile_id === p.profile_id ? styles.tabActive : ''}`}
                    onClick={() => selectProfile(p)}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            )}
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
            devices={devices}
            profiles={profiles}
            activeDevice={activeDevice}
            activeProfile={activeProfile}
            onSelectDevice={selectDevice}
            onSelectProfile={selectProfile}
          />
        )}

        {!expandedCategory && showSearch && (
          <div>
            {searchLoading && <div className={styles.loading}>Поиск…</div>}
            {!searchLoading && searchResults !== null && searchResults.length === 0 && (
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
          </div>
        )}

        {!expandedCategory && !showSearch && (
          <div className={styles.rows}>
            {categories.map(cat => (
              <CategoryRow
                key={cat.id}
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
        {!expandedCategory && mainSearchFloating && (
          <div className={styles.floatingBar}>
            <div className={styles.floatingBarInner}>
              <span className={styles.floatingIcon}>🔍</span>
              <input
                className={styles.floatingInput}
                placeholder="Поиск…"
                value={searchValue}
                onChange={e => handleSearchChange(e.target.value)}
              />
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
