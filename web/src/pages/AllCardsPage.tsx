import { useEffect, useState, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './NewCardsPage.module.scss'

interface Card {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  original_title: string
  year: string
  vote_average: number
  vote_count: number
  created_at: string
  trackers: string
  language: string
  runtime: number
  episode_run_time: number
  best_video_quality: number
  category: string
  categories: string[]
  latest_torrent_date: string
  release_date: string
}

type FilterKey = 'media_type' | 'year' | 'language' | 'trackers'

const FILTER_COLS: { key: FilterKey; label: string }[] = [
  { key: 'media_type', label: 'Тип' },
  { key: 'year',       label: 'Год' },
  { key: 'language',   label: 'Язык' },
  { key: 'trackers',   label: 'Трекер' },
]

function fmtRuntime(c: Card): string {
  const min = c.media_type === 'movie' ? c.runtime : c.episode_run_time
  if (!min) return '—'
  if (min < 60) return `${min} мин`
  const h = Math.floor(min / 60), m = min % 60
  return m ? `${h}ч ${m}м` : `${h}ч`
}

interface RuntimeRange { min: string; max: string }
interface DateRange { from: string; to: string }

// ── FilterHeader ──────────────────────────────────────────────────────────────

interface FilterHeaderProps {
  col: typeof FILTER_COLS[number]
  active: Set<string> | undefined
  openCol: FilterKey | null
  values: [string, number][]
  onToggleOpen: (key: FilterKey) => void
  onToggleValue: (key: FilterKey, val: string) => void
  onClear: (key: FilterKey) => void
}

function FilterHeader({ col, active, openCol, values, onToggleOpen, onToggleValue, onClear }: FilterHeaderProps) {
  const count  = active?.size ?? 0
  const isOpen = openCol === col.key
  const ref    = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen) return
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onToggleOpen(col.key)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [isOpen, col.key, onToggleOpen])

  return (
    <th style={{ userSelect: 'none' }}>
      <span onClick={() => onToggleOpen(col.key)} style={{
        cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 4,
        color: count > 0 ? '#4a90e2' : undefined,
      }}>
        {col.label}
        {count > 0 && <span style={{ fontSize: '0.75em', background: '#4a90e2', color: '#fff',
          borderRadius: 8, padding: '0 5px', lineHeight: '1.6' }}>{count}</span>}
        <span style={{ fontSize: '0.7em', opacity: 0.6 }}>{isOpen ? '▲' : '▼'}</span>
      </span>
      {isOpen && (
        <div ref={ref} style={{
          position: 'absolute', top: '100%', left: 0, zIndex: 100,
          background: '#1a1a1a', border: '1px solid #333', borderRadius: 6,
          minWidth: 140, maxHeight: 260, overflowY: 'auto',
          boxShadow: '0 4px 16px rgba(0,0,0,.6)', padding: '4px 0',
        }}>
          {count > 0 && (
            <button onClick={() => onClear(col.key)} style={{
              display: 'block', width: '100%', textAlign: 'left',
              padding: '6px 12px', background: 'none', border: 'none',
              color: '#e05555', fontSize: '0.8rem', cursor: 'pointer',
              borderBottom: '1px solid #2a2a2a',
            }}>Сбросить</button>
          )}
          {values.map(([val, cnt]) => {
            const checked = active?.has(val) ?? false
            return (
              <label key={val} style={{
                display: 'flex', alignItems: 'center', gap: 8,
                padding: '6px 12px', cursor: 'pointer', fontSize: '0.85rem',
                background: checked ? 'rgba(74,144,226,0.12)' : 'none',
              }}>
                <input type="checkbox" checked={checked}
                  onChange={() => onToggleValue(col.key, val)}
                  style={{ accentColor: '#4a90e2' }} />
                <span style={{ flex: 1 }}>{val}</span>
                <span style={{ color: '#666', fontSize: '0.75em' }}>{cnt}</span>
              </label>
            )
          })}
        </div>
      )}
    </th>
  )
}

// ── RuntimeFilterHeader ───────────────────────────────────────────────────────

function RuntimeFilterHeader({ range, isOpen, onToggleOpen, onChange, onClear }: {
  range: RuntimeRange
  isOpen: boolean
  onToggleOpen: () => void
  onChange: (r: RuntimeRange) => void
  onClear: () => void
}) {
  const active = range.min !== '' || range.max !== ''
  const ref    = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen) return
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onToggleOpen()
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [isOpen, onToggleOpen])

  const label = active
    ? range.min && range.max ? `${range.min}–${range.max}м`
      : range.min ? `≥${range.min}м` : `≤${range.max}м`
    : 'Длит.'

  const inputStyle: React.CSSProperties = {
    flex: 1, background: '#111', border: '1px solid #444', borderRadius: 4,
    color: '#fff', padding: '4px 8px', fontSize: '0.85rem', width: '100%', outline: 'none',
  }

  return (
    <th style={{ userSelect: 'none' }}>
      <span onClick={onToggleOpen} style={{
        cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 4,
        color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap',
      }}>
        {label}
        <span style={{ fontSize: '0.7em', opacity: 0.6 }}>{isOpen ? '▲' : '▼'}</span>
      </span>
      {isOpen && (
        <div ref={ref} style={{
          position: 'absolute', top: '100%', left: 0, zIndex: 100,
          background: '#1a1a1a', border: '1px solid #333', borderRadius: 6,
          width: 170, boxShadow: '0 4px 16px rgba(0,0,0,.6)',
          padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 8,
        }}>
          {(['min', 'max'] as const).map(k => (
            <div key={k} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: '#888', fontSize: '0.82rem', width: 20 }}>
                {k === 'min' ? 'от' : 'до'}
              </span>
              <input type="number" min={0} placeholder="мин"
                value={range[k]}
                onChange={e => onChange({ ...range, [k]: e.target.value })}
                style={inputStyle} />
            </div>
          ))}
          {active && (
            <button onClick={onClear} style={{
              background: 'none', border: '1px solid #e05555', borderRadius: 4,
              color: '#e05555', fontSize: '0.8rem', cursor: 'pointer', padding: '3px 0',
            }}>Сбросить</button>
          )}
        </div>
      )}
    </th>
  )
}

// ── DateRangeFilterHeader ─────────────────────────────────────────────────────

function DateRangeFilterHeader({ label, range, isOpen, sortDir, onToggleOpen, onChange, onClear, onSort }: {
  label: string
  range: DateRange
  isOpen: boolean
  sortDir: 'asc' | 'desc' | null
  onToggleOpen: () => void
  onChange: (r: DateRange) => void
  onClear: () => void
  onSort: () => void
}) {
  const active = range.from !== '' || range.to !== ''
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen) return
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onToggleOpen()
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [isOpen, onToggleOpen])

  const inputStyle: React.CSSProperties = {
    flex: 1, background: '#111', border: '1px solid #444', borderRadius: 4,
    color: '#fff', padding: '4px 6px', fontSize: '0.82rem', outline: 'none',
  }

  return (
    <th style={{ userSelect: 'none', whiteSpace: 'nowrap' }}>
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
        <span onClick={onSort} style={{ cursor: 'pointer', color: sortDir ? '#4a90e2' : undefined }}>
          {label}{sortDir === 'asc' ? ' ↑' : sortDir === 'desc' ? ' ↓' : ''}
        </span>
        <span onClick={onToggleOpen} style={{ cursor: 'pointer', fontSize: '0.7em', opacity: 0.6,
          color: active ? '#4a90e2' : undefined }}>
          {active ? '●' : '▼'}
        </span>
      </span>
      {isOpen && (
        <div ref={ref} style={{
          position: 'absolute', top: '100%', right: 0, zIndex: 100,
          background: '#1a1a1a', border: '1px solid #333', borderRadius: 6,
          width: 190, boxShadow: '0 4px 16px rgba(0,0,0,.6)',
          padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 8,
        }}>
          {(['from', 'to'] as const).map(k => (
            <div key={k} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: '#888', fontSize: '0.82rem', width: 20 }}>
                {k === 'from' ? 'от' : 'до'}
              </span>
              <input type="date" value={range[k]}
                onChange={e => onChange({ ...range, [k]: e.target.value })}
                style={inputStyle} />
            </div>
          ))}
          {active && (
            <button onClick={onClear} style={{
              background: 'none', border: '1px solid #e05555', borderRadius: 4,
              color: '#e05555', fontSize: '0.8rem', cursor: 'pointer', padding: '3px 0',
            }}>Сбросить</button>
          )}
        </div>
      )}
    </th>
  )
}

// ── EditableDate ─────────────────────────────────────────────────────────────

function EditableDate({ cardId, field, value, onSaved }: {
  cardId: string
  field: 'latest_torrent_date' | 'release_date'
  value: string
  onSaved: (cardId: string, field: string, newVal: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [val, setVal] = useState(value.slice(0, 10))
  const [saving, setSaving] = useState(false)

  async function save() {
    if (!val) { setEditing(false); return }
    setSaving(true)
    await fetch(`/api/admin/cards/${cardId}/dates`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ [field]: val }),
    })
    setSaving(false)
    setEditing(false)
    onSaved(cardId, field, val)
  }

  if (editing) {
    return (
      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }} onClick={e => e.stopPropagation()}>
        <input type="date" value={val} onChange={e => setVal(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') save(); if (e.key === 'Escape') setEditing(false) }}
          autoFocus
          style={{ background: '#111', border: '1px solid #4a90e2', borderRadius: 4,
            color: '#fff', padding: '2px 6px', fontSize: '0.8rem', outline: 'none' }} />
        <button onClick={save} disabled={saving}
          style={{ background: '#4a90e2', border: 'none', borderRadius: 4, color: '#fff',
            padding: '2px 6px', fontSize: '0.75rem', cursor: 'pointer' }}>
          {saving ? '…' : '✓'}
        </button>
        <button onClick={() => setEditing(false)}
          style={{ background: 'none', border: 'none', color: '#888', cursor: 'pointer', padding: '2px' }}>✕</button>
      </span>
    )
  }

  const display = val || '—'
  return (
    <span onClick={e => { e.stopPropagation(); setEditing(true) }}
      style={{ cursor: 'pointer', borderBottom: '1px dashed #555', fontSize: '0.82rem',
        color: val ? '#ccc' : '#555' }}
      title="Нажмите чтобы изменить">
      {display}
    </span>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

const PAGE_SIZE = 100

export default function AllCardsPage({ noRuntime }: { noRuntime?: 'movie' | 'tv' } = {}) {
  const navigate = useNavigate()
  const basePath = noRuntime === 'movie' ? '/admin/no-runtime-movies'
    : noRuntime === 'tv' ? '/admin/no-runtime-tv'
    : '/admin/all-cards'
  const pageTitle = noRuntime === 'movie' ? 'Фильмы без runtime'
    : noRuntime === 'tv' ? 'Сериалы без runtime'
    : 'Все карточки'
  const [cards, setCards]       = useState<Card[]>([])
  const [total, setTotal]       = useState(0)
  const [loading, setLoading]   = useState(true)
  const [distinctValues, setDistinctValues] = useState<Partial<Record<FilterKey, [string, number][]>>>({})
  const [filters, setFilters]   = useState<Partial<Record<FilterKey, Set<string>>>>({})
  const [openCol, setOpenCol]   = useState<FilterKey | null>(null)
  const [runtimeRange, setRuntimeRange] = useState<RuntimeRange>({ min: '', max: '' })
  const [runtimeOpen, setRuntimeOpen]   = useState(false)
  const [torrentDateRange, setTorrentDateRange] = useState<DateRange>({ from: '', to: '' })
  const [torrentDateOpen, setTorrentDateOpen]   = useState(false)
  const [releaseDateRange, setReleaseDateRange] = useState<DateRange>({ from: '', to: '' })
  const [releaseDateOpen, setReleaseDateOpen]   = useState(false)
  const [dateSort, setDateSort] = useState<{ key: 'latest_torrent_date' | 'release_date'; dir: 'asc' | 'desc' } | null>({ key: 'latest_torrent_date', dir: 'desc' })
  const [filterDrawer, setFilterDrawer] = useState(false)
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleting, setDeleting] = useState(false)
  const [confirm, setConfirm]   = useState(false)
  const [page, setPage]         = useState(1)
  const [refreshKey, setRefreshKey] = useState(0)
  const [searchInput, setSearchInput] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    const handler = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [])

  useEffect(() => {
    if (!filterDrawer) return
    const scrollY = window.scrollY
    document.body.style.position = 'fixed'
    document.body.style.top = `-${scrollY}px`
    document.body.style.width = '100%'
    document.body.style.overflowY = 'scroll'
    return () => {
      document.body.style.position = ''
      document.body.style.top = ''
      document.body.style.width = ''
      document.body.style.overflowY = ''
      window.scrollTo(0, scrollY)
    }
  }, [filterDrawer])

  // Load distinct filter values once
  useEffect(() => {
    fetch('/api/admin/all-cards/meta')
      .then(r => r.ok ? r.json() : {})
      .then((d: Record<string, [string, number][]>) => setDistinctValues({
        media_type: d.media_type ?? [],
        year:       d.year ?? [],
        language:   d.language ?? [],
        trackers:   d.trackers ?? [],
      }))
  }, [])

  // Reset page when filters/search change
  useEffect(() => { setPage(1) }, [filters, searchQuery, runtimeRange, torrentDateRange, releaseDateRange])

  // Fetch cards from server
  useEffect(() => {
    const ctrl = new AbortController()
    const q = new URLSearchParams()
    q.set('page', String(page))
    q.set('per_page', String(PAGE_SIZE))
    if (searchQuery) q.set('search', searchQuery)
    if (dateSort) { q.set('sort_by', dateSort.key); q.set('sort_dir', dateSort.dir) }
    ;(filters.media_type ?? new Set()).forEach(v => q.append('media_type', v))
    ;(filters.year ?? new Set()).forEach(v => q.append('year', v))
    ;(filters.language ?? new Set()).forEach(v => q.append('language', v))
    ;(filters.trackers ?? new Set()).forEach(v => q.append('trackers', v))
    if (runtimeRange.min) q.set('runtime_min', runtimeRange.min)
    if (runtimeRange.max) q.set('runtime_max', runtimeRange.max)
    if (torrentDateRange.from) q.set('torrent_date_from', torrentDateRange.from)
    if (torrentDateRange.to)   q.set('torrent_date_to', torrentDateRange.to)
    if (releaseDateRange.from) q.set('release_date_from', releaseDateRange.from)
    if (releaseDateRange.to)   q.set('release_date_to', releaseDateRange.to)
    if (noRuntime) q.set('no_runtime', noRuntime)

    setLoading(true)
    fetch('/api/admin/all-cards?' + q, { signal: ctrl.signal })
      .then(r => r.ok ? r.json() : { cards: [], total: 0 })
      .then(d => { setCards(d.cards ?? []); setTotal(d.total ?? 0) })
      .catch(() => {})
      .finally(() => setLoading(false))

    return () => ctrl.abort()
  }, [page, searchQuery, filters, runtimeRange, torrentDateRange, releaseDateRange, dateSort, refreshKey, noRuntime])

  function handleSearch(val: string) {
    setSearchInput(val)
    if (searchTimer.current) clearTimeout(searchTimer.current)
    searchTimer.current = setTimeout(() => {
      setSearchQuery(val.length >= 3 ? val.trim().toLowerCase() : '')
    }, 300)
  }

  function toggleOpen(key: FilterKey) {
    setOpenCol(prev => prev === key ? null : key)
    setRuntimeOpen(false)
  }
  function toggleValue(key: FilterKey, value: string) {
    setFilters(prev => {
      const set = new Set(prev[key] ?? [])
      set.has(value) ? set.delete(value) : set.add(value)
      return { ...prev, [key]: set }
    })
  }
  function clearCol(key: FilterKey) {
    setFilters(prev => ({ ...prev, [key]: new Set() }))
  }

  function toggleDateSort(key: 'latest_torrent_date' | 'release_date') {
    setDateSort(prev => {
      if (!prev || prev.key !== key) return { key, dir: 'desc' }
      if (prev.dir === 'desc') return { key, dir: 'asc' }
      return null
    })
  }

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const allPageSelected = cards.length > 0 && cards.every(c => selected.has(c.card_id))

  function toggleSelectAll() {
    if (allPageSelected) {
      setSelected(prev => { const s = new Set(prev); cards.forEach(c => s.delete(c.card_id)); return s })
    } else {
      setSelected(prev => new Set([...prev, ...cards.map(c => c.card_id)]))
    }
  }
  function toggleSelect(id: string) {
    setSelected(prev => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s })
  }

  function handleDateSaved(cardId: string, field: string, newVal: string) {
    setCards(prev => prev.map(c => c.card_id === cardId ? { ...c, [field]: newVal } : c))
  }

  async function deleteSelected() {
    setDeleting(true)
    setConfirm(false)
    try {
      await fetch('/api/admin/cards', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ card_ids: [...selected] }),
      })
      setSelected(new Set())
      setRefreshKey(k => k + 1)
    } finally {
      setDeleting(false)
    }
  }

  const hasFilters = FILTER_COLS.some(c => (filters[c.key]?.size ?? 0) > 0) ||
    runtimeRange.min !== '' || runtimeRange.max !== '' || searchQuery !== '' ||
    torrentDateRange.from !== '' || torrentDateRange.to !== '' ||
    releaseDateRange.from !== '' || releaseDateRange.to !== ''

  const activeFilterCount = FILTER_COLS.reduce((n, c) => n + (filters[c.key]?.size ?? 0), 0) +
    (runtimeRange.min !== '' || runtimeRange.max !== '' ? 1 : 0) +
    (torrentDateRange.from !== '' || torrentDateRange.to !== '' ? 1 : 0) +
    (releaseDateRange.from !== '' || releaseDateRange.to !== '' ? 1 : 0)

  function resetAll() {
    setFilters({}); setRuntimeRange({ min: '', max: '' }); setSearchInput(''); setSearchQuery('')
    setTorrentDateRange({ from: '', to: '' }); setReleaseDateRange({ from: '', to: '' })
  }

  const selectedCount = selected.size

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            {pageTitle}
            {total > 0 ? ` (${total.toLocaleString()})` : ''}
          </h1>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            {selectedCount > 0 && !confirm && (
              <button onClick={() => setConfirm(true)} style={{
                padding: '4px 12px', borderRadius: 6, border: '1px solid #e05555',
                background: 'none', color: '#e05555', fontSize: '0.82rem', cursor: 'pointer',
              }}>
                Удалить ({selectedCount})
              </button>
            )}
            {confirm && (
              <>
                <span style={{ fontSize: '0.82rem', color: '#e05555' }}>
                  Удалить {selectedCount} карточек?
                </span>
                <button onClick={deleteSelected} disabled={deleting} style={{
                  padding: '4px 12px', borderRadius: 6, border: 'none',
                  background: '#e05555', color: '#fff', fontSize: '0.82rem', cursor: 'pointer',
                }}>
                  {deleting ? 'Удаление…' : 'Да, удалить'}
                </button>
                <button onClick={() => setConfirm(false)} style={{
                  padding: '4px 12px', borderRadius: 6, border: '1px solid #444',
                  background: 'none', color: '#aaa', fontSize: '0.82rem', cursor: 'pointer',
                }}>Отмена</button>
              </>
            )}
            {hasFilters && (
              <button onClick={resetAll} style={{
                padding: '4px 10px', borderRadius: 6, border: '1px solid #555',
                background: 'none', color: '#aaa', fontSize: '0.8rem', cursor: 'pointer',
              }}>Сбросить фильтры</button>
            )}
            {isMobile && (
              <button onClick={() => setFilterDrawer(true)} style={{
                padding: '4px 10px', borderRadius: 6, border: '1px solid #555',
                background: activeFilterCount > 0 ? 'rgba(74,144,226,0.15)' : 'none',
                borderColor: activeFilterCount > 0 ? '#4a90e2' : '#555',
                color: activeFilterCount > 0 ? '#7ab4f5' : '#aaa', fontSize: '0.8rem', cursor: 'pointer',
              }}>
                Фильтры{activeFilterCount > 0 ? ` (${activeFilterCount})` : ''}
              </button>
            )}
            {isMobile && cards.length > 0 && (
              <button onClick={toggleSelectAll} style={{
                padding: '4px 10px', borderRadius: 6, border: '1px solid #555',
                background: allPageSelected ? 'rgba(74,144,226,0.15)' : 'none',
                borderColor: allPageSelected ? '#4a90e2' : '#555',
                color: allPageSelected ? '#7ab4f5' : '#aaa', fontSize: '0.8rem', cursor: 'pointer',
              }}>
                {allPageSelected ? 'Снять выбор' : `Выбрать страницу`}
              </button>
            )}
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>Все медиакарточки в базе.</p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && total === 0 && <div className={styles.empty}>Нет карточек по фильтрам</div>}

        {!loading && cards.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th style={{ width: 32 }}>
                  <input type="checkbox" checked={allPageSelected}
                    onChange={toggleSelectAll} style={{ accentColor: '#4a90e2', cursor: 'pointer' }} />
                </th>
                <FilterHeader col={FILTER_COLS[0]} active={filters.media_type} openCol={openCol}
                  values={distinctValues.media_type ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <th>
                  <div style={{ position: 'relative', display: 'inline-flex', alignItems: 'center' }}>
                    <input
                      placeholder="Название…"
                      value={searchInput}
                      onChange={e => handleSearch(e.target.value)}
                      onClick={e => e.stopPropagation()}
                      style={{ background: 'transparent', border: 'none', borderBottom: `1px solid ${searchQuery ? '#4a90e2' : '#555'}`,
                        color: '#fff', padding: '2px 20px 2px 0', fontSize: '0.82rem',
                        outline: 'none', width: 130, fontWeight: 400 }}
                    />
                    {searchInput
                      ? <button onClick={e => { e.stopPropagation(); setSearchInput(''); setSearchQuery('') }}
                          style={{ position: 'absolute', right: 0, background: 'none', border: 'none',
                            color: '#888', cursor: 'pointer', fontSize: '0.85rem', padding: 0, lineHeight: 1 }}>✕</button>
                      : <span style={{ position: 'absolute', right: 0, color: '#555', fontSize: '0.75rem', pointerEvents: 'none' }}>🔍</span>
                    }
                  </div>
                </th>
                <th>Оригинал</th>
                <FilterHeader col={FILTER_COLS[1]} active={filters.year} openCol={openCol}
                  values={distinctValues.year ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <th>Рейтинг</th>
                <RuntimeFilterHeader
                  range={runtimeRange} isOpen={runtimeOpen}
                  onToggleOpen={() => { setRuntimeOpen(o => !o); setOpenCol(null) }}
                  onChange={setRuntimeRange}
                  onClear={() => setRuntimeRange({ min: '', max: '' })} />
                <FilterHeader col={FILTER_COLS[2]} active={filters.language} openCol={openCol}
                  values={distinctValues.language ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <FilterHeader col={FILTER_COLS[3]} active={filters.trackers} openCol={openCol}
                  values={distinctValues.trackers ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <th>Добавлено</th>
                <DateRangeFilterHeader label="Дата торрента"
                  range={torrentDateRange} isOpen={torrentDateOpen}
                  sortDir={dateSort?.key === 'latest_torrent_date' ? dateSort.dir : null}
                  onToggleOpen={() => { setTorrentDateOpen(o => !o); setReleaseDateOpen(false); setOpenCol(null); setRuntimeOpen(false) }}
                  onChange={setTorrentDateRange} onClear={() => setTorrentDateRange({ from: '', to: '' })}
                  onSort={() => toggleDateSort('latest_torrent_date')} />
                <DateRangeFilterHeader label="Дата релиза"
                  range={releaseDateRange} isOpen={releaseDateOpen}
                  sortDir={dateSort?.key === 'release_date' ? dateSort.dir : null}
                  onToggleOpen={() => { setReleaseDateOpen(o => !o); setTorrentDateOpen(false); setOpenCol(null); setRuntimeOpen(false) }}
                  onChange={setReleaseDateRange} onClear={() => setReleaseDateRange({ from: '', to: '' })}
                  onSort={() => toggleDateSort('release_date')} />
              </tr>
            </thead>
            <tbody>
              {cards.map(c => (
                <tr key={c.card_id}
                  className={styles.row}
                  style={selected.has(c.card_id) ? { background: 'rgba(74,144,226,0.08)' } : undefined}
                  onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: basePath } })}>
                  <td style={{ padding: 0 }}>
                    <label onClick={e => e.stopPropagation()}
                      style={{ display: 'flex', alignItems: 'center', justifyContent: 'center',
                        padding: '8px 6px', cursor: 'pointer', height: '100%' }}>
                      <input type="checkbox" checked={selected.has(c.card_id)}
                        onChange={() => toggleSelect(c.card_id)}
                        style={{ accentColor: '#4a90e2', cursor: 'pointer' }} />
                    </label>
                  </td>
                  <td data-label="Тип"      className={styles.type}>{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                  <td data-label="Название" className={styles.cardTitle}>{c.title}</td>
                  <td data-label="Оригинал" className={styles.muted}>{c.original_title !== c.title ? c.original_title : '—'}</td>
                  <td data-label="Год"      className={styles.year}>{c.year || '—'}</td>
                  <td data-label="Рейтинг"  className={styles.rating}>
                    {c.vote_count > 0 ? `${c.vote_average.toFixed(1)} (${c.vote_count})` : '—'}
                  </td>
                  <td data-label="Длит."    className={styles.muted}>{fmtRuntime(c)}</td>
                  <td data-label="Язык"     className={styles.muted}>{c.language ? c.language.toUpperCase() : '—'}</td>
                  <td data-label="Трекер"   className={styles.muted}>{c.trackers || '—'}</td>
                  <td data-label="Добавлено" className={styles.time}>{c.created_at}</td>
                  <td data-label="Дата торрента">
                    <EditableDate cardId={c.card_id} field="latest_torrent_date"
                      value={c.latest_torrent_date} onSaved={handleDateSaved} />
                  </td>
                  <td data-label="Дата релиза" className={styles.muted}>
                    {c.release_date ? c.release_date.slice(0, 10) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {totalPages > 1 && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 16, flexWrap: 'wrap' }}>
            <button onClick={() => setPage(1)} disabled={page === 1} style={pgBtn(page === 1)}>«</button>
            <button onClick={() => setPage(p => Math.max(1, p - 1))} disabled={page === 1} style={pgBtn(page === 1)}>‹</button>
            {Array.from({ length: totalPages }, (_, i) => i + 1)
              .filter(p => p === 1 || p === totalPages || Math.abs(p - page) <= 2)
              .reduce<(number | '…')[]>((acc, p, i, arr) => {
                if (i > 0 && p - (arr[i - 1] as number) > 1) acc.push('…')
                acc.push(p)
                return acc
              }, [])
              .map((p, i) => p === '…'
                ? <span key={`e${i}`} style={{ color: '#666', padding: '0 4px' }}>…</span>
                : <button key={p} onClick={() => setPage(p as number)}
                    style={pgBtn(false, p === page)}>{p}</button>
              )}
            <button onClick={() => setPage(p => Math.min(totalPages, p + 1))} disabled={page === totalPages} style={pgBtn(page === totalPages)}>›</button>
            <button onClick={() => setPage(totalPages)} disabled={page === totalPages} style={pgBtn(page === totalPages)}>»</button>
            <span style={{ color: '#888', fontSize: '0.82rem', marginLeft: 4 }}>
              {(page - 1) * PAGE_SIZE + 1}–{Math.min(page * PAGE_SIZE, total)} из {total.toLocaleString()}
            </span>
          </div>
        )}
      </div>

      {/* ── Mobile filter drawer ── */}
      {filterDrawer && (
        <div style={{ position: 'fixed', inset: 0, zIndex: 300 }}>
          <div onClick={() => setFilterDrawer(false)}
            style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.5)' }} />
          <div style={{
            position: 'absolute', bottom: 0, left: 0, right: 0,
            background: '#1a1a1a', borderRadius: '12px 12px 0 0',
            padding: '16px 16px 32px', maxHeight: '80vh', overflowY: 'auto',
            overflowX: 'hidden',
            display: 'flex', flexDirection: 'column', gap: 20,
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span style={{ fontWeight: 600, fontSize: '1rem' }}>Фильтры</span>
              <button onClick={() => setFilterDrawer(false)}
                style={{ background: 'none', border: 'none', color: '#aaa', fontSize: '1.2rem', cursor: 'pointer' }}>✕</button>
            </div>

            <div>
              <div style={{ fontSize: '0.82rem', color: '#888', marginBottom: 6 }}>Название</div>
              <div style={{ position: 'relative' }}>
                <input placeholder="Поиск…" value={searchInput} onChange={e => handleSearch(e.target.value)}
                  style={{ width: '100%', background: '#111', border: '1px solid #444', borderRadius: 6,
                    color: '#fff', padding: '8px 36px 8px 12px', fontSize: '0.9rem', outline: 'none',
                    boxSizing: 'border-box' }} />
                {searchInput && (
                  <button onClick={() => { setSearchInput(''); setSearchQuery('') }}
                    style={{ position: 'absolute', right: 10, top: '50%', transform: 'translateY(-50%)',
                      background: 'none', border: 'none', color: '#888', cursor: 'pointer',
                      fontSize: '1rem', padding: 0, lineHeight: 1 }}>✕</button>
                )}
              </div>
            </div>

            {FILTER_COLS.map(col => (
              <div key={col.key}>
                <div style={{ fontSize: '0.82rem', color: '#888', marginBottom: 6 }}>{col.label}</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {(distinctValues[col.key] ?? []).map(([val, cnt]) => {
                    const checked = filters[col.key]?.has(val) ?? false
                    return (
                      <button key={val} onClick={() => toggleValue(col.key, val)} style={{
                        padding: '4px 10px', borderRadius: 20, border: '1px solid',
                        borderColor: checked ? '#4a90e2' : '#444',
                        background: checked ? 'rgba(74,144,226,0.2)' : 'none',
                        color: checked ? '#7ab4f5' : '#ccc', fontSize: '0.82rem', cursor: 'pointer',
                      }}>
                        {val} <span style={{ opacity: 0.6, fontSize: '0.75em' }}>{cnt}</span>
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}

            <div>
              <div style={{ fontSize: '0.82rem', color: '#888', marginBottom: 6 }}>Длительность (мин)</div>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                {(['min', 'max'] as const).map((k, i) => (
                  <>
                    {i === 1 && <span key="sep" style={{ color: '#666', flexShrink: 0 }}>—</span>}
                    <input key={k} type="number" placeholder={k === 'min' ? 'от' : 'до'}
                      value={runtimeRange[k]}
                      onChange={e => setRuntimeRange(r => ({ ...r, [k]: e.target.value }))}
                      style={{ flex: 1, minWidth: 0, background: '#111', border: '1px solid #444',
                        borderRadius: 6, color: '#fff', padding: '8px 10px', fontSize: '0.9rem',
                        outline: 'none', boxSizing: 'border-box' }} />
                  </>
                ))}
              </div>
            </div>

            {([
              { label: 'Дата торрента', range: torrentDateRange, set: setTorrentDateRange },
              { label: 'Дата релиза',   range: releaseDateRange, set: setReleaseDateRange },
            ] as const).map(({ label, range, set }) => (
              <div key={label}>
                <div style={{ fontSize: '0.82rem', color: '#888', marginBottom: 6 }}>{label}</div>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <input type="date" value={range.from} onChange={e => set(r => ({ ...r, from: e.target.value }))}
                    style={{ flex: 1, minWidth: 0, background: '#111', border: '1px solid #444',
                      borderRadius: 6, color: '#fff', padding: '6px 8px', fontSize: '0.85rem',
                      outline: 'none', boxSizing: 'border-box' }} />
                  <span style={{ color: '#666', flexShrink: 0 }}>—</span>
                  <input type="date" value={range.to} onChange={e => set(r => ({ ...r, to: e.target.value }))}
                    style={{ flex: 1, minWidth: 0, background: '#111', border: '1px solid #444',
                      borderRadius: 6, color: '#fff', padding: '6px 8px', fontSize: '0.85rem',
                      outline: 'none', boxSizing: 'border-box' }} />
                </div>
              </div>
            ))}

            <div style={{ display: 'flex', gap: 8 }}>
              {hasFilters && (
                <button onClick={() => { resetAll(); setFilterDrawer(false) }} style={{
                  flex: 1, padding: '10px', borderRadius: 8, border: '1px solid #555',
                  background: 'none', color: '#aaa', fontSize: '0.9rem', cursor: 'pointer',
                }}>Сбросить</button>
              )}
              <button onClick={() => setFilterDrawer(false)} style={{
                flex: 1, padding: '10px', borderRadius: 8, border: 'none',
                background: '#4a90e2', color: '#fff', fontSize: '0.9rem', cursor: 'pointer',
              }}>Применить</button>
            </div>
          </div>
        </div>
      )}
    </Layout>
  )
}

function pgBtn(disabled: boolean, active = false): React.CSSProperties {
  return {
    padding: '3px 8px', borderRadius: 4, border: '1px solid',
    borderColor: active ? '#4a90e2' : '#444',
    background: active ? '#4a90e2' : 'none',
    color: disabled ? '#555' : active ? '#fff' : '#ccc',
    cursor: disabled ? 'default' : 'pointer',
    fontSize: '0.82rem', minWidth: 28,
  }
}
