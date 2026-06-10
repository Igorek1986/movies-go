import { useEffect, useState, useMemo, useRef } from 'react'
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
  created_at: string
}

type FilterKey = 'username' | 'device_name' | 'profile'
type SortKey   = 'username' | 'device_name' | 'profile' | 'card_id' | 'item' | 'created_at'

function profileLabel(t: TimecodeRow) {
  return t.profile_name || t.profile_id || '—'
}

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
    <th onClick={() => onSort(col)} style={{ cursor: 'pointer', userSelect: 'none',
      color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap' }}>
      {label}{active ? (sort!.dir === 'asc' ? ' ↑' : ' ↓') : ''}
    </th>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

export default function TimecodesTodayPage() {
  const [rows, setRows]       = useState<TimecodeRow[]>([])
  const [loading, setLoading] = useState(true)
  const [filters, setFilters] = useState<Record<FilterKey, Set<string>>>({
    username: new Set(), device_name: new Set(), profile: new Set(),
  })
  const [openCol, setOpenCol] = useState<FilterKey | null>(null)
  const [sort, setSort]       = useState<{ key: SortKey; dir: 'asc' | 'desc' } | null>(
    { key: 'created_at', dir: 'desc' }
  )
  const navigate = useNavigate()

  useEffect(() => {
    fetch('/api/admin/timecodes-today')
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

  const distinctValues = useMemo(() => {
    const make = (getter: (r: TimecodeRow) => string) => {
      const m = new Map<string, number>()
      rows.forEach(r => { const v = getter(r); m.set(v, (m.get(v) ?? 0) + 1) })
      return Array.from(m.entries()).sort((a, b) => b[1] - a[1])
    }
    return {
      username:    make(r => r.username),
      device_name: make(r => r.device_name),
      profile:     make(r => profileLabel(r)),
    }
  }, [rows])

  const filtered = useMemo(() => rows.filter(r =>
    (!filters.username.size    || filters.username.has(r.username)) &&
    (!filters.device_name.size || filters.device_name.has(r.device_name)) &&
    (!filters.profile.size     || filters.profile.has(profileLabel(r)))
  ), [rows, filters])

  const sorted = useMemo(() => {
    if (!sort) return filtered
    return [...filtered].sort((a, b) => {
      let av: string, bv: string
      if (sort.key === 'profile') { av = profileLabel(a); bv = profileLabel(b) }
      else { av = String(a[sort.key]); bv = String(b[sort.key]) }
      return sort.dir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av)
    })
  }, [filtered, sort])

  const hasFilters = Object.values(filters).some(s => s.size > 0)

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Новые таймкоды сегодня
            {hasFilters ? ` (${sorted.length} / ${rows.length})` : rows.length > 0 ? ` (${rows.length})` : ''}
          </h1>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {hasFilters && (
              <button onClick={() => setFilters({ username: new Set(), device_name: new Set(), profile: new Set() })}
                style={{ padding: '4px 10px', borderRadius: 6, border: '1px solid #555',
                  background: 'none', color: '#aaa', fontSize: '0.8rem', cursor: 'pointer' }}>
                Сбросить фильтры
              </button>
            )}
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>
          Таймкоды, созданные сегодня — {new Date().toLocaleDateString('ru-RU')}.
        </p>

        {!loading && rows.length > 0 && (
          <div className={styles.mobileControls}>
            <select value={sort ? sort.key : ''} onChange={e => {
              const k = e.target.value as SortKey | ''
              setSort(k ? { key: k, dir: sort?.key === k ? sort.dir : 'desc' } : null)
            }} style={{ background: '#111', border: '1px solid #444', color: '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">— сортировка —</option>
              <option value="username">Пользователь</option>
              <option value="device_name">Устройство</option>
              <option value="profile">Профиль</option>
              <option value="card_id">Карточка</option>
              <option value="item">Эпизод</option>
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
            <select value={filters.device_name.size === 1 ? [...filters.device_name][0] : ''} onChange={e => {
              if (e.target.value) setFilters(prev => ({ ...prev, device_name: new Set([e.target.value]) }))
              else clearFilter('device_name')
            }} style={{ background: '#111', border: '1px solid #444', color: filters.device_name.size > 0 ? '#4a90e2' : '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">Все устройства</option>
              {distinctValues.device_name.map(([v]) => <option key={v} value={v}>{v}</option>)}
            </select>
            <select value={filters.profile.size === 1 ? [...filters.profile][0] : ''} onChange={e => {
              if (e.target.value) setFilters(prev => ({ ...prev, profile: new Set([e.target.value]) }))
              else clearFilter('profile')
            }} style={{ background: '#111', border: '1px solid #444', color: filters.profile.size > 0 ? '#4a90e2' : '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">Все профили</option>
              {distinctValues.profile.map(([v]) => <option key={v} value={v}>{v}</option>)}
            </select>
          </div>
        )}

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && rows.length === 0 && <div className={styles.empty}>Сегодня новых таймкодов нет</div>}
        {!loading && sorted.length === 0 && rows.length > 0 && (
          <div className={styles.empty}>Нет таймкодов по выбранным фильтрам</div>
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
                  values={distinctValues.device_name} active={filters.device_name}
                  isOpen={openCol === 'device_name'}
                  onToggleOpen={() => toggleOpen('device_name')}
                  onToggle={v => toggleFilter('device_name', v)}
                  onClear={() => clearFilter('device_name')}
                  onSort={() => toggleSort('device_name')} sortActive={sort?.key === 'device_name'} sortDir={sort?.dir} />
                <FilterHeader label="Профиль"
                  values={distinctValues.profile} active={filters.profile}
                  isOpen={openCol === 'profile'}
                  onToggleOpen={() => toggleOpen('profile')}
                  onToggle={v => toggleFilter('profile', v)}
                  onClear={() => clearFilter('profile')}
                  onSort={() => toggleSort('profile')} sortActive={sort?.key === 'profile'} sortDir={sort?.dir} />
                <SortTh col="card_id"    label="Карточка"  sort={sort} onSort={toggleSort} />
                <SortTh col="item"       label="Эпизод"    sort={sort} onSort={toggleSort} />
                <SortTh col="created_at" label="Время"     sort={sort} onSort={toggleSort} />
              </tr>
            </thead>
            <tbody>
              {sorted.map(t => (
                <tr key={t.id} className={styles.row}
                  onClick={() => navigate(`/card/${t.card_id}`, { state: { backUrl: '/admin/timecodes-today' } })}>
                  <td data-label="Пользователь" className={styles.cardTitle}>{t.username}</td>
                  <td data-label="Устройство"   className={styles.muted}>{t.device_name}</td>
                  <td data-label="Профиль"       className={styles.muted}>{profileLabel(t)}</td>
                  <td data-label="Карточка"      className={styles.muted}>{t.card_id}</td>
                  <td data-label="Эпизод"        className={styles.muted}>{t.item}</td>
                  <td data-label="Время"         className={styles.time}>{t.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Layout>
  )
}
