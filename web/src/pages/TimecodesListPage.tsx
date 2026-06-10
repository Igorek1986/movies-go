import { useEffect, useState, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './NewCardsPage.module.scss'

interface TimecodeRow {
  id: number
  username: string
  device_name: string
  profile_id: string
  profile_name: string
  card_id: string
  item: string
  updated_at: string
}

interface PagedResult {
  total: number
  page: number
  per_page: number
  items: TimecodeRow[]
}

type SortKey = 'username' | 'device' | 'profile' | 'card_id' | 'item' | 'updated_at'

export default function TimecodesListPage() {
  const [data, setData]         = useState<PagedResult | null>(null)
  const [loading, setLoading]   = useState(true)
  const [search, setSearch]     = useState('')
  const [query, setQuery]       = useState('')
  const [page, setPage]         = useState(1)
  const [sortBy, setSortBy]     = useState<SortKey>('updated_at')
  const [sortDir, setSortDir]   = useState<'asc' | 'desc'>('desc')
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const navigate = useNavigate()

  useEffect(() => {
    setLoading(true)
    const params = new URLSearchParams({
      page: String(page),
      sort_by: sortBy,
      sort_dir: sortDir,
    })
    if (query) params.set('search', query)
    fetch(`/api/admin/timecodes-list?${params}`)
      .then(r => r.ok ? r.json() : null)
      .then(setData)
      .finally(() => setLoading(false))
  }, [page, query, sortBy, sortDir])

  function handleSearch(val: string) {
    setSearch(val)
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => { setPage(1); setQuery(val.trim()) }, 300)
  }

  function toggleSort(key: SortKey) {
    if (sortBy === key) {
      setSortDir(d => d === 'desc' ? 'asc' : 'desc')
    } else {
      setSortBy(key)
      setSortDir('desc')
    }
    setPage(1)
  }

  function SortTh({ col, label }: { col: SortKey; label: string }) {
    const active = sortBy === col
    return (
      <th onClick={() => toggleSort(col)} style={{ cursor: 'pointer', userSelect: 'none',
        color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap' }}>
        {label}{active ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}
      </th>
    )
  }

  const totalPages = data ? Math.ceil(data.total / data.per_page) : 0

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Таймкоды{data ? ` (${data.total.toLocaleString()})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <div style={{ marginBottom: 16, position: 'relative', display: 'inline-flex', alignItems: 'center' }}>
          <input
            placeholder="Поиск по пользователю или карточке…"
            value={search}
            onChange={e => handleSearch(e.target.value)}
            style={{
              background: '#111', border: '1px solid #444', borderRadius: 6,
              color: '#fff', padding: '6px 28px 6px 12px', fontSize: '0.85rem',
              outline: 'none', width: 300,
            }}
          />
          {search && (
            <button onClick={() => handleSearch('')}
              style={{ position: 'absolute', right: 8, background: 'none', border: 'none', color: '#666', cursor: 'pointer', fontSize: '1rem', lineHeight: 1, padding: 0 }}>×</button>
          )}
        </div>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && data?.items.length === 0 && <div className={styles.empty}>Нет таймкодов</div>}

        {!loading && data && data.items.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <SortTh col="username"   label="Пользователь" />
                <SortTh col="device"     label="Устройство" />
                <SortTh col="profile"    label="Профиль" />
                <SortTh col="card_id"    label="Карточка" />
                <SortTh col="item"       label="Эпизод" />
                <SortTh col="updated_at" label="Обновлено" />
              </tr>
            </thead>
            <tbody>
              {data.items.map(t => (
                <tr key={t.id} className={styles.row}
                  onClick={() => navigate(`/card/${t.card_id}`, { state: { backUrl: '/admin/timecodes-list' } })}>
                  <td data-label="Пользователь" className={styles.cardTitle}>{t.username}</td>
                  <td data-label="Устройство"   className={styles.muted}>{t.device_name}</td>
                  <td data-label="Профиль"       className={styles.muted}>{t.profile_name || t.profile_id || '—'}</td>
                  <td data-label="Карточка"      className={styles.muted}>{t.card_id}</td>
                  <td data-label="Эпизод"        className={styles.muted}>{t.item}</td>
                  <td data-label="Обновлено"     className={styles.time}>{t.updated_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {totalPages > 1 && <Pagination page={page} total={totalPages} onChange={p => { setPage(p) }} />}
      </div>
    </Layout>
  )
}

function Pagination({ page, total, onChange }: { page: number; total: number; onChange: (p: number) => void }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 16, flexWrap: 'wrap' }}>
      <PgBtn disabled={page === 1} onClick={() => onChange(1)}>«</PgBtn>
      <PgBtn disabled={page === 1} onClick={() => onChange(page - 1)}>‹</PgBtn>
      {Array.from({ length: total }, (_, i) => i + 1)
        .filter(p => p === 1 || p === total || Math.abs(p - page) <= 2)
        .reduce<(number | '…')[]>((acc, p, i, arr) => {
          if (i > 0 && p - (arr[i - 1] as number) > 1) acc.push('…')
          acc.push(p)
          return acc
        }, [])
        .map((p, i) => p === '…'
          ? <span key={`e${i}`} style={{ color: '#666', padding: '0 4px' }}>…</span>
          : <PgBtn key={p} active={p === page} disabled={false} onClick={() => onChange(p as number)}>{p}</PgBtn>
        )}
      <PgBtn disabled={page === total} onClick={() => onChange(page + 1)}>›</PgBtn>
      <PgBtn disabled={page === total} onClick={() => onChange(total)}>»</PgBtn>
    </div>
  )
}

function PgBtn({ children, disabled, active, onClick }: {
  children: React.ReactNode; disabled: boolean; active?: boolean; onClick: () => void
}) {
  return (
    <button onClick={onClick} disabled={disabled} style={{
      padding: '3px 8px', borderRadius: 4, border: '1px solid',
      borderColor: active ? '#4a90e2' : '#444',
      background: active ? '#4a90e2' : 'none',
      color: disabled ? '#555' : active ? '#fff' : '#ccc',
      cursor: disabled ? 'default' : 'pointer',
      fontSize: '0.82rem', minWidth: 28,
    }}>{children}</button>
  )
}
