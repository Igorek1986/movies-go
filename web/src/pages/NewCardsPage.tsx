import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './NewCardsPage.module.scss'

interface NewCard {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  original_title: string
  year: string
  vote_average: number
  vote_count: number
  created_at: string
}

export default function NewCardsPage() {
  const navigate = useNavigate()
  const [cards, setCards] = useState<NewCard[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/admin/cards-today')
      .then(r => r.ok ? r.json() : [])
      .then(setCards)
      .finally(() => setLoading(false))
  }, [])

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Добавлено сегодня</h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Карточки, добавленные парсером сегодня — {new Date().toLocaleDateString('ru-RU')}.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && cards.length === 0 && (
          <div className={styles.empty}>Сегодня новых карточек нет</div>
        )}

        {!loading && cards.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Время</th>
                <th>Тип</th>
                <th>Название</th>
                <th>Оригинал</th>
                <th>Год</th>
                <th>Рейтинг</th>
              </tr>
            </thead>
            <tbody>
              {cards.map(c => (
                <tr
                  key={c.card_id}
                  className={styles.row}
                  onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/cards-today' } })}
                >
                  <td data-label="Время" className={styles.time}>{c.created_at}</td>
                  <td data-label="Тип" className={styles.type}>{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                  <td data-label="Название" className={styles.cardTitle}>{c.title}</td>
                  <td data-label="Оригинал" className={styles.muted}>{c.original_title !== c.title ? c.original_title : '—'}</td>
                  <td data-label="Год" className={styles.year}>{c.year || '—'}</td>
                  <td data-label="Рейтинг" className={styles.rating}>
                    {c.vote_count > 0 ? `${c.vote_average.toFixed(1)} (${c.vote_count})` : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Layout>
  )
}
