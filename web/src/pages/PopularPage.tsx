import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import DailyChart, { type DailyPoint } from '@/components/DailyChart'
import { posterUrl } from '@/utils/poster'
import styles from './PopularPage.module.scss'

interface PopularCard {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  poster_path: string
  year: string
  viewers: number
  plays: number
  avg_percent: number
  finished_rate: number
}

interface PopularData {
  days: number
  daily: DailyPoint[]
  cards: PopularCard[]
}

type SortKey = 'viewers' | 'plays' | 'avg_percent' | 'finished_rate' | 'year' | 'title'
type SortState = { key: SortKey; dir: 'asc' | 'desc' }
type TypeFilter = 'all' | 'movie' | 'tv'

const LS_KEY = 'popular_local_prefs'

function loadPrefs(): { sort?: SortState; type?: TypeFilter } {
  try { return JSON.parse(localStorage.getItem(LS_KEY) || '{}') } catch { return {} }
}

function fmtDayFull(date: string): string {
  const [y, m, d] = date.split('-')
  return `${d}.${m}.${y}`
}

function SortableTh({ label, k, sort, onSort, className, title }: {
  label: string
  k: SortKey
  sort: SortState
  onSort: (k: SortKey) => void
  className?: string
  title?: string
}) {
  const active = sort.key === k
  return (
    <th className={`${className ?? ''} ${styles.sortable}`} onClick={() => onSort(k)} title={title}>
      {label}{active && <span className={styles.sortArrow}>{sort.dir === 'asc' ? ' ↑' : ' ↓'}</span>}
    </th>
  )
}

export default function PopularPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<PopularData | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState<TypeFilter>(() => loadPrefs().type ?? 'all')
  const [sort, setSort] = useState<SortState>(() => loadPrefs().sort ?? { key: 'viewers', dir: 'desc' })
  // Daily-chart filter: a selected day restricts the card list to that date.
  const [selectedDate, setSelectedDate] = useState<string | null>(null)
  const [dayCards, setDayCards] = useState<PopularCard[] | null>(null)
  const [dayLoading, setDayLoading] = useState(false)

  useEffect(() => {
    fetch('/api/admin/popular')
      .then(r => (r.ok ? r.json() : null))
      .then(setData)
      .finally(() => setLoading(false))
  }, [])

  // Refetch the card ranking for the selected day (chart filter).
  useEffect(() => {
    if (!selectedDate) { setDayCards(null); return }
    setDayLoading(true)
    let cancelled = false
    fetch(`/api/admin/popular?date=${selectedDate}`)
      .then(r => (r.ok ? r.json() : null))
      .then(d => { if (!cancelled) setDayCards(d?.cards ?? []) })
      .finally(() => { if (!cancelled) setDayLoading(false) })
    return () => { cancelled = true }
  }, [selectedDate])

  useEffect(() => {
    localStorage.setItem(LS_KEY, JSON.stringify({ sort, type: typeFilter }))
  }, [sort, typeFilter])

  const daily = data?.daily ?? []
  const allCards = selectedDate ? (dayCards ?? []) : (data?.cards ?? [])

  function toggleSort(key: SortKey) {
    setSort(prev => prev.key === key
      ? { key, dir: prev.dir === 'desc' ? 'asc' : 'desc' }
      : { key, dir: key === 'title' || key === 'year' ? 'asc' : 'desc' })
  }

  const cards = useMemo(() => {
    const q = search.trim().toLowerCase()
    const list = allCards.filter(c =>
      (typeFilter === 'all' || c.media_type === typeFilter) &&
      (q === '' || c.title.toLowerCase().includes(q))
    )
    const { key, dir } = sort
    const mul = dir === 'asc' ? 1 : -1
    return [...list].sort((a, b) => {
      if (key === 'title') return a.title.localeCompare(b.title, 'ru') * mul
      if (key === 'year') return ((Number(a.year) || 0) - (Number(b.year) || 0)) * mul
      return (((a[key] as number) || 0) - ((b[key] as number) || 0)) * mul
    })
  }, [allCards, search, typeFilter, sort])

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Популярные карточки{allCards.length > 0 ? ` (${cards.length}/${allCards.length})` : ''}
          </h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Локальные просмотры ваших пользователей за последние {data?.days ?? 30} дней —
          учитываются всегда, даже если задан Popular Source URL. Один просмотр на зрителя
          в сутки (с 30% досмотра).
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && daily.length > 0 && (
          <DailyChart
            daily={daily}
            title="Динамика просмотров по дням"
            selected={selectedDate}
            onSelect={setSelectedDate}
          />
        )}

        {selectedDate && (
          <p className={styles.filterNote}>
            Показаны просмотры за {fmtDayFull(selectedDate)}.{' '}
            <button className={styles.resetBtn} onClick={() => setSelectedDate(null)}>
              Сбросить
            </button>
          </p>
        )}

        {!loading && dayLoading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && !dayLoading && allCards.length === 0 && (
          <div className={styles.empty}>
            {selectedDate ? 'В этот день просмотров не было' : 'Пока нет данных о просмотрах'}
          </div>
        )}

        {!loading && !dayLoading && allCards.length > 0 && (
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
              <div className={styles.mobileSort}>
                <select
                  className={styles.select}
                  value={sort.key}
                  onChange={e => setSort(s => ({ key: e.target.value as SortKey, dir: s.dir }))}
                >
                  <option value="viewers">Зрителей</option>
                  <option value="plays">Просмотров</option>
                  <option value="avg_percent">Средний %</option>
                  <option value="finished_rate">Финал</option>
                  <option value="year">Год</option>
                  <option value="title">Название</option>
                </select>
                <button
                  className={styles.dirBtn}
                  onClick={() => setSort(s => ({ key: s.key, dir: s.dir === 'asc' ? 'desc' : 'asc' }))}
                  title="Направление сортировки"
                >
                  {sort.dir === 'asc' ? '↑ возр.' : '↓ убыв.'}
                </button>
              </div>
            </div>

            <table className={styles.table}>
              <thead>
                <tr>
                  <th className={styles.rank}>#</th>
                  <th></th>
                  <SortableTh label="Название" k="title" sort={sort} onSort={toggleSort} className={styles.titleCol} />
                  <SortableTh label="Год" k="year" sort={sort} onSort={toggleSort} />
                  <th>Тип</th>
                  <SortableTh label="Зрителей" k="viewers" sort={sort} onSort={toggleSort} className={styles.num} />
                  <SortableTh label="Просмотров" k="plays" sort={sort} onSort={toggleSort} className={styles.num} />
                  <SortableTh label="Средний %" k="avg_percent" sort={sort} onSort={toggleSort} className={styles.num} title="Средняя глубина просмотра" />
                  <SortableTh label="Финал" k="finished_rate" sort={sort} onSort={toggleSort} className={styles.num} title="Доля просмотров, досмотренных до конца (≥85%)" />
                </tr>
              </thead>
              <tbody>
                {cards.map((c, i) => {
                  const poster = posterUrl(c.poster_path, 'w92')
                  return (
                    <tr
                      key={c.card_id}
                      className={styles.row}
                      onClick={() => navigate(`/card/${c.card_id}`, { state: { backUrl: '/admin/popular' } })}
                    >
                      <td className={styles.rank}>{i + 1}</td>
                      <td className={styles.posterCell}>
                        {poster
                          ? <img src={poster} alt="" className={styles.poster} loading="lazy" />
                          : <div className={styles.posterPlaceholder} />}
                      </td>
                      <td className={styles.cardTitle}>{c.title}</td>
                      <td className={styles.muted} data-label="Год">{c.year || '—'}</td>
                      <td className={styles.muted} data-label="Тип">{c.media_type === 'movie' ? 'Фильм' : 'Сериал'}</td>
                      <td className={`${styles.num} ${styles.numStrong}`} data-label="Зрителей">{c.viewers.toLocaleString('ru')}</td>
                      <td className={`${styles.num} ${styles.muted}`} data-label="Просмотров">{c.plays.toLocaleString('ru')}</td>
                      <td className={`${styles.num} ${c.avg_percent ? '' : styles.muted}`} data-label="Средний %">{c.avg_percent ? `${c.avg_percent}%` : '—'}</td>
                      <td className={`${styles.num} ${c.avg_percent ? '' : styles.muted}`} data-label="Финал">{c.avg_percent ? `${c.finished_rate}%` : '—'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            {cards.length === 0 && <div className={styles.empty}>Ничего не найдено</div>}
          </>
        )}
      </div>
    </Layout>
  )
}
