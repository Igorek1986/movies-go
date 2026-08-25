import { useEffect, useMemo, useState } from 'react'
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

// Fetches page 1 immediately (onFirstPage), then the remaining pages
// (2..total_pages) concurrently in the background, delivering each as it
// arrives via onMorePage — so the table renders right away and fills in
// rather than blocking on all ~37 pages up front. A page that fails is
// dropped (best-effort), matching the backend's own behavior.
async function loadPopularSourceProgressively(
  date: string | null,
  onFirstPage: (data: SourceData) => void,
  onMorePage: (results: RawCard[]) => void,
  isCancelled: () => boolean,
): Promise<void> {
  const dateQS = date ? `date=${date}` : ''
  const first: SourceData = await fetch(`/api/admin/popular-source${dateQS ? `?${dateQS}` : ''}`)
    .then(r => (r.ok ? r.json() : Promise.reject()))
  if (isCancelled()) return
  onFirstPage(first)

  const totalPages = first.total_pages ?? 1
  if (totalPages <= 1) return
  const pending = Array.from({ length: totalPages - 1 }, (_, i) => i + 2)
  const concurrency = 6
  async function worker() {
    while (pending.length > 0) {
      if (isCancelled()) return
      const page = pending.shift()
      if (page === undefined) return
      try {
        const qs = ['page=' + page, dateQS].filter(Boolean).join('&')
        const d: { results: RawCard[] } = await fetch(`/api/admin/popular-source/page?${qs}`)
          .then(r => (r.ok ? r.json() : Promise.reject()))
        if (!isCancelled()) onMorePage(d.results ?? [])
      } catch {
        // best-effort — skip a failed page rather than aborting the rest
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, pending.length) }, worker))
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
  const [data, setData] = useState<SourceData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<TypeFilter>(() => loadPrefs().type ?? 'all')
  const [sort, setSort] = useState<SortState>(() => loadPrefs().sort ?? { key: 'rank', dir: 'asc' })
  // Daily-chart filter: a selected day restricts the list to that date. The
  // source must support the date param; older sources ignore it (see note).
  const [selectedDate, setSelectedDate] = useState<string | null>(null)
  const [dayData, setDayData] = useState<SourceData | null>(null)
  const [dayLoading, setDayLoading] = useState(false)

  useEffect(() => {
    let cancelled = false
    loadPopularSourceProgressively(
      null,
      first => { if (!cancelled) { setData(first); setLoading(false) } },
      more => { if (!cancelled) setData(d => (d ? { ...d, results: [...d.results, ...more] } : d)) },
      () => cancelled,
    ).catch(() => { if (!cancelled) { setError(true); setLoading(false) } })
    return () => { cancelled = true }
  }, [])

  // Refetch the source list for the selected day (chart filter) — same
  // page-1-first-then-fill-in behavior as the main list.
  useEffect(() => {
    if (!selectedDate) { setDayData(null); return }
    setDayLoading(true)
    let cancelled = false
    loadPopularSourceProgressively(
      selectedDate,
      first => { if (!cancelled) { setDayData(first); setDayLoading(false) } },
      more => { if (!cancelled) setDayData(d => (d ? { ...d, results: [...d.results, ...more] } : d)) },
      () => cancelled,
    ).catch(() => { if (!cancelled) { setDayData({ source_url: '', results: [], total_results: 0 }); setDayLoading(false) } })
    return () => { cancelled = true }
  }, [selectedDate])

  useEffect(() => {
    localStorage.setItem(LS_KEY, JSON.stringify({ sort, type: typeFilter }))
  }, [sort, typeFilter])

  // Attach a stable popularity rank from the source order.
  const activeData = selectedDate ? dayData : data
  const results = activeData?.results ?? []
  const allCards: SourceCard[] = useMemo(
    () => results.map((c, idx) => ({ ...c, rank: idx + 1 })),
    [results],
  )
  const hasCounts = allCards.some(c => typeof c.viewers === 'number')
  const hasMetrics = allCards.some(c => typeof c.avg_percent === 'number')
  const daily = data?.daily ?? []
  // Remaining pages still filling in the background (lazy-loaded, see
  // loadPopularSourceProgressively) — non-blocking, the table is already
  // interactive with what's loaded so far.
  const loadingMore = !!activeData && results.length < activeData.total_results

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
            Популярное (источник){data ? ` (${cards.length}/${allCards.length})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        {loadingMore && (
          <p className={styles.filterNote}>
            Догружаем ещё {(activeData!.total_results - results.length).toLocaleString('ru')} карточек…
          </p>
        )}

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

        {!loading && !error && dayLoading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && !error && !dayLoading && allCards.length === 0 && (
          <div className={styles.empty}>
            {selectedDate ? 'В этот день просмотров не было' : 'Источник вернул пустой список'}
          </div>
        )}

        {!loading && !error && !dayLoading && allCards.length > 0 && (
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
          </>
        )}
      </div>
    </Layout>
  )
}
