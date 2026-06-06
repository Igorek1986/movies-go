import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import styles from './PopularPage.module.scss'

interface DailyPoint {
  date: string
  plays: number
  viewers: number
  cards: number
}

interface PopularCard {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  poster_path: string
  year: string
  viewers: number
  plays: number
  avg_percent: number
  finished_rate: number
}

interface PopularData {
  days: number
  daily: DailyPoint[]
  cards: PopularCard[]
}

type SortKey = 'viewers' | 'plays' | 'avg_percent' | 'finished_rate' | 'year' | 'title'

function fmtDay(date: string): string {
  // date is "YYYY-MM-DD"
  const [, m, d] = date.split('-')
  return `${d}.${m}`
}

export default function PopularPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<PopularData | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<'all' | 'movie' | 'tv'>('all')
  const [sort, setSort] = useState<{ key: SortKey; dir: 'asc' | 'desc' }>({ key: 'viewers', dir: 'desc' })

  useEffect(() => {
    fetch('/api/admin/popular')
      .then(r => (r.ok ? r.json() : null))
      .then(setData)
      .finally(() => setLoading(false))
  }, [])

  const daily = data?.daily ?? []
  const allCards = data?.cards ?? []
  const maxPlays = daily.reduce((m, d) => Math.max(m, d.plays), 0) || 1

  function toggleSort(key: SortKey) {
    setSort(prev => prev.key === key
      ? { key, dir: prev.dir === 'desc' ? 'asc' : 'desc' }
      : { key, dir: key === 'title' || key === 'year' ? 'asc' : 'desc' })
  }

  const cards = useMemo(() => {
    const q = search.trim().toLowerCase()
    let list = allCards.filter(c =>
      (typeFilter === 'all' || c.media_type === typeFilter) &&
      (q === '' || c.title.toLowerCase().includes(q))
    )
    const { key, dir } = sort
    const mul = dir === 'asc' ? 1 : -1
    list = [...list].sort((a, b) => {
      if (key === 'title') return a.title.localeCompare(b.title, 'ru') * mul
      if (key === 'year') return ((Number(a.year) || 0) - (Number(b.year) || 0)) * mul
      return ((a[key] as number) - (b[key] as number)) * mul
    })
    return list
  }, [allCards, search, typeFilter, sort])

  function SortTh({ label, k, className, title }: { label: string; k: SortKey; className?: string; title?: string }) {
    const active = sort.key === k
    return (
      <th className={`${className ?? ''} ${styles.sortable}`} onClick={() => toggleSort(k)} title={title}>
        {label}{active && <span className={styles.sortArrow}>{sort.dir === 'asc' ? ' ↑' : ' ↓'}</span>}
      </th>
    )
  }

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Популярные карточки{allCards.length > 0 ? ` (${cards.length}/${allCards.length})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Локальные просмотры ваших пользователей за последние {data?.days ?? 30} дней —
          учитываются всегда, даже если задан Popular Source URL. Один просмотр на зрителя
          в сутки (с 30% досмотра).
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && allCards.length === 0 && (
          <div className={styles.empty}>Пока нет данных о просмотрах</div>
        )}

        {!loading && daily.length > 0 && (
          <div className={styles.chartCard}>
            <p className={styles.chartTitle}>Динамика просмотров по дням</p>
            <div className={styles.chart}>
              {daily.map(d => (
                <div
                  key={d.date}
                  className={styles.bar}
                  title={`${fmtDay(d.date)}: ${d.plays} просмотров, ${d.viewers} зрителей, ${d.cards} карточек`}
                >
                  <div
                    className={styles.barFill}
                    style={{ height: `${(d.plays / maxPlays) * 100}%` }}
                  />
                </div>
              ))}
            </div>
            <div className={styles.chart} style={{ height: 'auto' }}>
              {daily.map(d => (
                <div key={d.date} className={styles.barLabel}>{fmtDay(d.date)}</div>
              ))}
            </div>
          </div>
        )}

        {!loading && allCards.length > 0 && (
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
                  <th className={styles.rank}>#</th>
                  <th></th>
                  <SortTh label="Название" k="title" />
                  <SortTh label="Год" k="year" />
                  <th>Тип</th>
                  <SortTh label="Зрителей" k="viewers" className={styles.num} />
                  <SortTh label="Просмотров" k="plays" className={styles.num} />
                  <SortTh label="Досмотр" k="avg_percent" className={styles.num} title="Средняя глубина досмотра" />
                  <SortTh label="Додосмотрели" k="finished_rate" className={styles.num} title="Доля просмотров, досмотренных до конца (≥85%)" />
                </tr>
              </thead>
              <tbody>
                {cards.map((c, i) => {
                  const poster = posterUrl(c.poster_path, 'w92')
                  return (
                    <tr
                      key={c.card_id}
                      className={styles.row}
                      onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/popular' } })}
                    >
                      <td className={styles.rank}>{i + 1}</td>
                      <td>
                        {poster
                          ? <img src={poster} alt="" className={styles.poster} loading="lazy" />
                          : <div className={styles.posterPlaceholder} />}
                      </td>
                      <td className={styles.cardTitle}>{c.title}</td>
                      <td className={styles.muted}>{c.year || '—'}</td>
                      <td className={styles.muted}>{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                      <td className={`${styles.num} ${styles.numStrong}`}>{c.viewers.toLocaleString('ru')}</td>
                      <td className={`${styles.num} ${styles.muted}`}>{c.plays.toLocaleString('ru')}</td>
                      <td className={`${styles.num} ${c.avg_percent ? '' : styles.muted}`}>{c.avg_percent ? `${c.avg_percent}%` : '—'}</td>
                      <td className={`${styles.num} ${c.avg_percent ? '' : styles.muted}`}>{c.avg_percent ? `${c.finished_rate}%` : '—'}</td>
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
