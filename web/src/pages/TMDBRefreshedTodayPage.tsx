import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './NewCardsPage.module.scss'

interface CardRow {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  original_title: string
  year: number
  vote_average: number
  vote_count: number
  updated_at: string
}

export default function TMDBRefreshedTodayPage() {
  const [cards, setCards] = useState<CardRow[]>([])
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()

  useEffect(() => {
    fetch('/api/admin/tmdb-refreshed-today')
      .then(r => r.ok ? r.json() : [])
      .then(setCards)
      .finally(() => setLoading(false))
  }, [])

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Обновлено из TMDB сегодня{cards.length > 0 ? ` (${cards.length})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Карточки, метаданные которых обновлены из TMDB сегодня — {new Date().toLocaleDateString('ru-RU')}.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && cards.length === 0 && <div className={styles.empty}>Сегодня обновлений из TMDB нет</div>}

        {!loading && cards.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Тип</th>
                <th>Название</th>
                <th>Оригинал</th>
                <th>Год</th>
                <th>Рейтинг</th>
                <th>Время</th>
              </tr>
            </thead>
            <tbody>
              {cards.map(c => (
                <tr key={c.card_id} className={styles.row}
                  onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/tmdb-refreshed-today' } })}>
                  <td data-label="Тип" className={styles.type}>{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                  <td data-label="Название" className={styles.cardTitle}>{c.title}</td>
                  <td data-label="Оригинал" className={styles.muted}>{c.original_title !== c.title ? c.original_title : '—'}</td>
                  <td data-label="Год" className={styles.year}>{c.year || '—'}</td>
                  <td data-label="Рейтинг" className={styles.rating}>
                    {c.vote_count > 0 ? `${c.vote_average.toFixed(1)} (${c.vote_count})` : '—'}
                  </td>
                  <td data-label="Время" className={styles.time}>{c.updated_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Layout>
  )
}
