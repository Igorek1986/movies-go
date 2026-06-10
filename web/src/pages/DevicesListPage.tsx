import { useEffect, useState, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { ProfileBadges, type ProfileItem } from '@/components/ProfileBadges'
import styles from './NewCardsPage.module.scss'

interface DeviceRow {
  id: number
  user_id: number
  username: string
  name: string
  profile_count: number
  timecode_count: number
  profiles: ProfileItem[]
  created_at: string
}

interface PagedResult {
  total: number
  page: number
  per_page: number
  items: DeviceRow[]
}

type SortKey = 'username' | 'name' | 'profiles' | 'timecodes' | 'created_at'

function SortTh({ label, col, sortBy, sortDir, onSort }: {
  label: string
  col: SortKey
  sortBy: SortKey
  sortDir: 'asc' | 'desc'
  onSort: (k: SortKey) => void
}) {
  const active = sortBy === col
  return (
    <th onClick={() => onSort(col)} style={{
      cursor: 'pointer', userSelect: 'none',
      color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap',
    }}>
      {label}{active ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}
    </th>
  )
}

export default function DevicesListPage() {
  const [data, setData]       = useState<PagedResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch]   = useState('')
  const [query, setQuery]     = useState('')
  const [page, setPage]       = useState(1)
  const [sortBy, setSortBy]   = useState<SortKey>('created_at')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
  const [profileSel, setProfileSel] = useState<Record<number, ProfileItem | null>>({})
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    setLoading(true)
    const params = new URLSearchParams({ page: String(page), sort_by: sortBy, sort_dir: sortDir })
    if (query) params.set('search', query)
    fetch(`/api/admin/devices-list?${params}`)
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
    if (sortBy === key) setSortDir(d => d === 'desc' ? 'asc' : 'desc')
    else { setSortBy(key); setSortDir('desc') }
    setPage(1)
  }

  const totalPages = data ? Math.ceil(data.total / data.per_page) : 0

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Устройства{data ? ` (${data.total.toLocaleString()})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <div style={{ marginBottom: 16, position: 'relative', display: 'inline-flex', alignItems: 'center' }}>
          <input
            placeholder="Поиск по пользователю или устройству…"
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
        {!loading && data?.items.length === 0 && <div className={styles.empty}>Нет устройств</div>}

        {!loading && data && data.items.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <SortTh col="username"   label="Пользователь" sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                <SortTh col="name"       label="Устройство"   sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                <SortTh col="profiles"   label="Профили"      sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                <SortTh col="timecodes"  label="Таймкодов"    sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
                <SortTh col="created_at" label="Добавлено"    sortBy={sortBy} sortDir={sortDir} onSort={toggleSort} />
              </tr>
            </thead>
            <tbody>
              {data.items.map(d => (
                <tr key={d.id}>
                  <td data-label="Пользователь" className={styles.cardTitle}>{d.username}</td>
                  <td data-label="Устройство"   className={styles.muted}>{d.name}</td>
                  <td data-label="Профили">
                    <ProfileBadges profiles={d.profiles}
                      onSelect={p => setProfileSel(prev => ({ ...prev, [d.id]: p }))} />
                  </td>
                  <td data-label="Таймкодов" className={styles.muted}>
                    {(() => { const n = profileSel[d.id]?.timecode_count ?? d.timecode_count; return n > 0 ? n.toLocaleString() : '—' })()}
                  </td>
                  <td data-label="Добавлено" className={styles.time}>{d.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {totalPages > 1 && <Pagination page={page} total={totalPages} onChange={setPage} />}
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
