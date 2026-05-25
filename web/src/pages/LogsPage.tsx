import { useEffect, useRef, useState, useMemo } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './LogsPage.module.scss'
import { useCountdown, useParserStatus, fmtCountdown, fmtDateTime } from '@/hooks/useParserStatus'

interface LogLine {
  t: string
  text: string
  level: 'success' | 'skip' | 'error' | 'info'
}

type Tab = 'all' | 'success' | 'skip' | 'error' | 'info'

const TAB_LABELS: Record<Tab, string> = {
  all:     'Все',
  success: 'Добавлено',
  skip:    'Пропущено',
  error:   'Ошибки',
  info:    'Инфо',
}

const MAX_DISPLAY = 2000

function toDateKey(t: string) {
  return t.slice(0, 10) // YYYY-MM-DD
}

function todayKey() {
  return new Date().toISOString().slice(0, 10)
}

function dayLabel(dateKey: string): string {
  const today = todayKey()
  const yesterday = new Date(Date.now() - 86400000).toISOString().slice(0, 10)
  if (dateKey === today) return 'Сегодня'
  if (dateKey === yesterday) return 'Вчера'
  const [, m, d] = dateKey.split('-')
  const months = ['', 'янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']
  return `${parseInt(d)} ${months[parseInt(m)]}`
}

export default function LogsPage() {
  const [parserStatus, refreshStatus] = useParserStatus()
  const countdown = useCountdown(parserStatus.nextRunAt)

  const [liveLines, setLiveLines] = useState<LogLine[]>([])
  const [histLines, setHistLines] = useState<LogLine[]>([])
  const [histLoading, setHistLoading] = useState(false)
  const [connected, setConnected] = useState(false)
  const [tab, setTab] = useState<Tab>('all')
  const [selectedDay, setSelectedDay] = useState<string>(todayKey())
  const [autoScroll, setAutoScroll] = useState(true)
  const bottomRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)
  const isAutoScrollingRef = useRef(false)

  // SSE for live + today history
  useEffect(() => {
    const es = new EventSource('/api/admin/logs')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (e) => {
      try {
        const line: LogLine = JSON.parse(e.data)
        setLiveLines(prev => {
          const next = [...prev, line]
          return next.length > 30000 ? next.slice(-30000) : next
        })
      } catch { /* ignore */ }
    }
    return () => es.close()
  }, [])

  // Load past day from REST
  useEffect(() => {
    if (selectedDay === todayKey()) {
      setHistLines([])
      return
    }
    setHistLoading(true)
    fetch(`/api/admin/logs/day?date=${selectedDay}`)
      .then(r => r.json())
      .then((d: { lines: LogLine[] }) => setHistLines(d.lines ?? []))
      .catch(() => setHistLines([]))
      .finally(() => setHistLoading(false))
  }, [selectedDay])

  useEffect(() => {
    if (!autoScrollRef.current || !containerRef.current) return
    isAutoScrollingRef.current = true
    containerRef.current.scrollTop = containerRef.current.scrollHeight
    requestAnimationFrame(() => { isAutoScrollingRef.current = false })
  }, [liveLines, histLines, tab, selectedDay])

  function handleScroll() {
    if (isAutoScrollingRef.current) return
    const el = containerRef.current
    if (!el) return
    autoScrollRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60
    setAutoScroll(autoScrollRef.current)
  }

  async function runNow() {
    await fetch('/api/admin/parsers/run', { method: 'POST' })
    refreshStatus()
  }

  async function stopNow() {
    if (!confirm('Остановить парсер?\nТекущий трекер будет завершён.')) return
    await fetch('/api/admin/parsers/stop', { method: 'POST' })
    refreshStatus()
  }

  function scrollToBottom() {
    autoScrollRef.current = true
    setAutoScroll(true)
    bottomRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }

  function switchDay(day: string) {
    setSelectedDay(day)
    setTab('all')
    autoScrollRef.current = true
    setAutoScroll(true)
  }

  function switchTab(t: Tab) {
    setTab(t)
    autoScrollRef.current = true
    setAutoScroll(true)
  }

  const isToday = selectedDay === todayKey()
  const activeLines: LogLine[] = isToday
    ? liveLines.filter(l => toDateKey(l.t) === selectedDay)
    : histLines

  // Available days: today + days seen in liveLines + last 7 days with data
  const availableDays = useMemo(() => {
    const days = new Set<string>()
    const today = todayKey()
    days.add(today)
    for (const l of liveLines) days.add(toDateKey(l.t))
    // Show last 7 calendar days as options (user can click to load)
    for (let i = 1; i < 7; i++) {
      const d = new Date(Date.now() - i * 86400000).toISOString().slice(0, 10)
      days.add(d)
    }
    return [...days].sort().reverse()
  }, [liveLines])

  const counts: Record<Tab, number> = { all: activeLines.length, success: 0, skip: 0, error: 0, info: 0 }
  for (const l of activeLines) counts[l.level] = (counts[l.level] ?? 0) + 1

  const filtered = (tab === 'all' ? activeLines : activeLines.filter(l => l.level === tab))
    .slice(-MAX_DISPLAY)

  return (
    <Layout>
      <div className={styles.page}>
        {/* Header */}
        <div className={styles.header}>
          <h1 className={styles.title}>Логи</h1>
          <div className={styles.headerActions}>
            {isToday && <span className={`${styles.connDot} ${connected ? styles.connOn : styles.connOff}`} />}
            {isToday && <span className={styles.connLabel}>{connected ? 'Live' : 'Отключено'}</span>}
            <button className={styles.btn} onClick={() => { setLiveLines([]); setHistLines([]) }}>Очистить</button>
            {!autoScroll && (
              <button className={`${styles.btn} ${styles.btnPrimary}`} onClick={scrollToBottom}>↓ В конец</button>
            )}
            <Link to="/admin/parsers" className={styles.backLink}>Парсеры</Link>
            <Link to="/admin" className={styles.backLink}>Назад</Link>
          </div>
        </div>

        {/* Parser status bar */}
        <div className={`${styles.statusBar} ${parserStatus.running ? styles.statusRunning : styles.statusIdle}`}>
          <span className={`${styles.statusDot} ${parserStatus.running ? styles.statusDotRunning : styles.statusDotIdle}`} />
          {parserStatus.running ? (
            parserStatus.stopRequested
              ? <span className={styles.statusText}>Остановка после текущего трекера…</span>
              : <span className={styles.statusText}>Парсер запущен</span>
          ) : (
            <span className={styles.statusText}>
              {parserStatus.nextRunAt
                ? <>Ожидает — следующий запуск в <strong>{fmtDateTime(parserStatus.nextRunAt)}</strong> (через {fmtCountdown(countdown)})</>
                : 'Ожидает'}
            </span>
          )}
          <div className={styles.statusActions}>
            {parserStatus.running ? (
              <button
                className={`${styles.btn} ${styles.btnWarn}`}
                disabled={parserStatus.stopRequested}
                onClick={stopNow}
              >
                {parserStatus.stopRequested ? 'Остановка…' : 'Остановить'}
              </button>
            ) : (
              <button className={`${styles.btn} ${styles.btnPrimary}`} onClick={runNow}>
                Запустить
              </button>
            )}
          </div>
        </div>

        {/* Day selector */}
        <div className={styles.tabs}>
          {availableDays.map(day => (
            <button
              key={day}
              className={`${styles.tab} ${selectedDay === day ? styles.tabActive : ''}`}
              onClick={() => switchDay(day)}
            >
              {dayLabel(day)}
            </button>
          ))}
        </div>

        {/* Level tabs */}
        <div className={styles.tabs}>
          {(Object.keys(TAB_LABELS) as Tab[]).map(t => (
            <button
              key={t}
              className={`${styles.tab} ${tab === t ? styles.tabActive : ''} ${t !== 'all' ? styles['tab_' + t] : ''}`}
              onClick={() => switchTab(t)}
            >
              {TAB_LABELS[t]}
              {counts[t] > 0 && (
                <span className={`${styles.badge} ${tab === t ? styles.badgeActive : ''} ${t !== 'all' ? styles['badge_' + t] : ''}`}>
                  {counts[t].toLocaleString()}
                </span>
              )}
            </button>
          ))}
        </div>

        {/* Terminal */}
        <div className={styles.terminal} ref={containerRef} onScroll={handleScroll}>
          {histLoading && <span className={styles.empty}>Загрузка…</span>}
          {!histLoading && filtered.length === 0 && (
            <span className={styles.empty}>
              {activeLines.length === 0 ? 'Нет логов за этот день' : 'Нет записей в этой категории'}
            </span>
          )}
          {!histLoading && filtered.map((line, i) => (
            <div key={i} className={`${styles.line} ${styles['lvl_' + line.level]}`}>
              {line.text}
            </div>
          ))}
          <div ref={bottomRef} />
        </div>
      </div>
    </Layout>
  )
}
