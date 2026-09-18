import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './ParserNotFoundPage.module.scss'

interface NotFoundItem {
  id: number
  tracker: string
  reason: string
  raw_title: string
  parsed_name: string
  parsed_year: number
  is_movie: boolean
  checked_at: string
}

interface NotFoundResult {
  items: NotFoundItem[]
  total: number
}

const TRACKERS = ['rutor', 'kinozal', 'nnmclub', 'rutracker']
const PER_PAGE = 50

export default function ParserNotFoundPage() {
  const [data, setData] = useState<NotFoundResult>({ items: [], total: 0 })
  const [loading, setLoading] = useState(true)
  const [tracker, setTracker] = useState('')
  const [reason, setReason] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    const params = new URLSearchParams({ page: String(page), per_page: String(PER_PAGE) })
    if (tracker) params.set('tracker', tracker)
    if (reason) params.set('reason', reason)
    if (search) params.set('search', search)
    const t = setTimeout(() => {
      fetch(`/api/admin/not-found?${params}`, { signal: controller.signal })
        .then(r => r.ok ? r.json() : null)
        .then(d => { if (d) setData(d) })
        .finally(() => setLoading(false))
    }, search ? 300 : 0)
    return () => { clearTimeout(t); controller.abort() }
  }, [tracker, reason, search, page])

  const totalPages = Math.max(1, Math.ceil(data.total / PER_PAGE))

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Раздачи без сопоставления с TMDB</h1>
          <Link to="/admin" className={styles.backLink}>Админ</Link>
        </div>

        <p className={styles.desc}>
          Раздачи, которые парсер не смог сопоставить с TMDB — либо кандидат вообще не нашёлся
          («не найдено»), либо нашёлся, но дата релиза в TMDB позже даты раздачи («до релиза», похоже на утечку).
        </p>

        <div className={styles.filters}>
          <select className={styles.select} value={tracker} onChange={e => { setTracker(e.target.value); setPage(1) }}>
            <option value="">Все трекеры</option>
            {TRACKERS.map(t => <option key={t} value={t}>{t}</option>)}
          </select>
          <select className={styles.select} value={reason} onChange={e => { setReason(e.target.value); setPage(1) }}>
            <option value="">Любая причина</option>
            <option value="not_found">Не найдено</option>
            <option value="pre_release">До релиза</option>
          </select>
          <input
            className={styles.search}
            type="text"
            placeholder="Поиск по названию…"
            value={search}
            onChange={e => { setSearch(e.target.value); setPage(1) }}
          />
        </div>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && data.items.length === 0 && (
          <div className={styles.empty}>Ничего не найдено</div>
        )}

        {!loading && data.items.length > 0 && (
          <>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>Трекер</th>
                  <th>Причина</th>
                  <th>Тип</th>
                  <th>Название</th>
                  <th>Год</th>
                  <th>Заголовок</th>
                  <th>Проверено</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map(it => (
                  <tr key={it.id}>
                    <td data-label="Трекер" className={styles.tracker}>{it.tracker}</td>
                    <td data-label="Причина" className={styles.muted}>
                      {it.reason === 'pre_release' ? 'До релиза' : 'Не найдено'}
                    </td>
                    <td data-label="Тип" className={styles.muted}>{it.is_movie ? 'Фильм' : 'Сериал'}</td>
                    <td data-label="Название">{it.parsed_name || '—'}</td>
                    <td data-label="Год" className={styles.year}>{it.parsed_year || '—'}</td>
                    <td data-label="Заголовок" className={styles.rawTitle} title={it.raw_title}>{it.raw_title}</td>
                    <td data-label="Проверено" className={styles.muted}>{it.checked_at}</td>
                  </tr>
                ))}
              </tbody>
            </table>

            <div className={styles.pagination}>
              <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}>← Назад</button>
              <span>Стр. {page} из {totalPages} ({data.total.toLocaleString()})</span>
              <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>Вперёд →</button>
            </div>
          </>
        )}
      </div>
    </Layout>
  )
}
