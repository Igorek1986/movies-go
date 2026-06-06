import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
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
  rank: number // popularity position from the source (1-based)
}

interface SourceData {
  source_url: string
  results: Omit<SourceCard, 'rank'>[]
  total_results: number
}

type SortKey = 'rank' | 'title' | 'year' | 'viewers' | 'plays'
type SortState = { key: SortKey; dir: 'asc' | 'desc' }
type TypeFilter = 'all' | 'movie' | 'tv'

const LS_KEY = 'popular_source_prefs'

function loadPrefs(): { sort?: SortState; type?: TypeFilter } {
  try { return JSON.parse(localStorage.getItem(LS_KEY) || '{}') } catch { return {} }
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

  useEffect(() => {
    fetch('/api/admin/popular-source')
      .then(r => (r.ok ? r.json() : Promise.reject()))
      .then(setData)
      .catch(() => setError(true))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    localStorage.setItem(LS_KEY, JSON.stringify({ sort, type: typeFilter }))
  }, [sort, typeFilter])

  // Attach a stable popularity rank from the source order.
  const allCards: SourceCard[] = useMemo(
    () => (data?.results ?? []).map((c, idx) => ({ ...c, rank: idx + 1 })),
    [data],
  )
  const hasCounts = allCards.some(c => typeof c.viewers === 'number')

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
    return [...list].sort((a, b) => {
      if (key === 'title') return a.title.localeCompare(b.title, 'ru') * mul
      if (key === 'year') return (yearOf(a) - yearOf(b)) * mul
      if (key === 'rank') return (a.rank - b.rank) * mul
      return (((a[key] ?? 0) as number) - ((b[key] ?? 0) as number)) * mul
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

        <p className={styles.desc}>
          Список от внешнего источника (Popular Source URL{data?.source_url ? `: ${data.source_url}` : ''}).
          {hasCounts
            ? ' Зрители/просмотры — агрегированная статистика источника по всем его клиентам.'
            : ' Источник отдаёт только порядок популярности (счётчики появятся после обновления источника).'}
          {' '}Свои локальные просмотры смотри на странице «Популярных (локально)».
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && error && <div className={styles.empty}>Источник недоступен</div>}
        {!loading && !error && allCards.length === 0 && (
          <div className={styles.empty}>Источник вернул пустой список</div>
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
                  <th></th>
                  <SortableTh label="Название" k="title" sort={sort} onSort={toggleSort} className={styles.titleCol} />
                  <SortableTh label="Год" k="year" sort={sort} onSort={toggleSort} />
                  <th>Тип</th>
                  {hasCounts && <SortableTh label="Зрителей" k="viewers" sort={sort} onSort={toggleSort} className={styles.num} />}
                  {hasCounts && <SortableTh label="Просмотров" k="plays" sort={sort} onSort={toggleSort} className={styles.num} />}
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
                      {hasCounts && <td className={`${styles.num} ${styles.muted}`} data-label="Просмотров">{typeof c.plays === 'number' ? c.plays.toLocaleString('ru') : '—'}</td>}
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
