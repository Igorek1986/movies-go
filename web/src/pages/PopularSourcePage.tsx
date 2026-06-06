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
}

interface SourceData {
  source_url: string
  results: SourceCard[]
  total_results: number
}

type SortKey = 'default' | 'title' | 'year' | 'viewers' | 'plays'

function year(c: SourceCard): string {
  const d = c.media_type === 'movie' ? c.release_date : c.first_air_date
  return d ? d.slice(0, 4) : '—'
}

export default function PopularSourcePage() {
  const navigate = useNavigate()
  const [data, setData] = useState<SourceData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<'all' | 'movie' | 'tv'>('all')
  const [sort, setSort] = useState<{ key: SortKey; dir: 'asc' | 'desc' }>({ key: 'default', dir: 'desc' })

  useEffect(() => {
    fetch('/api/admin/popular-source')
      .then(r => (r.ok ? r.json() : Promise.reject()))
      .then(setData)
      .catch(() => setError(true))
      .finally(() => setLoading(false))
  }, [])

  const allCards = data?.results ?? []
  const hasCounts = allCards.some(c => typeof c.viewers === 'number')

  function toggleSort(key: SortKey) {
    setSort(prev => prev.key === key
      ? { key, dir: prev.dir === 'desc' ? 'asc' : 'desc' }
      : { key, dir: key === 'title' || key === 'year' ? 'asc' : 'desc' })
  }

  const cards = useMemo(() => {
    const q = search.trim().toLowerCase()
    const list = allCards
      .map((c, idx) => ({ c, idx }))
      .filter(({ c }) =>
        (typeFilter === 'all' || c.media_type === typeFilter) &&
        (q === '' || c.title.toLowerCase().includes(q)))
    const { key, dir } = sort
    const mul = dir === 'asc' ? 1 : -1
    list.sort((a, b) => {
      if (key === 'default') return (a.idx - b.idx) * mul
      if (key === 'title') return a.c.title.localeCompare(b.c.title, 'ru') * mul
      if (key === 'year') return ((Number(year(a.c)) || 0) - (Number(year(b.c)) || 0)) * mul
      return (((a.c[key] ?? 0) as number) - ((b.c[key] ?? 0) as number)) * mul
    })
    return list
  }, [allCards, search, typeFilter, sort])

  function SortTh({ label, k, className }: { label: string; k: SortKey; className?: string }) {
    const active = sort.key === k
    return (
      <th className={`${className ?? ''} ${styles.sortable}`} onClick={() => toggleSort(k)}>
        {label}{active && <span className={styles.sortArrow}>{sort.dir === 'asc' ? ' ↑' : ' ↓'}</span>}
      </th>
    )
  }

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
              <select className={styles.select} value={typeFilter} onChange={e => setTypeFilter(e.target.value as 'all' | 'movie' | 'tv')}>
                <option value="all">Все типы</option>
                <option value="movie">Фильмы</option>
                <option value="tv">Сериалы</option>
              </select>
            </div>

            <table className={styles.table}>
              <thead>
                <tr>
                  <SortTh label="#" k="default" className={styles.rank} />
                  <th></th>
                  <SortTh label="Название" k="title" />
                  <SortTh label="Год" k="year" />
                  <th>Тип</th>
                  {hasCounts && <SortTh label="Зрителей" k="viewers" className={styles.num} />}
                  {hasCounts && <SortTh label="Просмотров" k="plays" className={styles.num} />}
                </tr>
              </thead>
              <tbody>
                {cards.map(({ c, idx }, i) => {
                  const poster = posterUrl(c.poster_path, 'w92')
                  const cardId = `${c.id}_${c.media_type}`
                  const rank = sort.key === 'default' && sort.dir === 'desc' ? idx + 1 : i + 1
                  return (
                    <tr
                      key={cardId}
                      className={styles.row}
                      onClick={() => navigate(`/card/${cardId}`, { state: { backUrl: '/admin/popular-source' } })}
                    >
                      <td className={styles.rank}>{rank}</td>
                      <td>
                        {poster
                          ? <img src={poster} alt="" className={styles.poster} loading="lazy" />
                          : <div className={styles.posterPlaceholder} />}
                      </td>
                      <td className={styles.cardTitle}>{c.title}</td>
                      <td className={styles.muted}>{year(c)}</td>
                      <td className={styles.muted}>{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                      {hasCounts && <td className={`${styles.num} ${styles.numStrong}`}>{typeof c.viewers === 'number' ? c.viewers.toLocaleString('ru') : '—'}</td>}
                      {hasCounts && <td className={`${styles.num} ${styles.muted}`}>{typeof c.plays === 'number' ? c.plays.toLocaleString('ru') : '—'}</td>}
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
