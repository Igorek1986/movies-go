import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './TMDBMissingPage.module.scss'

interface MissingCard {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  original_title: string
  release_date: string
  vote_average: number
  vote_count: number
  not_found_at: string
}

export default function TMDBMissingPage() {
  const navigate = useNavigate()
  const [cards, setCards] = useState<MissingCard[]>([])
  const [loading, setLoading] = useState(true)
  const [deleting, setDeleting] = useState<Set<string>>(new Set())

  async function load() {
    setLoading(true)
    try {
      const r = await fetch('/api/admin/tmdb-missing')
      if (r.ok) setCards(await r.json())
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function handleDelete(cardID: string) {
    if (!confirm(`Удалить карточку ${cardID}?`)) return
    setDeleting(prev => new Set(prev).add(cardID))
    try {
      const r = await fetch(`/api/admin/tmdb-missing/${encodeURIComponent(cardID)}`, { method: 'DELETE' })
      if (r.ok) setCards(prev => prev.filter(c => c.card_id !== cardID))
    } finally {
      setDeleting(prev => { const s = new Set(prev); s.delete(cardID); return s })
    }
  }

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Не найдено в TMDB</h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Карточки, которые не нашлись в TMDB при последнем обновлении (404). Данные могут быть устаревшими.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && cards.length === 0 && (
          <div className={styles.empty}>Проблемных карточек нет</div>
        )}

        {!loading && cards.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>TMDB ID</th>
                <th>Тип</th>
                <th>Название</th>
                <th>Оригинал</th>
                <th>Год</th>
                <th>Рейтинг</th>
                <th>Не найден</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {cards.map(c => (
                <tr
                  key={c.card_id}
                  className={styles.row}
                  onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/tmdb-missing' } })}
                >
                  <td data-label="TMDB ID">
                    <a
                      href={`https://www.themoviedb.org/${c.media_type}/${c.tmdb_id}`}
                      target="_blank"
                      rel="noreferrer"
                      className={styles.tmdbLink}
                      onClick={e => e.stopPropagation()}
                    >
                      {c.tmdb_id}
                    </a>
                  </td>
                  <td data-label="Тип" className={styles.type}>{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                  <td data-label="Название" className={styles.cardLink}>{c.title}</td>
                  <td data-label="Оригинал" className={styles.muted}>{c.original_title !== c.title ? c.original_title : '—'}</td>
                  <td data-label="Год" className={styles.year}>{c.release_date || '—'}</td>
                  <td data-label="Рейтинг" className={styles.rating}>
                    {c.vote_count > 0 ? `${c.vote_average.toFixed(1)} (${c.vote_count})` : '—'}
                  </td>
                  <td data-label="Не найден" className={styles.muted}>{c.not_found_at ? c.not_found_at.slice(0, 10) : '—'}</td>
                  <td data-label="">
                    <button
                      className={styles.deleteBtn}
                      onClick={e => { e.stopPropagation(); handleDelete(c.card_id) }}
                      disabled={deleting.has(c.card_id)}
                    >
                      {deleting.has(c.card_id) ? '…' : 'Удалить'}
                    </button>
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
