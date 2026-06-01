import { useEffect, useState, useRef, useCallback } from 'react'
import { Link, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { tmdbUrl } from '@/utils/poster'
import styles from './NewCardsPage.module.scss'

type SortKey = 'cards' | 'rating'

interface PersonItem {
  person_id: number
  person_name: string
  profile_path: string
  card_count: number
  avg_rating: number
}

interface PageResponse {
  items: PersonItem[]
  total: number
  page: number
  per_page: number
}

export default function PersonsAdminPage() {
  const { pathname } = useLocation()
  const isDirectors = pathname.includes('directors')
  const title = isDirectors ? 'Режиссёры' : 'Актёры'
  const endpoint = isDirectors ? '/api/admin/directors' : '/api/admin/actors'
  const catPrefix = isDirectors ? 'director' : 'actor'

  const [items, setItems] = useState<PersonItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [sortKey, setSortKey] = useState<SortKey>('cards')
  const pageRef = useRef(1)
  const hasMoreRef = useRef(true)
  const sentinelRef = useRef<HTMLDivElement>(null)

  const loadPage = useCallback(async (page: number, sort: SortKey, reset: boolean) => {
    if (loading) return
    setLoading(true)
    try {
      const sortParam = sort === 'rating' ? 'rating' : 'cards'
      const res = await fetch(`${endpoint}?page=${page}&sort=${sortParam}`)
      if (!res.ok) return
      const data: PageResponse = await res.json()
      setTotal(data.total)
      setItems(prev => reset ? data.items : [...prev, ...data.items])
      hasMoreRef.current = data.items.length === data.per_page
      pageRef.current = page
    } finally {
      setLoading(false)
    }
  }, [endpoint, loading])

  // Reset and reload on sort or endpoint change
  useEffect(() => {
    pageRef.current = 1
    hasMoreRef.current = true
    setItems([])
    loadPage(1, sortKey, true)
  }, [sortKey, endpoint]) // eslint-disable-line react-hooks/exhaustive-deps

  // Infinite scroll
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && !loading && hasMoreRef.current) {
        loadPage(pageRef.current + 1, sortKey, false)
      }
    }, { rootMargin: '200px' })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [loading, sortKey, loadPage])

  function toggleSort(key: SortKey) {
    if (key !== sortKey) setSortKey(key)
  }

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>{title} ({total.toLocaleString()})</h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
          {(['cards', 'rating'] as SortKey[]).map(key => (
            <button key={key} onClick={() => toggleSort(key)} style={{
              padding: '4px 12px', borderRadius: 6, border: '1px solid', cursor: 'pointer',
              background: sortKey === key ? '#4a90e2' : 'transparent',
              borderColor: sortKey === key ? '#4a90e2' : '#444',
              color: sortKey === key ? '#fff' : '#aaa', fontSize: '0.82rem',
            }}>
              {key === 'cards' ? 'По карточкам' : 'По рейтингу'}
            </button>
          ))}
        </div>

        {items.length === 0 && !loading && (
          <div className={styles.empty}>Нет данных — запустите «Заполнить актёров и режиссёров»</div>
        )}

        {items.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>#</th>
                <th>Имя</th>
                <th style={{ cursor: 'pointer' }} onClick={() => toggleSort('cards')}>
                  Карточек {sortKey === 'cards' ? '↓' : ''}
                </th>
                <th style={{ cursor: 'pointer' }} onClick={() => toggleSort('rating')}>
                  Рейтинг {sortKey === 'rating' ? '↓' : ''}
                </th>
                <th>Категория</th>
              </tr>
            </thead>
            <tbody>
              {items.map((p, i) => {
                const photo = p.profile_path ? tmdbUrl(p.profile_path, 'w45') : null
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
                      <Link to={`/catalog?cat=${catPrefix}_${p.person_id}`}
                        style={{ fontSize: '0.8rem', color: 'var(--color-primary, #4a90e2)' }}>
                        Открыть →
                      </Link>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}

        {loading && <div className={styles.empty}>Загрузка…</div>}
        <div ref={sentinelRef} style={{ height: 1 }} />
      </div>
    </Layout>
  )
}
