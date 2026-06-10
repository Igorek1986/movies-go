import { useEffect, useState, useMemo, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { ProfileBadges, type ProfileItem } from '@/components/ProfileBadges'
import styles from './NewCardsPage.module.scss'

interface DeviceRow {
  id: number
  username: string
  name: string
  profile_count: number
  timecode_count: number
  profiles: ProfileItem[]
  created_at: string
}

type FilterKey = 'username' | 'name'
type SortKey   = 'username' | 'name' | 'profile_count' | 'timecode_count' | 'created_at'

// ── FilterHeader ──────────────────────────────────────────────────────────────

function FilterHeader({ label, values, active, isOpen, onToggleOpen, onToggle, onClear, onSort, sortActive, sortDir }: {
  label: string
  values: [string, number][]
  active: Set<string>
  isOpen: boolean
  onToggleOpen: () => void
  onToggle: (v: string) => void
  onClear: () => void
  onSort?: () => void
  sortActive?: boolean
  sortDir?: 'asc' | 'desc'
}) {
  const ref = useRef<HTMLDivElement>(null)
  const count = active.size

  useEffect(() => {
    if (!isOpen) return
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onToggleOpen()
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [isOpen, onToggleOpen])

  return (
    <th style={{ userSelect: 'none', whiteSpace: 'nowrap' }}>
      <span onClick={onSort} style={{ cursor: onSort ? 'pointer' : 'default', color: sortActive ? '#4a90e2' : undefined }}>
        {label}{sortActive ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}
      </span>
      {' '}
      <span onClick={onToggleOpen} style={{ cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 2, color: count > 0 ? '#4a90e2' : '#666' }}>
        <span style={{ fontSize: '0.7em' }}>{isOpen ? '▲' : '▼'}</span>
        {count > 0 && (
          <span style={{ fontSize: '0.75em', background: '#4a90e2', color: '#fff',
            borderRadius: 8, padding: '0 5px', lineHeight: '1.6' }}>{count}</span>
        )}
      </span>
      {isOpen && (
        <div ref={ref} style={{
          position: 'absolute', top: '100%', left: 0, zIndex: 100,
          background: '#1a1a1a', border: '1px solid #333', borderRadius: 6,
          minWidth: 160, maxHeight: 260, overflowY: 'auto',
          boxShadow: '0 4px 16px rgba(0,0,0,.6)', padding: '4px 0',
        }}>
          {count > 0 && (
            <button onClick={onClear} style={{
              display: 'block', width: '100%', textAlign: 'left',
              padding: '6px 12px', background: 'none', border: 'none',
              color: '#e05555', fontSize: '0.8rem', cursor: 'pointer',
              borderBottom: '1px solid #2a2a2a',
            }}>Сбросить</button>
          )}
          {values.map(([val, cnt]) => (
            <label key={val} style={{
              display: 'flex', alignItems: 'center', gap: 8,
              padding: '6px 12px', cursor: 'pointer', fontSize: '0.85rem',
              background: active.has(val) ? 'rgba(74,144,226,0.12)' : 'none',
            }}>
              <input type="checkbox" checked={active.has(val)} onChange={() => onToggle(val)}
                style={{ accentColor: '#4a90e2' }} />
              <span style={{ flex: 1 }}>{val}</span>
              <span style={{ color: '#666', fontSize: '0.75em' }}>{cnt}</span>
            </label>
          ))}
        </div>
      )}
    </th>
  )
}

// ── SortTh ────────────────────────────────────────────────────────────────────

function SortTh({ label, col, sort, onSort }: {
  label: string
  col: SortKey
  sort: { key: SortKey; dir: 'asc' | 'desc' } | null
  onSort: (k: SortKey) => void
}) {
  const active = sort?.key === col
  return (
    <th onClick={() => onSort(col)} style={{
      cursor: 'pointer', userSelect: 'none',
      color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap',
    }}>
      {label}{active ? (sort!.dir === 'asc' ? ' ↑' : ' ↓') : ''}
    </th>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function DevicesTodayPage() {
  const [rows, setRows]       = useState<DeviceRow[]>([])
  const [loading, setLoading] = useState(true)
  const [filters, setFilters] = useState<Record<FilterKey, Set<string>>>({
    username: new Set(), name: new Set(),
  })
  const [openCol, setOpenCol] = useState<FilterKey | null>(null)
  const [sort, setSort]       = useState<{ key: SortKey; dir: 'asc' | 'desc' } | null>(
    { key: 'created_at', dir: 'desc' }
  )
  const [profileSel, setProfileSel] = useState<Record<number, ProfileItem | null>>({})

  useEffect(() => {
    fetch('/api/admin/devices-today')
      .then(r => r.ok ? r.json() : [])
      .then(setRows)
      .finally(() => setLoading(false))
  }, [])

  function toggleOpen(col: FilterKey) {
    setOpenCol(prev => prev === col ? null : col)
  }
  function toggleFilter(col: FilterKey, val: string) {
    setFilters(prev => {
      const s = new Set(prev[col])
      s.has(val) ? s.delete(val) : s.add(val)
      return { ...prev, [col]: s }
    })
  }
  function clearFilter(col: FilterKey) {
    setFilters(prev => ({ ...prev, [col]: new Set() }))
  }
  function toggleSort(key: SortKey) {
    setSort(prev => {
      if (!prev || prev.key !== key) return { key, dir: 'desc' }
      if (prev.dir === 'desc') return { key, dir: 'asc' }
      return null
    })
    setOpenCol(null)
  }

  const distinctValues = useMemo(() => ({
    username: (() => {
      const m = new Map<string, number>()
      rows.forEach(r => m.set(r.username, (m.get(r.username) ?? 0) + 1))
      return Array.from(m.entries()).sort((a, b) => b[1] - a[1])
    })(),
    name: (() => {
      const m = new Map<string, number>()
      rows.forEach(r => m.set(r.name, (m.get(r.name) ?? 0) + 1))
      return Array.from(m.entries()).sort((a, b) => b[1] - a[1])
    })(),
  }), [rows])

  const filtered = useMemo(() => rows.filter(r =>
    (!filters.username.size || filters.username.has(r.username)) &&
    (!filters.name.size     || filters.name.has(r.name))
  ), [rows, filters])

  const sorted = useMemo(() => {
    if (!sort) return filtered
    return [...filtered].sort((a, b) => {
      const av = sort.key === 'profile_count' || sort.key === 'timecode_count'
        ? a[sort.key] - b[sort.key]
        : String(a[sort.key]).localeCompare(String(b[sort.key]))
      return typeof av === 'number'
        ? sort.dir === 'asc' ? av : -av
        : sort.dir === 'asc' ? av : -av
    })
  }, [filtered, sort])

  const hasFilters = Object.values(filters).some(s => s.size > 0)

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Новые устройства сегодня
            {hasFilters ? ` (${sorted.length} / ${rows.length})` : rows.length > 0 ? ` (${rows.length})` : ''}
          </h1>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {hasFilters && (
              <button onClick={() => setFilters({ username: new Set(), name: new Set() })}
                style={{ padding: '4px 10px', borderRadius: 6, border: '1px solid #555',
                  background: 'none', color: '#aaa', fontSize: '0.8rem', cursor: 'pointer' }}>
                Сбросить фильтры
              </button>
            )}
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>
          Устройства, активированные сегодня — {new Date().toLocaleDateString('ru-RU')}.
        </p>

        {!loading && rows.length > 0 && (
          <div className={styles.mobileControls}>
            <select value={sort ? sort.key : ''} onChange={e => {
              const k = e.target.value as SortKey | ''
              setSort(k ? { key: k, dir: sort?.key === k ? sort.dir : 'desc' } : null)
            }} style={{ background: '#111', border: '1px solid #444', color: '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">— сортировка —</option>
              <option value="username">Пользователь</option>
              <option value="name">Устройство</option>
              <option value="profile_count">Профили</option>
              <option value="timecode_count">Таймкодов</option>
              <option value="created_at">Время</option>
            </select>
            {sort && (
              <button onClick={() => setSort(prev => prev ? { ...prev, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : null)}
                style={{ background: 'none', border: '1px solid #444', color: '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem', cursor: 'pointer' }}>
                {sort.dir === 'asc' ? '↑' : '↓'}
              </button>
            )}
            <select value={filters.username.size === 1 ? [...filters.username][0] : ''} onChange={e => {
              if (e.target.value) setFilters(prev => ({ ...prev, username: new Set([e.target.value]) }))
              else clearFilter('username')
            }} style={{ background: '#111', border: '1px solid #444', color: filters.username.size > 0 ? '#4a90e2' : '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">Все пользователи</option>
              {distinctValues.username.map(([v]) => <option key={v} value={v}>{v}</option>)}
            </select>
            <select value={filters.name.size === 1 ? [...filters.name][0] : ''} onChange={e => {
              if (e.target.value) setFilters(prev => ({ ...prev, name: new Set([e.target.value]) }))
              else clearFilter('name')
            }} style={{ background: '#111', border: '1px solid #444', color: filters.name.size > 0 ? '#4a90e2' : '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">Все устройства</option>
              {distinctValues.name.map(([v]) => <option key={v} value={v}>{v}</option>)}
            </select>
          </div>
        )}

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && rows.length === 0 && <div className={styles.empty}>Сегодня новых устройств нет</div>}
        {!loading && sorted.length === 0 && rows.length > 0 && (
          <div className={styles.empty}>Нет устройств по выбранным фильтрам</div>
        )}

        {!loading && sorted.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <FilterHeader label="Пользователь"
                  values={distinctValues.username} active={filters.username}
                  isOpen={openCol === 'username'}
                  onToggleOpen={() => toggleOpen('username')}
                  onToggle={v => toggleFilter('username', v)}
                  onClear={() => clearFilter('username')}
                  onSort={() => toggleSort('username')} sortActive={sort?.key === 'username'} sortDir={sort?.dir} />
                <FilterHeader label="Устройство"
                  values={distinctValues.name} active={filters.name}
                  isOpen={openCol === 'name'}
                  onToggleOpen={() => toggleOpen('name')}
                  onToggle={v => toggleFilter('name', v)}
                  onClear={() => clearFilter('name')}
                  onSort={() => toggleSort('name')} sortActive={sort?.key === 'name'} sortDir={sort?.dir} />
                <SortTh col="profile_count"  label="Профили"   sort={sort} onSort={toggleSort} />
                <SortTh col="timecode_count" label="Таймкодов" sort={sort} onSort={toggleSort} />
                <SortTh col="created_at"     label="Время"     sort={sort} onSort={toggleSort} />
              </tr>
            </thead>
            <tbody>
              {sorted.map(d => (
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
                  <td data-label="Время" className={styles.time}>{d.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Layout>
  )
}
