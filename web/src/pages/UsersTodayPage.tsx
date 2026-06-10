import { useEffect, useState, useMemo, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { DeviceBadges, type DeviceItem } from '@/components/DeviceBadges'
import styles from './NewCardsPage.module.scss'

interface UserRow {
  id: number
  username: string
  role: string
  is_admin: boolean
  device_count: number
  timecode_count: number
  devices: DeviceItem[]
  created_at: string
}

type SortKey = 'username' | 'role' | 'device_count' | 'timecode_count' | 'created_at'

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
    function h(e: MouseEvent) { if (ref.current && !ref.current.contains(e.target as Node)) onToggleOpen() }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [isOpen, onToggleOpen])
  return (
    <th style={{ userSelect: 'none', whiteSpace: 'nowrap' }}>
      <span onClick={onSort} style={{ cursor: onSort ? 'pointer' : 'default', color: sortActive ? '#4a90e2' : undefined }}>
        {label}{sortActive ? (sortDir === 'asc' ? ' ↑' : ' ↓') : ''}
      </span>
      {' '}
      <span onClick={onToggleOpen} style={{ cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 2, color: count > 0 ? '#4a90e2' : '#666' }}>
        <span style={{ fontSize: '0.7em' }}>{isOpen ? '▲' : '▼'}</span>
        {count > 0 && <span style={{ fontSize: '0.75em', background: '#4a90e2', color: '#fff', borderRadius: 8, padding: '0 5px', lineHeight: '1.6' }}>{count}</span>}
      </span>
      {isOpen && (
        <div ref={ref} style={{ position: 'absolute', top: '100%', left: 0, zIndex: 100, background: '#1a1a1a', border: '1px solid #333', borderRadius: 6, minWidth: 140, maxHeight: 260, overflowY: 'auto', boxShadow: '0 4px 16px rgba(0,0,0,.6)', padding: '4px 0' }}>
          {count > 0 && <button onClick={onClear} style={{ display: 'block', width: '100%', textAlign: 'left', padding: '6px 12px', background: 'none', border: 'none', color: '#e05555', fontSize: '0.8rem', cursor: 'pointer', borderBottom: '1px solid #2a2a2a' }}>Сбросить</button>}
          {values.map(([val, cnt]) => (
            <label key={val} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 12px', cursor: 'pointer', fontSize: '0.85rem', background: active.has(val) ? 'rgba(74,144,226,0.12)' : 'none' }}>
              <input type="checkbox" checked={active.has(val)} onChange={() => onToggle(val)} style={{ accentColor: '#4a90e2' }} />
              <span style={{ flex: 1 }}>{val}</span>
              <span style={{ color: '#666', fontSize: '0.75em' }}>{cnt}</span>
            </label>
          ))}
        </div>
      )}
    </th>
  )
}

function SortTh({ label, col, sort, onSort }: { label: string; col: SortKey; sort: { key: SortKey; dir: 'asc' | 'desc' } | null; onSort: (k: SortKey) => void }) {
  const active = sort?.key === col
  return (
    <th onClick={() => onSort(col)} style={{ cursor: 'pointer', userSelect: 'none', color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap' }}>
      {label}{active ? (sort!.dir === 'asc' ? ' ↑' : ' ↓') : ''}
    </th>
  )
}

export default function UsersTodayPage() {
  const [rows, setRows]       = useState<UserRow[]>([])
  const [loading, setLoading] = useState(true)
  const [roleFilter, setRoleFilter] = useState<Set<string>>(new Set())
  const [openCol, setOpenCol] = useState<string | null>(null)
  const [sort, setSort]       = useState<{ key: SortKey; dir: 'asc' | 'desc' } | null>({ key: 'created_at', dir: 'desc' })
  const [deviceSel, setDeviceSel] = useState<Record<number, DeviceItem | null>>({})

  useEffect(() => {
    fetch('/api/admin/users-today')
      .then(r => r.ok ? r.json() : [])
      .then(setRows)
      .finally(() => setLoading(false))
  }, [])

  function toggleSort(key: SortKey) {
    setSort(prev => {
      if (!prev || prev.key !== key) return { key, dir: 'desc' }
      if (prev.dir === 'desc') return { key, dir: 'asc' }
      return null
    })
    setOpenCol(null)
  }

  const roleValues = useMemo(() => {
    const m = new Map<string, number>()
    rows.forEach(r => m.set(r.role, (m.get(r.role) ?? 0) + 1))
    return Array.from(m.entries()).sort((a, b) => b[1] - a[1])
  }, [rows])

  const filtered = useMemo(() =>
    roleFilter.size ? rows.filter(r => roleFilter.has(r.role)) : rows
  , [rows, roleFilter])

  const sorted = useMemo(() => {
    if (!sort) return filtered
    const num = sort.key === 'device_count' || sort.key === 'timecode_count'
    return [...filtered].sort((a, b) => {
      const av = num ? (a[sort.key] as number) : String(a[sort.key])
      const bv = num ? (b[sort.key] as number) : String(b[sort.key])
      if (typeof av === 'number') return sort.dir === 'asc' ? av - (bv as number) : (bv as number) - av
      return sort.dir === 'asc' ? (av as string).localeCompare(bv as string) : (bv as string).localeCompare(av as string)
    })
  }, [filtered, sort])

  const hasFilter = roleFilter.size > 0

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Новые пользователи сегодня
            {hasFilter ? ` (${sorted.length} / ${rows.length})` : rows.length > 0 ? ` (${rows.length})` : ''}
          </h1>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {hasFilter && (
              <button onClick={() => setRoleFilter(new Set())}
                style={{ padding: '4px 10px', borderRadius: 6, border: '1px solid #555', background: 'none', color: '#aaa', fontSize: '0.8rem', cursor: 'pointer' }}>
                Сбросить фильтры
              </button>
            )}
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>Пользователи, зарегистрированные сегодня — {new Date().toLocaleDateString('ru-RU')}.</p>

        {!loading && rows.length > 0 && (
          <div className={styles.mobileControls}>
            <select value={sort ? sort.key : ''} onChange={e => {
              const k = e.target.value as SortKey | ''
              setSort(k ? { key: k, dir: sort?.key === k ? sort.dir : 'desc' } : null)
            }} style={{ background: '#111', border: '1px solid #444', color: '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">— сортировка —</option>
              <option value="username">Имя</option>
              <option value="timecode_count">Таймкодов</option>
              <option value="created_at">Время</option>
            </select>
            {sort && (
              <button onClick={() => setSort(prev => prev ? { ...prev, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : null)}
                style={{ background: 'none', border: '1px solid #444', color: '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem', cursor: 'pointer' }}>
                {sort.dir === 'asc' ? '↑' : '↓'}
              </button>
            )}
            <select value={roleFilter.size === 1 ? [...roleFilter][0] : ''} onChange={e => {
              if (e.target.value) setRoleFilter(new Set([e.target.value]))
              else setRoleFilter(new Set())
            }} style={{ background: '#111', border: '1px solid #444', color: roleFilter.size > 0 ? '#4a90e2' : '#ccc', padding: '5px 8px', borderRadius: 6, fontSize: '0.8rem' }}>
              <option value="">Все роли</option>
              {roleValues.map(([v]) => <option key={v} value={v}>{v}</option>)}
            </select>
          </div>
        )}

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && rows.length === 0 && <div className={styles.empty}>Сегодня новых пользователей нет</div>}
        {!loading && sorted.length === 0 && rows.length > 0 && <div className={styles.empty}>Нет пользователей по фильтру</div>}

        {!loading && sorted.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <SortTh col="username"       label="Имя"        sort={sort} onSort={toggleSort} />
                <FilterHeader label="Роль" values={roleValues} active={roleFilter}
                  isOpen={openCol === 'role'} onToggleOpen={() => setOpenCol(p => p === 'role' ? null : 'role')}
                  onToggle={v => setRoleFilter(prev => { const s = new Set(prev); s.has(v) ? s.delete(v) : s.add(v); return s })}
                  onClear={() => setRoleFilter(new Set())}
                  onSort={() => toggleSort('role')} sortActive={sort?.key === 'role'} sortDir={sort?.dir} />
                <th>Устройства</th>
                <SortTh col="timecode_count" label="Таймкодов"  sort={sort} onSort={toggleSort} />
                <SortTh col="created_at"     label="Время"      sort={sort} onSort={toggleSort} />
              </tr>
            </thead>
            <tbody>
              {sorted.map(u => (
                <tr key={u.id}>
                  <td data-label="Имя" className={styles.cardTitle}>
                    {u.username}{u.is_admin ? ' 👑' : ''}
                  </td>
                  <td data-label="Роль" className={styles.muted}>{u.role}</td>
                  <td data-label="Устройства">
                    <DeviceBadges devices={u.devices}
                      onSelect={d => setDeviceSel(prev => ({ ...prev, [u.id]: d }))} />
                  </td>
                  <td data-label="Таймкодов" className={styles.muted}>
                    {(() => { const n = deviceSel[u.id]?.timecode_count ?? u.timecode_count; return n > 0 ? n.toLocaleString() : '—' })()}
                  </td>
                  <td data-label="Время" className={styles.time}>{u.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Layout>
  )
}
