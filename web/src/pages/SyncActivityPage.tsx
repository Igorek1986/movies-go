import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import DailyChart, { type DailyPoint } from '@/components/DailyChart'
import styles from './PopularPage.module.scss'

interface ActivityRow {
  direction: 'pull' | 'push_in'
  dataset: 'cards' | 'events' | 'episode_runtimes'
  peer_name: string
  peer_url: string
  applied: number
  failed: number
  synced_at: string
}

interface ActivityDaily {
  date: string
  total: number
  pull: number
  push: number
}

interface ActivityData {
  days: number
  daily: ActivityDaily[]
  items: ActivityRow[]
}

type TypeFilter = 'all' | 'pull' | 'push_in'

const DATASET_LABEL: Record<string, string> = {
  cards: 'карточки',
  events: 'play-события',
  episode_runtimes: 'runtime серий',
}

function fmtDayFull(date: string): string {
  const [y, m, d] = date.split('-')
  return `${d}.${m}.${y}`
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString('ru', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export default function SyncActivityPage() {
  const [data, setData] = useState<ActivityData | null>(null)
  const [loading, setLoading] = useState(true)
  const [typeFilter, setTypeFilter] = useState<TypeFilter>('all')
  const [selectedDate, setSelectedDate] = useState<string | null>(null)
  const [dayItems, setDayItems] = useState<ActivityRow[] | null>(null)
  const [dayLoading, setDayLoading] = useState(false)

  useEffect(() => {
    fetch('/api/admin/sync-activity')
      .then(r => (r.ok ? r.json() : null))
      .then(setData)
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!selectedDate) { setDayItems(null); return }
    setDayLoading(true)
    let cancelled = false
    fetch(`/api/admin/sync-activity?date=${selectedDate}`)
      .then(r => (r.ok ? r.json() : null))
      .then(d => { if (!cancelled) setDayItems(d?.items ?? []) })
      .finally(() => { if (!cancelled) setDayLoading(false) })
    return () => { cancelled = true }
  }, [selectedDate])

  // DailyChart's fields are generic (plays/viewers/cards) — alias onto our
  // own (total/pull/push) rather than naming the API response after a chart
  // component's internals.
  const daily: DailyPoint[] = useMemo(() => (data?.daily ?? []).map(d => (
    { date: d.date, plays: d.total, viewers: d.pull, cards: d.push }
  )), [data])
  const allItems = selectedDate ? (dayItems ?? []) : (data?.items ?? [])

  const items = useMemo(() => {
    if (typeFilter === 'all') return allItems
    return allItems.filter(i => i.direction === typeFilter)
  }, [allItems, typeFilter])

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>
            Синхронизация{allItems.length > 0 ? ` (${items.length}/${allItems.length})` : ''}
          </h1>
          <div style={{ display: 'flex', gap: 12 }}>
            <Link to="/admin/sync" className={styles.backLink}>Настройки синка</Link>
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>
          Что реально изменилось от синхронизации с пиром — pull (забрали у пира) и push_in
          (пир прислал нам). Исходящий push сюда не попадает — он не меняет данные этого
          инстанса. Имя пира — то, что он сам о себе сообщает (см. «Название этого инстанса»
          в настройках синка), может быть переименовано в любой момент. Окно — последние {data?.days ?? 90} дней.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && daily.length > 0 && (
          <DailyChart
            daily={daily}
            title="Синхронизаций по дням"
            selected={selectedDate}
            onSelect={setSelectedDate}
            formatReadout={d => `${fmtDayFull(d.date).slice(0, 5)}: ${d.plays} · pull ${d.viewers} · push_in ${d.cards}`}
            formatTooltip={d => `${fmtDayFull(d.date)}: ${d.plays} записей активности (pull: ${d.viewers}, push_in: ${d.cards})`}
          />
        )}

        {selectedDate && (
          <p className={styles.filterNote}>
            Показана активность за {fmtDayFull(selectedDate)}.{' '}
            <button className={styles.resetBtn} onClick={() => setSelectedDate(null)}>
              Сбросить
            </button>
          </p>
        )}

        {!loading && dayLoading && <div className={styles.empty}>Загрузка…</div>}
        {!loading && !dayLoading && allItems.length === 0 && (
          <div className={styles.empty}>
            {selectedDate ? 'В этот день синхронизации не было' : 'Пока нет активности синхронизации — sync_peer_url не задан или ещё не было изменений'}
          </div>
        )}

        {!loading && !dayLoading && allItems.length > 0 && (
          <>
            <div className={styles.toolbar}>
              <select className={styles.select} value={typeFilter} onChange={e => setTypeFilter(e.target.value as TypeFilter)}>
                <option value="all">Оба направления</option>
                <option value="pull">Только pull</option>
                <option value="push_in">Только push_in</option>
              </select>
            </div>

            <table className={styles.table}>
              <thead>
                <tr>
                  <th>Направление</th>
                  <th>Данные</th>
                  <th>Пир</th>
                  <th>Применено</th>
                  <th>Ошибок</th>
                  <th>Когда</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it, i) => (
                  <tr key={i} className={styles.row}>
                    <td data-label="Направление">{it.direction === 'pull' ? '⬇ pull' : '⬆ push_in'}</td>
                    <td className={styles.muted} data-label="Данные">{DATASET_LABEL[it.dataset] ?? it.dataset}</td>
                    <td className={styles.cardTitle} data-label="Пир">
                      {it.peer_name || '(без имени)'}
                      {it.peer_url && <span className={styles.muted} style={{ marginLeft: 6, fontSize: '.8em' }}>{it.peer_url}</span>}
                    </td>
                    <td data-label="Применено">{it.applied.toLocaleString('ru')}</td>
                    <td className={it.failed ? undefined : styles.muted} data-label="Ошибок">{it.failed || '—'}</td>
                    <td className={styles.muted} data-label="Когда">{fmtDateTime(it.synced_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {items.length === 0 && <div className={styles.empty}>Ничего не найдено</div>}
          </>
        )}
      </div>
    </Layout>
  )
}
