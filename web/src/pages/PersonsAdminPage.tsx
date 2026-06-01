import { useEffect, useState, useMemo } from 'react'
import { Link, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { tmdbUrl } from '@/utils/poster'
import styles from './NewCardsPage.module.scss'

type SortKey = 'card_count' | 'avg_rating'

interface PersonItem {
  person_id: number
  person_name: string
  profile_path: string
  card_count: number
  avg_rating: number
}

export default function PersonsAdminPage() {
  const { pathname } = useLocation()
  const isDirectors = pathname.includes('directors')
  const title = isDirectors ? 'Режиссёры' : 'Актёры'
  const endpoint = isDirectors ? '/api/admin/directors' : '/api/admin/actors'
  const catPrefix = isDirectors ? 'director' : 'actor'

  const [items, setItems] = useState<PersonItem[]>([])
  const [loading, setLoading] = useState(true)
  const [sortKey, setSortKey] = useState<SortKey>('card_count')

  useEffect(() => {
    setLoading(true)
    fetch(endpoint)
      .then(r => r.ok ? r.json() : [])
      .then(setItems)
      .finally(() => setLoading(false))
  }, [endpoint])

  const sorted = useMemo(() =>
    [...items].sort((a, b) => b[sortKey] - a[sortKey]),
    [items, sortKey]
  )

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>{title} ({items.length})</h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>
        <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
          <button
            onClick={() => setSortKey('card_count')}
            style={{ padding: '4px 12px', borderRadius: 6, border: '1px solid', cursor: 'pointer',
              background: sortKey === 'card_count' ? '#4a90e2' : 'transparent',
              borderColor: sortKey === 'card_count' ? '#4a90e2' : '#444',
              color: sortKey === 'card_count' ? '#fff' : '#aaa', fontSize: '0.82rem' }}
          >
            По карточкам
          </button>
          <button
            onClick={() => setSortKey('avg_rating')}
            style={{ padding: '4px 12px', borderRadius: 6, border: '1px solid', cursor: 'pointer',
              background: sortKey === 'avg_rating' ? '#4a90e2' : 'transparent',
              borderColor: sortKey === 'avg_rating' ? '#4a90e2' : '#444',
              color: sortKey === 'avg_rating' ? '#fff' : '#aaa', fontSize: '0.82rem' }}
          >
            По рейтингу
          </button>
        </div>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && items.length === 0 && (
          <div className={styles.empty}>Нет данных — запустите «Заполнить актёров и режиссёров»</div>
        )}

        {!loading && items.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>#</th>
                <th>Имя</th>
                <th style={{ cursor: 'pointer' }} onClick={() => setSortKey('card_count')}>
                  Карточек {sortKey === 'card_count' ? '↓' : ''}
                </th>
                <th style={{ cursor: 'pointer' }} onClick={() => setSortKey('avg_rating')}>
                  Рейтинг {sortKey === 'avg_rating' ? '↓' : ''}
                </th>
                <th>Категория</th>
              </tr>
            </thead>
            <tbody>
              {sorted.map((p, i) => {
                const photo = p.profile_path ? tmdbUrl(p.profile_path, 'w45') : null
                const catId = `${catPrefix}_${p.person_id}`
                return (
                  <tr key={p.person_id} className={styles.row}>
                    <td data-label="#">{i + 1}</td>
                    <td data-label="Имя">
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        {photo
                          ? <img src={photo} alt="" style={{ width: 32, height: 32, borderRadius: '50%', objectFit: 'cover', flexShrink: 0 }} />
                          : <div style={{ width: 32, height: 32, borderRadius: '50%', background: '#333', flexShrink: 0 }} />
                        }
                        {p.person_name}
                      </div>
                    </td>
                    <td data-label="Карточек">{p.card_count}</td>
                    <td data-label="Рейтинг">{p.avg_rating > 0 ? `★ ${p.avg_rating.toFixed(1)}` : '—'}</td>
                    <td data-label="Категория">
                      <Link to={`/catalog?cat=${catId}`} style={{ fontSize: '0.8rem', color: 'var(--color-primary, #4a90e2)' }}>
                        Открыть →
                      </Link>
                    </td>
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
