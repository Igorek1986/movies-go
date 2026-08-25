import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import DailyChart, { type DailyPoint } from '@/components/DailyChart'
import { posterUrl } from '@/utils/poster'
import styles from './PopularPage.module.scss'

interface SourceCard {
  id: number
  media_type: string
  title: string
  poster_path: string
  release_date: string
  first_air_date: string
  viewers?: number
  plays?: number
  avg_percent?: number
  finished_rate?: number
  weighted_plays?: number
  rank: number // popularity position from the source (1-based)
}

interface SourceData {
  source_url: string
  results: Omit<SourceCard, 'rank'>[]
  total_results: number
  total_pages?: number
  daily?: DailyPoint[] | null
}

type RawCard = Omit<SourceCard, 'rank'>

function pageURL(page: number, date: string | null): string {
  const qs = [page > 1 ? `page=${page}` : '', date ? `date=${date}` : ''].filter(Boolean).join('&')
  return page > 1 ? `/api/admin/popular-source/page?${qs}` : `/api/admin/popular-source${qs ? `?${qs}` : ''}`
}

// Loads the popular-source list a couple pages at a time instead of eagerly
// fetching everything in the background: page 1 renders immediately with a
// known, stable total (so the header count never jumps around), page 2 is
// prefetched right after so there's enough content to fill the viewport, and
// further pages load only when the caller's `loadMore()` is invoked — driven
// by scroll position in the component below.
function usePaginatedPopularSource(date: string | null, enabled: boolean) {
  const [data, setData] = useState<SourceData | null>(null)
  const [loading, setLoading] = useState(enabled)
  const [error, setError] = useState(false)
  const [pagesLoaded, setPagesLoaded] = useState(0)
  const [loadingMore, setLoadingMore] = useState(false)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    setLoading(true)
    setError(false)
    setData(null)
    setPagesLoaded(0)

    fetch(pageURL(1, date))
      .then(r => (r.ok ? r.json() : Promise.reject()))
      .then(async (first: SourceData) => {
        if (cancelled) return
        setData(first)
        setPagesLoaded(1)
        setLoading(false)
        if ((first.total_pages ?? 1) <= 1) return
        try {
          const second: { results: RawCard[] } = await fetch(pageURL(2, date))
            .then(r => (r.ok ? r.json() : Promise.reject()))
          if (!cancelled) {
            setData(d => (d ? { ...d, results: [...d.results, ...(second.results ?? [])] } : d))
            setPagesLoaded(2)
          }
        } catch {
          // best-effort prefetch — page 1 alone is still a usable result
        }
      })
      .catch(() => { if (!cancelled) { setError(true); setLoading(false) } })
    return () => { cancelled = true }
  }, [date, enabled])

  const totalPages = data?.total_pages ?? 1
  const hasMore = pagesLoaded < totalPages

  const loadMore = useCallback(() => {
    if (!enabled || loadingMore || !hasMore) return
    setLoadingMore(true)
    const next = pagesLoaded + 1
    fetch(pageURL(next, date))
      .then(r => (r.ok ? r.json() : Promise.reject()))
      .then((d: { results: RawCard[] }) => {
        setData(cur => (cur ? { ...cur, results: [...cur.results, ...(d.results ?? [])] } : cur))
        setPagesLoaded(p => p + 1)
      })
      .catch(() => { /* best-effort — next scroll trigger retries the same page */ })
      .finally(() => setLoadingMore(false))
  }, [enabled, loadingMore, hasMore, pagesLoaded, date])

  return { data, loading, error, loadingMore, hasMore, loadMore }
}

type SortKey = 'rank' | 'title' | 'year' | 'viewers' | 'plays' | 'avg_percent' | 'finished_rate'
type SortState = { key: SortKey; dir: 'asc' | 'desc' }
type TypeFilter = 'all' | 'movie' | 'tv'

const LS_KEY = 'popular_source_prefs'

function loadPrefs(): { sort?: SortState; type?: TypeFilter } {
  try { return JSON.parse(localStorage.getItem(LS_KEY) || '{}') } catch { return {} }
}

function fmtDayFull(date: string): string {
  const [y, m, d] = date.split('-')
  return `${d}.${m}.${y}`
}

function yearOf(c: SourceCard): number {
  const d = c.media_type === 'movie' ? c.release_date : c.first_air_date
  return d ? Number(d.slice(0, 4)) || 0 : 0
}

function SortableTh({ label, k, sort, onSort, className }: {
  label: string
  k: SortKey
  sort: SortState
  onSort: (k: SortKey) => void
  className?: string
}) {
  const active = sort.key === k
  return (
    <th className={`${className ?? ''} ${styles.sortable}`} onClick={() => onSort(k)}>
      {label}{active && <span className={styles.sortArrow}>{sort.dir === 'asc' ? ' ↑' : ' ↓'}</span>}
    </th>
  )
}

export default function PopularSourcePage() {
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<TypeFilter>(() => loadPrefs().type ?? 'all')
  const [sort, setSort] = useState<SortState>(() => loadPrefs().sort ?? { key: 'rank', dir: 'asc' })
  // Daily-chart filter: a selected day restricts the list to that date. The
  // source must support the date param; older sources ignore it (see note).
  const [selectedDate, setSelectedDate] = useState<string | null>(null)

  const main = usePaginatedPopularSource(null, true)
  const dayFiltered = usePaginatedPopularSource(selectedDate, !!selectedDate)
  const { data } = main // keeps the daily chart fed even while a date filter is active

  useEffect(() => {
    localStorage.setItem(LS_KEY, JSON.stringify({ sort, type: typeFilter }))
  }, [sort, typeFilter])

  const active = selectedDate ? dayFiltered : main
  const activeData = active.data
  const loading = active.loading
  const error = active.error
  const results = activeData?.results ?? []
  const allCards: SourceCard[] = useMemo(
    () => results.map((c, idx) => ({ ...c, rank: idx + 1 })),
    [results],
  )
  const hasCounts = allCards.some(c => typeof c.viewers === 'number')
  const hasMetrics = allCards.some(c => typeof c.avg_percent === 'number')
  const daily = data?.daily ?? []

  // Scroll-triggered pagination: load the next page once the sentinel below
  // the table comes into view. The sentinel only mounts once loading finishes
  // (it's inside the same conditional block as the table), so this uses a
  // state-backed callback ref rather than useRef+empty-deps-useEffect — a
  // plain ref would miss the node entirely since the effect that attaches
  // the observer would already have run (and found nothing) before the
  // sentinel div ever existed in the DOM.
  const [sentinelEl, setSentinelEl] = useState<HTMLDivElement | null>(null)
  const [sentinelVisible, setSentinelVisible] = useState(false)
  useEffect(() => {
    if (!sentinelEl) return
    const obs = new IntersectionObserver(([entry]) => setSentinelVisible(entry.isIntersecting), { rootMargin: '600px' })
    obs.observe(sentinelEl)
    return () => obs.disconnect()
  }, [sentinelEl])
  // Plain scroll/resize fallback alongside the IntersectionObserver above —
  // belt and suspenders, since IO delivery can be throttled/deferred in some
  // browser states (backgrounded or non-composited tabs) and this is the
  // core interaction the whole feature hinges on.
  useEffect(() => {
    if (!sentinelEl) return
    function check() {
      if (!sentinelEl) return
      const r = sentinelEl.getBoundingClientRect()
      setSentinelVisible(r.top < window.innerHeight + 600)
    }
    check()
    window.addEventListener('scroll', check, { passive: true })
    window.addEventListener('resize', check)
    return () => {
      window.removeEventListener('scroll', check)
      window.removeEventListener('resize', check)
    }
  }, [sentinelEl])
  // Search/filter only match loaded cards — while either is active, keep
  // loading the rest in the background (not just on scroll) so results stay
  // accurate instead of silently missing cards further down the list.
  const searching = search.trim() !== '' || typeFilter !== 'all'
  useEffect(() => {
    if ((sentinelVisible || searching) && active.hasMore && !active.loadingMore) active.loadMore()
  }, [sentinelVisible, searching, active.hasMore, active.loadingMore, active.loadMore])

  function toggleSort(key: SortKey) {
    setSort(prev => prev.key === key
      ? { key, dir: prev.dir === 'desc' ? 'asc' : 'desc' }
      : { key, dir: key === 'title' || key === 'year' || key === 'rank' ? 'asc' : 'desc' })
  }

  const cards = useMemo(() => {
    const q = search.trim().toLowerCase()
    const list = allCards.filter(c =>
      (typeFilter === 'all' || c.media_type === typeFilter) &&
      (q === '' || c.title.toLowerCase().includes(q))
    )
    const { key, dir } = sort
    const mul = dir === 'asc' ? 1 : -1
    // «Просмотров» сортируем по weighted_plays (если источник его отдаёт) — та же
    // метрика, что реально ранжирует «rank» (см. PopularPage.tsx).
    const val = (c: SourceCard) => key === 'plays'
      ? (c.weighted_plays ?? c.plays)
      : c[key as 'viewers' | 'avg_percent' | 'finished_rate']
    return [...list].sort((a, b) => {
      if (key === 'title') return a.title.localeCompare(b.title, 'ru') * mul
      if (key === 'year') return (yearOf(a) - yearOf(b)) * mul
      if (key === 'rank') return (a.rank - b.rank) * mul
      return (((val(a) ?? 0) as number) - ((val(b) ?? 0) as number)) * mul
    })
  }, [allCards, search, typeFilter, sort])

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Популярное (источник){activeData ? ` (${cards.length}/${activeData.total_results.toLocaleString('ru')})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Список от внешнего источника (Popular Source URL{data?.source_url ? `: ${data.source_url}` : ''}).
          {hasCounts
            ? ' Зрители/просмотры — агрегированная статистика источника по всем его клиентам.'
            : ' Источник отдаёт только порядок популярности (счётчики появятся после обновления источника).'}
          {' '}Свои локальные просмотры смотри на странице «Популярных (локально)».
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && error && <div className={styles.empty}>Источник недоступен</div>}

        {!loading && !error && daily.length > 0 && (
          <DailyChart
            daily={daily}
            title="Динамика просмотров по дням (источник)"
            selected={selectedDate}
            onSelect={setSelectedDate}
          />
        )}

        {selectedDate && (
          <p className={styles.filterNote}>
            Показаны данные источника за {fmtDayFull(selectedDate)}.{' '}
            <button className={styles.resetBtn} onClick={() => setSelectedDate(null)}>
              Сбросить
            </button>
          </p>
        )}

        {!loading && !error && allCards.length === 0 && (
          <div className={styles.empty}>
            {selectedDate ? 'В этот день просмотров не было' : 'Источник вернул пустой список'}
          </div>
        )}

        {!loading && !error && allCards.length > 0 && (
          <>
            <div className={styles.toolbar}>
              <input
                className={styles.search}
                placeholder="Поиск по названию…"
                value={search}
                onChange={e => setSearch(e.target.value)}
              />
              <select className={styles.select} value={typeFilter} onChange={e => setTypeFilter(e.target.value as TypeFilter)}>
                <option value="all">Все типы</option>
                <option value="movie">Фильмы</option>
                <option value="tv">Сериалы</option>
              </select>
              <div className={styles.mobileSort}>
                <select
                  className={styles.select}
                  value={sort.key}
                  onChange={e => setSort(s => ({ key: e.target.value as SortKey, dir: s.dir }))}
                >
                  <option value="rank">По популярности</option>
                  <option value="title">Название</option>
                  <option value="year">Год</option>
                  {hasCounts && <option value="viewers">Зрителей</option>}
                  {hasCounts && <option value="plays">Просмотров</option>}
                  {hasMetrics && <option value="avg_percent">Средний %</option>}
                  {hasMetrics && <option value="finished_rate">Финал</option>}
                </select>
                <button
                  className={styles.dirBtn}
                  onClick={() => setSort(s => ({ key: s.key, dir: s.dir === 'asc' ? 'desc' : 'asc' }))}
                  title="Направление сортировки"
                >
                  {sort.dir === 'asc' ? '↑ возр.' : '↓ убыв.'}
                </button>
              </div>
            </div>

            <table className={styles.table}>
              <thead>
                <tr>
                  <SortableTh label="#" k="rank" sort={sort} onSort={toggleSort} className={styles.rank} />
                  <th className={styles.thPoster}></th>
                  <SortableTh label="Название" k="title" sort={sort} onSort={toggleSort} className={styles.titleCol} />
                  <SortableTh label="Год" k="year" sort={sort} onSort={toggleSort} />
                  <th>Тип</th>
                  {hasCounts && <SortableTh label="Зрителей" k="viewers" sort={sort} onSort={toggleSort} className={styles.num} />}
                  {hasCounts && <SortableTh label="Просмотров" k="plays" sort={sort} onSort={toggleSort} className={styles.num} />}
                  {hasMetrics && <SortableTh label="Средний %" k="avg_percent" sort={sort} onSort={toggleSort} className={styles.num} />}
                  {hasMetrics && <SortableTh label="Финал" k="finished_rate" sort={sort} onSort={toggleSort} className={styles.num} />}
                </tr>
              </thead>
              <tbody>
                {cards.map(c => {
                  const poster = posterUrl(c.poster_path, 'w92')
                  const cardId = `${c.id}_${c.media_type}`
                  return (
                    <tr
                      key={cardId}
                      className={styles.row}
                      onClick={() => navigate(`/card/${cardId}`, { state: { backUrl: '/admin/popular-source' } })}
                    >
                      <td className={styles.rank}>{c.rank}</td>
                      <td className={styles.posterCell}>
                        {poster
                          ? <img src={poster} alt="" className={styles.poster} loading="lazy" />
                          : <div className={styles.posterPlaceholder} />}
                      </td>
                      <td className={styles.cardTitle}>{c.title}</td>
                      <td className={styles.muted} data-label="Год">{yearOf(c) || '—'}</td>
                      <td className={styles.muted} data-label="Тип">{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                      {hasCounts && <td className={`${styles.num} ${styles.numStrong}`} data-label="Зрителей">{typeof c.viewers === 'number' ? c.viewers.toLocaleString('ru') : '—'}</td>}
                      {hasCounts && (
                        <td className={`${styles.num} ${styles.muted}`} data-label="Просмотров">
                          {typeof c.plays === 'number' ? (
                            <>
                              {Math.round(c.weighted_plays ?? c.plays).toLocaleString('ru')}
                              {typeof c.weighted_plays === 'number' && c.weighted_plays !== c.plays && (
                                <span title="В скобках — реальное число просмотров без коэффициента movie/tv">
                                  {' '}({c.plays.toLocaleString('ru')})
                                </span>
                              )}
                            </>
                          ) : '—'}
                        </td>
                      )}
                      {hasMetrics && <td className={`${styles.num} ${c.avg_percent ? '' : styles.muted}`} data-label="Средний %">{c.avg_percent ? `${c.avg_percent}%` : '—'}</td>}
                      {hasMetrics && <td className={`${styles.num} ${c.avg_percent ? '' : styles.muted}`} data-label="Финал">{c.avg_percent ? `${c.finished_rate}%` : '—'}</td>}
                    </tr>
                  )
                })}
              </tbody>
            </table>
            {cards.length === 0 && <div className={styles.empty}>Ничего не найдено</div>}
            {active.hasMore && (search || typeFilter !== 'all') && (
              <p className={styles.filterNote}>
                Догружаем остальные карточки для поиска/фильтра: {results.length.toLocaleString('ru')} из {activeData!.total_results.toLocaleString('ru')}…
              </p>
            )}
            {active.hasMore && <div ref={setSentinelEl} className={styles.empty}>{active.loadingMore ? 'Загрузка…' : ''}</div>}
          </>
        )}
      </div>
    </Layout>
  )
}
