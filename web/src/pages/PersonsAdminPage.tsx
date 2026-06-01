import { useEffect, useState, useRef, useCallback } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { tmdbUrl } from '@/utils/poster'
import styles from './NewCardsPage.module.scss'

type SortKey = 'cards' | 'rating'
type SortDir = 'desc' | 'asc'
type SortEntry = { key: SortKey; dir: SortDir }

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

function sortArrow(entry: SortEntry | undefined, priority: number, total: number): string {
  if (!entry) return ''
  const arrow = entry.dir === 'desc' ? '↓' : '↑'
  return total > 1 ? `${arrow}${priority}` : arrow
}

export default function PersonsAdminPage() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const isDirectors = pathname.includes('directors')
  const title = isDirectors ? 'Режиссёры' : 'Актёры'
  const endpoint = isDirectors ? '/api/admin/directors' : '/api/admin/actors'
  const catPrefix = isDirectors ? 'director' : 'actor'

  const [items, setItems] = useState<PersonItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  // Array keeps priority order: first = primary sort
  const [sorts, setSorts] = useState<SortEntry[]>([{ key: 'cards', dir: 'desc' }])
  const pageRef = useRef(1)
  const hasMoreRef = useRef(true)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const loadingRef = useRef(false)

  function buildSortParam(s: SortEntry[]) {
    return s.map(e => `${e.key}_${e.dir}`).join(',')
  }

  const loadPage = useCallback(async (page: number, sortParam: string, reset: boolean) => {
    if (loadingRef.current) return
    loadingRef.current = true
    setLoading(true)
    try {
      const res = await fetch(`${endpoint}?page=${page}&sort=${encodeURIComponent(sortParam)}`)
      if (!res.ok) return
      const data: PageResponse = await res.json()
      setTotal(data.total)
      setItems(prev => reset ? data.items : [...prev, ...data.items])
      hasMoreRef.current = data.items.length === data.per_page
      pageRef.current = page
    } finally {
      loadingRef.current = false
      setLoading(false)
    }
  }, [endpoint])

  useEffect(() => {
    pageRef.current = 1
    hasMoreRef.current = true
    setItems([])
    loadPage(1, buildSortParam(sorts), true)
  }, [sorts, endpoint]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && !loadingRef.current && hasMoreRef.current) {
        loadPage(pageRef.current + 1, buildSortParam(sorts), false)
      }
    }, { rootMargin: '200px' })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [sorts, loadPage])

  function handleSortClick(key: SortKey) {
    setSorts(prev => {
      const idx = prev.findIndex(s => s.key === key)
      if (idx === -1) {
        // Not active → add as primary (unshift)
        return [{ key, dir: 'desc' }, ...prev]
      }
      const entry = prev[idx]
      if (entry.dir === 'desc') {
        // desc → asc
        const next = [...prev]
        next[idx] = { key, dir: 'asc' }
        return next
      }
      // asc → remove
      return prev.filter(s => s.key !== key)
    })
  }

  function colHeader(key: SortKey, label: string) {
    const entry = sorts.find(s => s.key === key)
    const priority = sorts.findIndex(s => s.key === key) + 1
    const arrow = sortArrow(entry, priority, sorts.length)
    return (
      <th
        style={{ cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap' }}
        onClick={() => handleSortClick(key)}
      >
        {label}{arrow && <span style={{ marginLeft: 4, color: '#4a90e2', fontSize: '0.85em' }}>{arrow}</span>}
      </th>
    )
  }

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>{title} ({total.toLocaleString()})</h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
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
                {colHeader('cards', 'Карточек')}
                {colHeader('rating', 'Рейтинг')}
                <th>Каталог</th>
              </tr>
            </thead>
            <tbody>
              {items.map((p, i) => {
                const photo = p.profile_path ? tmdbUrl(p.profile_path, 'w45') : null
                return (
                  <tr
                    key={p.person_id}
                    className={styles.row}
                    onClick={() => navigate(`/actor/${p.person_id}`)}
                  >
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
                    <td data-label="Каталог" onClick={e => e.stopPropagation()}>
                      <Link
                        to={`/catalog?cat=${catPrefix}_${p.person_id}`}
                        state={{ catName: (isDirectors ? 'Режиссёр: ' : 'В ролях: ') + p.person_name }}
                        style={{ fontSize: '0.8rem', color: '#4a90e2' }}
                      >
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
