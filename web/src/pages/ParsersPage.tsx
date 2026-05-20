import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './ParsersPage.module.scss'

interface TrackerStatus {
  name: string
  enabled: boolean
  last_parsed_at: string
}

interface ParsersData {
  parsers: TrackerStatus[]
  order: string
  running: boolean
}

interface Toast {
  id: number
  text: string
  ok: boolean
}

const TRACKER_LABELS: Record<string, string> = {
  kinozal: 'Kinozal.tv',
  nnmclub: 'NNMClub.to',
  rutor: 'Rutor.info',
}

function formatDate(iso: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export default function ParsersPage() {
  const [data, setData] = useState<ParsersData | null>(null)
  const [order, setOrder] = useState<string[]>([])
  const [enabled, setEnabled] = useState<Record<string, boolean>>({})
  const [overlapDays, setOverlapDays] = useState(2)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

  function toast(text: string, ok = true) {
    const id = Date.now()
    setToasts(prev => [...prev, { id, text, ok }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 3000)
  }

  const load = useCallback(async () => {
    const res = await fetch('/api/admin/parsers')
    if (!res.ok) return
    const d: ParsersData = await res.json()
    setData(d)

    const names = d.order.split(',').map(s => s.trim()).filter(Boolean)
    setOrder(names)

    const map: Record<string, boolean> = {}
    for (const p of d.parsers) map[p.name] = p.enabled
    setEnabled(map)

    if (d.running) startPoll()
    else stopPoll()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function startPoll() {
    if (pollRef.current) return
    pollRef.current = setInterval(load, 3000)
  }

  function stopPoll() {
    if (!pollRef.current) return
    clearInterval(pollRef.current)
    pollRef.current = null
  }

  useEffect(() => {
    setLoading(true)
    load().finally(() => setLoading(false))
    return () => stopPoll()
  }, [load])

  async function api(url: string, method = 'POST', body?: object) {
    const res = await fetch(url, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) {
      const d = await res.json().catch(() => ({}))
      throw new Error(d.error || 'Ошибка')
    }
    return res.json()
  }

  async function runNow() {
    try {
      const r = await api('/api/admin/parsers/run')
      if (r.status === 'already_running') {
        toast('Парсер уже запущен')
      } else {
        toast('Парсер запущен')
        await load()
        startPoll()
      }
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function saveSettings() {
    setSaving(true)
    try {
      await api('/api/admin/parsers/settings', 'POST', {
        order: order.join(','),
        kinozal_enabled: enabled['kinozal'] ?? true,
        nnmclub_enabled: enabled['nnmclub'] ?? true,
        rutor_enabled: enabled['rutor'] ?? false,
        overlap_days: overlapDays,
      })
      toast('Настройки сохранены')
      await load()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    } finally {
      setSaving(false)
    }
  }

  async function resetTracker(name: string) {
    if (!confirm(`Сбросить дату парсинга для ${TRACKER_LABELS[name] ?? name}?\nСледующий запуск выполнит полное сканирование.`)) return
    try {
      await api(`/api/admin/parsers/${name}/reset`)
      toast(`${TRACKER_LABELS[name] ?? name}: дата сброшена`)
      await load()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  function moveUp(i: number) {
    if (i === 0) return
    setOrder(prev => { const a = [...prev]; [a[i - 1], a[i]] = [a[i], a[i - 1]]; return a })
  }

  function moveDown(i: number) {
    if (i === order.length - 1) return
    setOrder(prev => { const a = [...prev]; [a[i], a[i + 1]] = [a[i + 1], a[i]]; return a })
  }

  function toggleEnabled(name: string) {
    setEnabled(prev => ({ ...prev, [name]: !prev[name] }))
  }

  // Build tracker info map for quick lookup
  const infoMap: Record<string, TrackerStatus> = {}
  for (const p of data?.parsers ?? []) infoMap[p.name] = p

  const running = data?.running ?? false

  return (
    <Layout>
      {toasts.length > 0 && (
        <div className={styles.toasts}>
          {toasts.map(t => (
            <div key={t.id} className={`${styles.toast} ${t.ok ? styles.toastOk : styles.toastErr}`}>
              {t.text}
            </div>
          ))}
        </div>
      )}

      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Парсеры</h1>
          <Link to="/admin" className={styles.backLink}>← Назад</Link>
        </div>

        <div className={styles.section}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>Трекеры</h2>
            <button
              className={`${styles.btn} ${running ? styles.btnRunning : styles.btnPrimary}`}
              onClick={runNow}
              disabled={running}
            >
              {running ? 'Запущен…' : 'Запустить'}
            </button>
          </div>

          {running && (
            <div className={styles.runningBanner}>
              Парсинг выполняется — страница обновляется автоматически
            </div>
          )}

          {loading && <p className={styles.empty}>Загрузка…</p>}

          {!loading && order.length > 0 && (
            <table className={styles.table}>
              <thead>
                <tr>
                  <th className={styles.thOrder}>Порядок</th>
                  <th>Трекер</th>
                  <th>Последний запуск</th>
                  <th>Статус</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {order.map((name, i) => {
                  const info = infoMap[name]
                  const isEnabled = enabled[name] ?? true
                  return (
                    <tr key={name} className={!isEnabled ? styles.rowDisabled : undefined}>
                      <td className={styles.tdOrder}>
                        <button className={styles.arrowBtn} onClick={() => moveUp(i)} disabled={i === 0}>↑</button>
                        <button className={styles.arrowBtn} onClick={() => moveDown(i)} disabled={i === order.length - 1}>↓</button>
                      </td>
                      <td className={styles.tdName}>{TRACKER_LABELS[name] ?? name}</td>
                      <td className={styles.tdDate}>{formatDate(info?.last_parsed_at ?? '')}</td>
                      <td className={styles.tdStatus}>
                        <span className={`${styles.dot} ${isEnabled ? styles.dotOn : styles.dotOff}`} />
                        {isEnabled ? 'Активен' : 'Выключен'}
                      </td>
                      <td className={styles.tdActions}>
                        <button
                          className={`${styles.btnSm} ${isEnabled ? styles.btnSmWarn : styles.btnSmOk}`}
                          onClick={() => toggleEnabled(name)}
                        >
                          {isEnabled ? 'Выключить' : 'Включить'}
                        </button>
                        <button
                          className={styles.btnSm}
                          onClick={() => resetTracker(name)}
                          title="Сбросить дату — следующий запуск выполнит полное сканирование"
                        >
                          Сбросить дату
                        </button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}

          <div className={styles.settingsRow}>
            <label className={styles.label}>
              Перекрытие (дней):
              <input
                type="number"
                min={0}
                max={30}
                className={styles.numInput}
                value={overlapDays}
                onChange={e => setOverlapDays(Number(e.target.value))}
              />
            </label>
            <button className={styles.btn} onClick={saveSettings} disabled={saving}>
              {saving ? 'Сохранение…' : 'Сохранить'}
            </button>
          </div>
        </div>
      </div>
    </Layout>
  )
}
