import { useEffect, useRef, useState, useCallback } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './BotPage.module.scss'

interface BotStatus {
  enabled: boolean
  username: string
}

interface Settings {
  telegram_bot_token: string
  telegram_bot_name: string
  telegram_admin_ids: string
  telegram_use_polling: string
}

interface LogLine {
  t: string
  text: string
  level: string
}

const EMPTY_SETTINGS: Settings = {
  telegram_bot_token: '',
  telegram_bot_name: '',
  telegram_admin_ids: '',
  telegram_use_polling: '0',
}

export default function BotPage() {
  const [status, setStatus] = useState<BotStatus>({ enabled: false, username: '' })
  const [settings, setSettings] = useState<Settings>(EMPTY_SETTINGS)
  const [applying, setApplying] = useState(false)
  const [actionStatus, setActionStatus] = useState('')

  const [logs, setLogs] = useState<LogLine[]>([])
  const [connected, setConnected] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  const loadStatus = useCallback(async () => {
    const r = await fetch('/api/admin/bot/status')
    if (r.ok) setStatus(await r.json())
  }, [])

  const loadSettings = useCallback(async () => {
    const r = await fetch('/api/admin/settings')
    if (!r.ok) return
    const all: Record<string, string> = await r.json()
    setSettings({
      telegram_bot_token:   all.telegram_bot_token   ?? '',
      telegram_bot_name:    all.telegram_bot_name    ?? '',
      telegram_admin_ids:   all.telegram_admin_ids   ?? '',
      telegram_use_polling: all.telegram_use_polling ?? '0',
    })
  }, [])

  useEffect(() => {
    loadStatus()
    loadSettings()
  }, [loadStatus, loadSettings])

  // SSE logs filtered to bot:
  useEffect(() => {
    const es = new EventSource('/api/admin/logs')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (e) => {
      try {
        const line: LogLine = JSON.parse(e.data)
        if (!line.text.includes('bot:')) return
        setLogs(prev => {
          const next = [...prev, line]
          return next.length > 1000 ? next.slice(-1000) : next
        })
      } catch { /* ignore */ }
    }
    return () => es.close()
  }, [])

  // Auto-scroll
  useEffect(() => {
    if (autoScrollRef.current && logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs])

  function handleScroll() {
    const el = logRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    autoScrollRef.current = atBottom
  }

  async function applyAndRestart() {
    setApplying(true)
    setActionStatus('')
    try {
      const sr = await fetch('/api/admin/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(settings),
      })
      if (!sr.ok) throw new Error('Ошибка сохранения: HTTP ' + sr.status)

      const r = await fetch('/api/admin/bot/restart', { method: 'POST' })
      const data = await r.json()
      if (!r.ok) throw new Error(data.error || 'HTTP ' + r.status)

      await loadStatus()
      setActionStatus(data.enabled ? `Бот запущен (@${data.username || settings.telegram_bot_name})` : 'Токен не задан — бот не запущен')
    } catch (e: unknown) {
      setActionStatus('Ошибка: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setApplying(false)
    }
  }

  function fmtTime(iso: string) {
    if (!iso) return ''
    const d = new Date(iso)
    return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }

  return (
    <Layout wide>
      <div className={styles.page}>

        <div className={styles.header}>
          <h1 className={styles.title}>Telegram бот</h1>
          <div className={styles.headerLinks}>
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        {/* Status */}
        <div className={styles.statusBar}>
          <span className={`${styles.statusDot} ${status.enabled ? styles.statusOn : styles.statusOff}`} />
          <span className={styles.statusText}>
            {status.enabled
              ? <>Бот запущен — <strong>@{status.username}</strong></>
              : 'Бот не запущен'
            }
          </span>
        </div>

        {/* Settings */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>Настройки</h2>
          <div className={styles.fieldGrid}>
            <span className={styles.label}>Токен</span>
            <input
              className={styles.input}
              type="text"
              placeholder="1234567890:AAF..."
              value={settings.telegram_bot_token}
              onChange={e => setSettings(s => ({ ...s, telegram_bot_token: e.target.value }))}
              autoComplete="off"
            />
            <span className={styles.label}>Username бота</span>
            <input
              className={styles.input}
              type="text"
              placeholder="mybot (без @)"
              value={settings.telegram_bot_name}
              onChange={e => setSettings(s => ({ ...s, telegram_bot_name: e.target.value }))}
              autoComplete="off"
            />
            <span className={styles.label}>ID администраторов</span>
            <input
              className={styles.input}
              type="text"
              placeholder="123456789,987654321"
              value={settings.telegram_admin_ids}
              onChange={e => setSettings(s => ({ ...s, telegram_admin_ids: e.target.value }))}
              autoComplete="off"
            />
            <label className={styles.checkboxRow}>
              <input
                type="checkbox"
                className={styles.checkboxInput}
                checked={settings.telegram_use_polling === '1'}
                onChange={e => setSettings(s => ({ ...s, telegram_use_polling: e.target.checked ? '1' : '0' }))}
              />
              Polling вместо Webhook
            </label>
          </div>
          <div className={styles.actions}>
            <button
              className={`${styles.btn} ${styles.btnPrimary}`}
              onClick={applyAndRestart}
              disabled={applying}
            >
              {applying ? 'Применение…' : 'Сохранить и запустить'}
            </button>
            {actionStatus && <span className={styles.actionStatus}>{actionStatus}</span>}
          </div>
        </div>

        {/* Logs */}
        <div className={styles.logsSection}>
          <div className={styles.logsHeader}>
            <h2 className={styles.sectionTitle}>Логи бота</h2>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span className={`${styles.connDot} ${connected ? styles.connOn : styles.connOff}`} />
              <span className={styles.connLabel}>{connected ? 'подключено' : 'нет соединения'}</span>
            </div>
          </div>
          <div className={styles.logScroll} ref={logRef} onScroll={handleScroll}>
            {logs.length === 0
              ? <div className={styles.logEmpty}>Нет записей — логи появятся по мере активности бота</div>
              : logs.map((line, i) => (
                <div key={i} className={styles.logLine}>
                  <span style={{ color: 'var(--color-text-muted)', marginRight: 8 }}>{fmtTime(line.t)}</span>
                  {line.text}
                </div>
              ))
            }
          </div>
        </div>

      </div>
    </Layout>
  )
}
