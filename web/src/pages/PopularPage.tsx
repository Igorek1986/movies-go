import { useEffect, useState } from 'react'
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

function fmtDay(date: string): string {
  // date is "YYYY-MM-DD"
  const [, m, d] = date.split('-')
  return `${d}.${m}`
}

export default function PopularPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<PopularData | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/admin/popular')
      .then(r => (r.ok ? r.json() : null))
      .then(setData)
      .finally(() => setLoading(false))
  }, [])

  const daily = data?.daily ?? []
  const cards = data?.cards ?? []
  const maxPlays = daily.reduce((m, d) => Math.max(m, d.plays), 0) || 1

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Популярные карточки{cards.length > 0 ? ` (${cards.length})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Просмотры за последние {data?.days ?? 30} дней. Один просмотр на зрителя в сутки
          (отсчитывается с 30% досмотра).
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && cards.length === 0 && (
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

        {!loading && cards.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th className={styles.rank}>#</th>
                <th></th>
                <th>Название</th>
                <th>Год</th>
                <th>Тип</th>
                <th className={styles.num}>Зрителей</th>
                <th className={styles.num}>Просмотров</th>
                <th className={styles.num} title="Средняя глубина досмотра">Досмотр</th>
                <th className={styles.num} title="Доля просмотров, досмотренных до конца (≥85%)">Додосмотрели</th>
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
        )}
      </div>
    </Layout>
  )
}
