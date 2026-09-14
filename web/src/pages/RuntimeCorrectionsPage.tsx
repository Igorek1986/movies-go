import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import DailyChart, { type DailyPoint } from '@/components/DailyChart'
import { posterUrl } from '@/utils/poster'
import styles from './PopularPage.module.scss'

interface CorrectionRow {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  poster_path: string
  season?: number
  episode?: number
  old_value_sec?: number
  new_value_sec: number
  corrected_at: string
}

interface CorrectionDaily {
  date: string
  total: number
  card: number
  episode: number
}

interface CorrectionsData {
  days: number
  daily: CorrectionDaily[]
  items: CorrectionRow[]
}

type SortKey = 'corrected_at' | 'title'
type SortState = { key: SortKey; dir: 'asc' | 'desc' }
type TypeFilter = 'all' | 'movie' | 'tv'

const LS_KEY = 'runtime_corrections_local_prefs'

function loadPrefs(): { sort?: SortState; type?: TypeFilter } {
  try { return JSON.parse(localStorage.getItem(LS_KEY) || '{}') } catch { return {} }
}

function fmtDayFull(date: string): string {
  const [y, m, d] = date.split('-')
  return `${d}.${m}.${y}`
}

function fmtDuration(sec?: number): string {
  if (!sec) return '—'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString('ru', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function SortableTh({ label, k, sort, onSort, className }: {
  label: string
  k: SortKey
  sort: SortState
  onSort: (k: SortKey) => void
  className?: string
}) {
  const active = sort.key === k
  return (
    <th className={`${className ?? ''} ${styles.sortable}`} onClick={() => onSort(k)}>
      {label}{active && <span className={styles.sortArrow}>{sort.dir === 'asc' ? ' ↑' : ' ↓'}</span>}
    </th>
  )
}

export default function RuntimeCorrectionsPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<CorrectionsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<TypeFilter>(() => loadPrefs().type ?? 'all')
  const [sort, setSort] = useState<SortState>(() => loadPrefs().sort ?? { key: 'corrected_at', dir: 'desc' })
  // Daily-chart filter: a selected day restricts the list to that date.
  const [selectedDate, setSelectedDate] = useState<string | null>(null)
  const [dayItems, setDayItems] = useState<CorrectionRow[] | null>(null)
  const [dayLoading, setDayLoading] = useState(false)

  useEffect(() => {
    fetch('/api/admin/runtime-corrections')
      .then(r => (r.ok ? r.json() : null))
      .then(setData)
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!selectedDate) { setDayItems(null); return }
    setDayLoading(true)
    let cancelled = false
    fetch(`/api/admin/runtime-corrections?date=${selectedDate}`)
      .then(r => (r.ok ? r.json() : null))
      .then(d => { if (!cancelled) setDayItems(d?.items ?? []) })
      .finally(() => { if (!cancelled) setDayLoading(false) })
    return () => { cancelled = true }
  }, [selectedDate])

  useEffect(() => {
    localStorage.setItem(LS_KEY, JSON.stringify({ sort, type: typeFilter }))
  }, [sort, typeFilter])

  // DailyChart's fields are generic (plays/viewers/cards) — alias onto our
  // own (total/card/episode) rather than naming the API response after a
  // chart component's internals.
  const daily: DailyPoint[] = useMemo(() => (data?.daily ?? []).map(d => (
    { date: d.date, plays: d.total, viewers: d.card, cards: d.episode }
  )), [data])
  const allItems = selectedDate ? (dayItems ?? []) : (data?.items ?? [])

  function toggleSort(key: SortKey) {
    setSort(prev => prev.key === key
      ? { key, dir: prev.dir === 'desc' ? 'asc' : 'desc' }
      : { key, dir: key === 'title' ? 'asc' : 'desc' })
  }

  const items = useMemo(() => {
    const q = search.trim().toLowerCase()
    const list = allItems.filter(c =>
      (typeFilter === 'all' || c.media_type === typeFilter) &&
      (q === '' || c.title.toLowerCase().includes(q))
    )
    const { key, dir } = sort
    const mul = dir === 'asc' ? 1 : -1
    return [...list].sort((a, b) => {
      if (key === 'title') return a.title.localeCompare(b.title, 'ru') * mul
      return (new Date(a.corrected_at).getTime() - new Date(b.corrected_at).getTime()) * mul
    })
  }, [allItems, search, typeFilter, sort])

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Runtime из реального просмотра{allItems.length > 0 ? ` (${items.length}/${allItems.length})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Каждая строка — реальная коррекция длительности от плеера при просмотре,
          применённая только когда значение неизвестно или отличается от сохранённого
          более чем на 5%. «Серия» — точечная правка одного эпизода, без неё — правка
          длительности всей карточки. Окно — последние {data?.days ?? 90} дней.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && daily.length > 0 && (
          <DailyChart
            daily={daily}
            title="Коррекций по дням"
            selected={selectedDate}
            onSelect={setSelectedDate}
            formatReadout={d => `${fmtDayFull(d.date).slice(0, 5)}: ${d.plays} испр. · ${d.viewers} карточка · ${d.cards} серия`}
            formatTooltip={d => `${fmtDayFull(d.date)}: ${d.plays} коррекций (${d.viewers} карточка целиком, ${d.cards} по конкретной серии)`}
          />
        )}

        {selectedDate && (
          <p className={styles.filterNote}>
            Показаны коррекции за {fmtDayFull(selectedDate)}.{' '}
            <button className={styles.resetBtn} onClick={() => setSelectedDate(null)}>
              Сбросить
            </button>
          </p>
        )}

        {!loading && dayLoading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && !dayLoading && allItems.length === 0 && (
          <div className={styles.empty}>
            {selectedDate ? 'В этот день коррекций не было' : 'Пока нет коррекций от реального просмотра'}
          </div>
        )}

        {!loading && !dayLoading && allItems.length > 0 && (
          <>
            <div className={styles.toolbar}>
              <input
                className={styles.search}
                placeholder="Поиск по названию…"
                value={search}
                onChange={e => setSearch(e.target.value)}
              />
              <select className={styles.select} value={typeFilter} onChange={e => setTypeFilter(e.target.value as TypeFilter)}>
                <option value="all">Все типы</option>
                <option value="movie">Фильмы</option>
                <option value="tv">Сериалы</option>
              </select>
            </div>

            <table className={styles.table}>
              <thead>
                <tr>
                  <th className={styles.thPoster}></th>
                  <SortableTh label="Название" k="title" sort={sort} onSort={toggleSort} className={styles.titleCol} />
                  <th>Серия</th>
                  <th>Было</th>
                  <th>Стало</th>
                  <SortableTh label="Когда" k="corrected_at" sort={sort} onSort={toggleSort} />
                </tr>
              </thead>
              <tbody>
                {items.map((c, i) => {
                  const poster = posterUrl(c.poster_path, 'w92')
                  return (
                    <tr
                      key={`${c.card_id}-${c.season ?? ''}-${c.episode ?? ''}-${c.corrected_at}-${i}`}
                      className={styles.row}
                      onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/runtime-corrections' } })}
                    >
                      <td className={styles.posterCell}>
                        {poster
                          ? <img src={poster} alt="" className={styles.poster} loading="lazy" />
                          : <div className={styles.posterPlaceholder} />}
                      </td>
                      <td className={styles.cardTitle}>{c.title || `${c.media_type} ${c.tmdb_id}`}</td>
                      <td className={styles.muted} data-label="Серия">
                        {c.season != null && c.episode != null
                          ? `S${c.season}E${c.episode}`
                          : c.media_type === 'movie' ? '—' : '— (сериал целиком)'}
                      </td>
                      <td className={styles.muted} data-label="Было">{fmtDuration(c.old_value_sec)}</td>
                      <td data-label="Стало">{fmtDuration(c.new_value_sec)}</td>
                      <td className={styles.muted} data-label="Когда">{fmtDateTime(c.corrected_at)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            {items.length === 0 && <div className={styles.empty}>Ничего не найдено</div>}
          </>
        )}
      </div>
    </Layout>
  )
}
