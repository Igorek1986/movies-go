import { useEffect, useRef, useState, useCallback } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, getGridCols } from '@/utils/scrollNav'
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

// Scroll the horizontal row container so el is centered (same as CatalogPage's scrollH).
function scrollH(el: HTMLElement) {
  const scroll = el.closest<HTMLElement>('[data-row-scroll]')
  if (!scroll) return
  const sr = scroll.getBoundingClientRect()
  const cr = el.getBoundingClientRect()
  const relCenter = cr.left - sr.left + scroll.scrollLeft + cr.width / 2
  scroll.scrollTo({ left: relCenter - scroll.clientWidth / 2 })
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

function LibraryRow({ status, label, token, profileId, onExpand, onCardClick, onActivate, activeCardId, initialCache, onItemsLoaded, onEmpty, slideDir, autoFocusIdx, hideHeader }: {
  status: StatusKey; label: string; token: string; profileId: string
  onExpand: (status: StatusKey) => void; onCardClick: (item: LibraryItem) => void
  onActivate?: (item: LibraryItem) => void
  activeCardId?: string | null
  initialCache?: RowCache
  onItemsLoaded?: (status: StatusKey, cache: RowCache) => void
  // Carousel mode (see CatalogPage's CategoryRow for the same pattern):
  // status turned out empty → advance; slide-in direction on mount; focus
  // this card index once loaded. All undefined/unused in Classic layout.
  onEmpty?: () => void
  slideDir?: 'from-top' | 'from-bottom'
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
  useEffect(() => {
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
    if (autoFocusIdx === undefined || autoFocusAppliedRef.current || !items?.length) return
    autoFocusAppliedRef.current = true
    requestAnimationFrame(() => {
      const cards = rowInnerRef.current?.querySelectorAll<HTMLElement>('[data-card]')
      if (!cards?.length) return
      cards[Math.min(autoFocusIdx, cards.length - 1)]?.focus({ preventScroll: true })
    })
  }, [items, autoFocusIdx])

  if (items !== null && items.length === 0) return null

  const hasMore = totalPages > 1
  const slideClass = slideDir === 'from-top' ? styles.slideFromTop : slideDir === 'from-bottom' ? styles.slideFromBottom : ''

  return (
    <section ref={rowRef} className={`${styles.row}${slideClass ? ' ' + slideClass : ''}`}>
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
                if (hasMore) onExpand(status)
              } else {
                const next = cards[idx + 1]
                next?.focus()
                // Horizontal-only move within the same row — its vertical position
                // doesn't change, so no scrollV here (it would force-recenter the
                // page, yanking the hero banner out of view for no reason).
                if (next) scrollH(next)
              }
            } else {
              const prev = cards[idx - 1]
              prev?.focus()
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
  const handleActivate = useCallback((item: LibraryItem) => hero.activate(item), [hero.activate])

  // Hero layout: non-scrolling carousel, same model as CatalogPage — exactly
  // one status's row mounted at a time, pinned to the bottom via CSS, never
  // scrolled. ArrowUp/Down (or a swipe) swaps which status that is.
  const [activeStatusIndex, setActiveStatusIndex] = useState(0)
  const [slideDir, setSlideDir] = useState<'from-top' | 'from-bottom' | undefined>(undefined)

  const handleEmptyStatus = useCallback(() => {
    setSlideDir(undefined)
    setActiveStatusIndex(idx => Math.min(idx + 1, ROW_ORDER.length - 1))
  }, [])

  const handleItemsLoaded = useCallback((status: StatusKey, cache: RowCache) => {
    _rowCache[status] = cache
  }, [])

  const switchStatus = useCallback((dir: 1 | -1) => {
    setActiveStatusIndex(idx => {
      const next = idx + dir
      if (next < 0 || next >= ROW_ORDER.length) return idx
      setSlideDir(dir > 0 ? 'from-bottom' : 'from-top')
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

      if (expanded) {
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
            // Hero carousel: the active row is always already pinned to the
            // bottom via CSS, nothing to scroll into view.
            if (layout !== 'hero' && first) scrollV(first)
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
        target?.focus()
        if (target) { scrollH(target); scrollV(target) }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [expanded, navigate, layout, activeStatusIndex, switchStatus])

  const carouselActive = layout === 'hero' && !expanded
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
                  slideDir={slideDir}
                  autoFocusIdx={lastRowFocusIdx.current.get(activeStatus) ?? 0}
                  hideHeader
                />
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
      </div>
    </Layout>
  )
}
