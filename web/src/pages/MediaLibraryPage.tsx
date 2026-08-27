import { useEffect, useRef, useState, useCallback } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { scrollV, getGridCols } from '@/utils/scrollNav'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { getStoredBrowseLayout } from '@/utils/browseLayout'
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

function Card({ item, onClick, onActivate, isHeroActive }: {
  item: LibraryItem; onClick: () => void; onActivate?: () => void; isHeroActive?: boolean
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
      onMouseEnter={onActivate}
    >
      {url
        ? <img className={styles.poster} src={url} alt={title} loading="lazy" />
        : <div className={styles.posterPlaceholder}>Нет постера</div>
      }
      {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
      <div className={styles.cardBody}>
        <p className={styles.cardTitle}>{title}</p>
        <span className={styles.cardYear}>{itemYear(item)}</span>
      </div>
    </div>
  )
}

// ── Row: lazy-loaded on scroll into view, horizontal, "Все →" to expand ────────

function LibraryRow({ status, label, token, profileId, onExpand, onCardClick, onActivate, activeCardId, onItemsLoaded }: {
  status: StatusKey; label: string; token: string; profileId: string
  onExpand: (status: StatusKey) => void; onCardClick: (item: LibraryItem) => void
  onActivate?: (item: LibraryItem) => void
  activeCardId?: string | null
  onItemsLoaded?: (status: StatusKey, items: LibraryItem[]) => void
}) {
  const [items, setItems] = useState<LibraryItem[] | null>(null)
  const [totalPages, setTotalPages] = useState(1)
  const rowRef = useRef<HTMLDivElement>(null)
  const loadedRef = useRef(false)

  const loadItems = useCallback(() => {
    if (loadedRef.current || !token) return
    loadedRef.current = true
    fetch(libraryUrl(status, { token, profile_id: profileId, page: '1', per_page: '20' }))
      .then(r => r.ok ? r.json() : Promise.reject())
      .then((data: LibraryResponse) => {
        const results = data.results || []
        setItems(results)
        setTotalPages(data.total_pages || 1)
        onItemsLoaded?.(status, results)
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

  if (items !== null && items.length === 0) return null

  const hasMore = totalPages > 1

  return (
    <section ref={rowRef} className={styles.row}>
      <div className={styles.rowHeader}>
        <h3 className={styles.rowTitle}>{label}</h3>
        {hasMore && (
          <button className={styles.rowMore} onClick={() => onExpand(status)}>Все →</button>
        )}
      </div>
      <div className={styles.rowScroll} data-row-scroll>
        <div
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
                if (next) { scrollH(next); scrollV(next) }
              }
            } else {
              const prev = cards[idx - 1]
              prev?.focus()
              if (prev) { scrollH(prev); scrollV(prev) }
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

  // Per-device (localStorage) — see BrowseLayoutSettings on /profiles.
  const [layout] = useState(() => getStoredBrowseLayout())
  const hero = useHeroPreview<LibraryItem>()
  const heroInitRef = useRef(false)
  // Rows lazy-load independently and can resolve in any order — reading
  // whichever answers first made the initial hero background essentially
  // random. Scan ROW_ORDER instead, stopping at the first row that hasn't
  // reported yet, so the result is always the first row's first item
  // regardless of network timing (mirrors CatalogPage's tryInitHero).
  const rowLoadedRef = useRef<Map<StatusKey, LibraryItem[]>>(new Map())

  const handleItemsLoaded = useCallback((status: StatusKey, items: LibraryItem[]) => {
    rowLoadedRef.current.set(status, items)
    if (heroInitRef.current) return
    for (const st of ROW_ORDER) {
      const cached = rowLoadedRef.current.get(st)
      if (cached === undefined) return // earlier-in-order row hasn't reported yet — wait for it
      if (cached.length > 0) {
        heroInitRef.current = true
        hero.activate(cached[0])
        // Actually focus the card (not just hero state) so it's visibly
        // marked as active — preventScroll since it's already on-screen.
        requestAnimationFrame(() => {
          document.querySelector<HTMLElement>(`[data-row-id="${st}"] [data-card]`)?.focus({ preventScroll: true })
        })
        return
      }
      // else: this row loaded empty — keep scanning the next one
    }
  }, [hero.activate])

  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''

  function openCard(item: LibraryItem) {
    navigate(`/card/${cardIdOf(item)}`, { state: { backUrl: '/media-library' } })
  }

  // Track last-focused card index per row, to restore position when navigating back to it.
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
  }, [expanded, navigate])

  return (
    <Layout>
      {/* location.key is unique per navigation — remounts everything below on
          every visit to this page, so a status changed elsewhere (card detail
          page) is never shown stale without a hard refresh. */}
      <div className={styles.page} key={location.key}>
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
                  onActivate={layout === 'hero' ? hero.activate : undefined}
                  activeCardId={layout === 'hero' && hero.item ? cardIdOf(hero.item) : null}
                  onItemsLoaded={handleItemsLoaded}
                />
              ))}
            </div>
          </>
        )}
      </div>
    </Layout>
  )
}
