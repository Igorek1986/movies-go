import { useEffect, useState, useMemo, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './NewCardsPage.module.scss'

interface NewCard {
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
}

type FilterKey = 'media_type' | 'year' | 'language' | 'trackers'

const FILTER_COLS: { key: FilterKey; label: string }[] = [
  { key: 'media_type', label: 'Тип' },
  { key: 'year',       label: 'Год' },
  { key: 'language',   label: 'Язык' },
  { key: 'trackers',   label: 'Трекер' },
]

function getVal(c: NewCard, key: FilterKey): string {
  if (key === 'media_type') return c.media_type === 'movie' ? 'Фильм' : 'Сериал'
  if (key === 'language')   return c.language ? c.language.toUpperCase() : '—'
  return (c[key] as string) || '—'
}

function fmtRuntime(c: NewCard): string {
  const min = c.media_type === 'movie' ? c.runtime : c.episode_run_time
  if (!min) return '—'
  if (min < 60) return `${min} мин`
  const h = Math.floor(min / 60), m = min % 60
  return m ? `${h}ч ${m}м` : `${h}ч`
}

// ── FilterHeader — outside parent to avoid remount on state change ──────────

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
    <th style={{ position: 'relative', userSelect: 'none' }}>
      <span
        onClick={() => onToggleOpen(col.key)}
        style={{ cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 4,
          color: count > 0 ? '#4a90e2' : undefined }}
      >
        {col.label}
        {count > 0 && (
          <span style={{ fontSize: '0.75em', background: '#4a90e2', color: '#fff',
            borderRadius: 8, padding: '0 5px', lineHeight: '1.6' }}>{count}</span>
        )}
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
            }}>
              Сбросить
            </button>
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

// ── Page ──────────────────────────────────────────────────────────────────────

export default function NewCardsPage() {
  const navigate = useNavigate()
  const [cards, setCards]     = useState<NewCard[]>([])
  const [loading, setLoading] = useState(true)
  const [filters, setFilters] = useState<Partial<Record<FilterKey, Set<string>>>>({})
  const [openCol, setOpenCol] = useState<FilterKey | null>(null)

  useEffect(() => {
    fetch('/api/admin/cards-today')
      .then(r => r.ok ? r.json() : [])
      .then(setCards)
      .finally(() => setLoading(false))
  }, [])

  const filtered = useMemo(() => cards.filter(c =>
    FILTER_COLS.every(({ key }) => {
      const active = filters[key]
      if (!active || active.size === 0) return true
      return active.has(getVal(c, key))
    })
  ), [cards, filters])

  const distinctValues = useMemo(() => {
    const result: Partial<Record<FilterKey, [string, number][]>> = {}
    for (const { key } of FILTER_COLS) {
      const vals = new Map<string, number>()
      cards.forEach(c => { const v = getVal(c, key); vals.set(v, (vals.get(v) ?? 0) + 1) })
      const entries = Array.from(vals.entries())
      if (key === 'year') {
        entries.sort(([a], [b]) => {
          if (a === '—') return -1
          if (b === '—') return 1
          return Number(b) - Number(a)
        })
      } else {
        entries.sort((a, b) => b[1] - a[1])
      }
      result[key] = entries
    }
    return result
  }, [cards])

  function toggleOpen(key: FilterKey) {
    setOpenCol(prev => prev === key ? null : key)
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

  const hasFilters = FILTER_COLS.some(c => (filters[c.key]?.size ?? 0) > 0)

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Добавлено сегодня
            {hasFilters
              ? ` (${filtered.length} / ${cards.length})`
              : cards.length > 0 ? ` (${cards.length})` : ''}
          </h1>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {hasFilters && (
              <button onClick={() => setFilters({})} style={{
                padding: '4px 10px', borderRadius: 6, border: '1px solid #e05555',
                background: 'none', color: '#e05555', fontSize: '0.8rem', cursor: 'pointer',
              }}>
                Сбросить всё
              </button>
            )}
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>
          Карточки, добавленные парсером сегодня — {new Date().toLocaleDateString('ru-RU')}.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && cards.length === 0 && <div className={styles.empty}>Сегодня новых карточек нет</div>}
        {!loading && filtered.length === 0 && cards.length > 0 && (
          <div className={styles.empty}>Нет карточек по выбранным фильтрам</div>
        )}

        {!loading && filtered.length > 0 && (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Время</th>
                <FilterHeader col={FILTER_COLS[0]} active={filters.media_type} openCol={openCol}
                  values={distinctValues.media_type ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <th>Название</th>
                <th>Оригинал</th>
                <FilterHeader col={FILTER_COLS[1]} active={filters.year} openCol={openCol}
                  values={distinctValues.year ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <th>Рейтинг</th>
                <th>Длит.</th>
                <FilterHeader col={FILTER_COLS[2]} active={filters.language} openCol={openCol}
                  values={distinctValues.language ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
                <FilterHeader col={FILTER_COLS[3]} active={filters.trackers} openCol={openCol}
                  values={distinctValues.trackers ?? []} onToggleOpen={toggleOpen}
                  onToggleValue={toggleValue} onClear={clearCol} />
              </tr>
            </thead>
            <tbody>
              {filtered.map(c => (
                <tr key={c.card_id} className={styles.row}
                  onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/cards-today' } })}>
                  <td data-label="Время"    className={styles.time}>{c.created_at}</td>
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
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </Layout>
  )
}
