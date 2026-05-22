import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './ParsersPage.module.scss'
import { useCountdown, useParserStatus, fmtCountdown, fmtDateTime } from '@/hooks/useParserStatus'
import { invalidateCatalogCache } from './CatalogPage'

interface TrackerStatus {
  name: string
  enabled: boolean
  last_parsed_at: string
}

interface ParsersData {
  parsers: TrackerStatus[]
  order: string
  running: boolean
  stop_requested: boolean
  current_tracker: string
  next_run_at: string
  retry_attempts: number
  retry_base_wait: number
  retry_max_wait: number
  retry_ratio: string
  kinozal_login: string
  kinozal_password: string
  catalog_trackers: string
  tracker_cards: Record<string, number>
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

const TRACKER_DOMAINS: Record<string, string> = {
  kinozal: 'kinozal.tv',
  nnmclub: 'nnmclub.to',
  rutor: 'rutor.info',
}

function formatDate(iso: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export default function ParsersPage() {
  const [parserStatus, refreshStatus] = useParserStatus()
  const countdown = useCountdown(parserStatus.nextRunAt)

  const [data, setData] = useState<ParsersData | null>(null)
  const [order, setOrder] = useState<string[]>([])
  const [enabled, setEnabled] = useState<Record<string, boolean>>({})
  const [overlapDays, setOverlapDays] = useState(2)
  const [retryAttempts, setRetryAttempts] = useState(10)
  const [retryBaseWait, setRetryBaseWait] = useState(30)
  const [retryMaxWait, setRetryMaxWait] = useState(120)
  const [retryRatio, setRetryRatio] = useState('2.0')
  const [trackerDates, setTrackerDates] = useState<Record<string, string>>({})
  const [catalogTrackers, setCatalogTrackers] = useState<Set<string>>(new Set())
  const [credModal, setCredModal] = useState<{ tracker: string; login: string; password: string } | null>(null)
  const [credSaving, setCredSaving] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
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

    const ct = d.catalog_trackers ?? 'rutor'
    setCatalogTrackers(new Set(ct.split(',').map(s => s.trim()).filter(Boolean)))

    setTrackerDates(prev => {
      const next = { ...prev }
      for (const p of d.parsers) {
        if (!next[p.name] && p.last_parsed_at)
          next[p.name] = p.last_parsed_at.split('T')[0]
      }
      return next
    })

    if (d.retry_attempts) setRetryAttempts(d.retry_attempts)
    if (d.retry_base_wait) setRetryBaseWait(d.retry_base_wait)
    if (d.retry_max_wait) setRetryMaxWait(d.retry_max_wait)
    if (d.retry_ratio) setRetryRatio(d.retry_ratio)

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

  useEffect(() => {
    if (!credModal) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setCredModal(null) }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [credModal])

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
        setData(prev => prev ? { ...prev, running: true, stop_requested: false } : prev)
        await Promise.all([load(), refreshStatus()])
        startPoll()
      }
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function stopNow() {
    if (!confirm('Остановить парсер?\nТекущий трекер будет завершён.')) return
    try {
      await api('/api/admin/parsers/stop')
      toast('Остановка запрошена — парсер завершит текущий трекер')
      setData(prev => prev ? { ...prev, stop_requested: true } : prev)
      await Promise.all([load(), refreshStatus()])
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function saveSettings() {
    setSaving(true)
    try {
      await api('/api/admin/parsers/settings', 'POST', {
        order: order.join(','),
        overlap_days: overlapDays,
        retry_attempts: retryAttempts,
        retry_base_wait: retryBaseWait,
        retry_max_wait: retryMaxWait,
        retry_ratio: retryRatio,
      })
      toast('Настройки сохранены')
      await load()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    } finally {
      setSaving(false)
    }
  }

  async function runTracker(name: string) {
    try {
      const r = await api(`/api/admin/parsers/${name}/run`)
      if (r.status === 'already_running') {
        toast('Парсер уже запущен')
      } else {
        toast(`${TRACKER_LABELS[name] ?? name}: запущен`)
        setData(prev => prev ? { ...prev, running: true, stop_requested: false, current_tracker: name } : prev)
        await Promise.all([load(), refreshStatus()])
        startPoll()
      }
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
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

  async function setTrackerDate(name: string) {
    const date = trackerDates[name]
    if (!date) return
    try {
      await api(`/api/admin/parsers/${name}/reset`, 'POST', { date })
      toast(`${TRACKER_LABELS[name] ?? name}: дата установлена`)
      setTrackerDates(d => ({ ...d, [name]: date }))
      await load()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  const CRED_TRACKERS = new Set(['kinozal'])

  function openCredModal(name: string) {
    setShowPassword(false)
    setCredModal({
      tracker: name,
      login: data?.kinozal_login ?? '',
      password: data?.kinozal_password ?? '',
    })
  }

  async function saveCredentials() {
    if (!credModal) return
    setCredSaving(true)
    try {
      await api('/api/admin/parsers/settings', 'POST', {
        kinozal_login: credModal.login,
        kinozal_password: credModal.password,
      })
      toast('Данные аккаунта сохранены')
      setCredModal(null)
      await load()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    } finally {
      setCredSaving(false)
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

  async function toggleEnabled(name: string) {
    const newVal = !(enabled[name] ?? true)
    setEnabled(prev => ({ ...prev, [name]: newVal }))
    try {
      await api('/api/admin/parsers/settings', 'POST', { [`${name}_enabled`]: newVal })
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function toggleCatalogTracker(name: string, checked: boolean) {
    const next = new Set(catalogTrackers)
    if (checked) next.add(name); else next.delete(name)
    setCatalogTrackers(next)
    try {
      await api('/api/admin/parsers/settings', 'POST', { catalog_trackers: [...next].join(',') })
      invalidateCatalogCache()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  // Build tracker info map for quick lookup
  const infoMap: Record<string, TrackerStatus> = {}
  for (const p of data?.parsers ?? []) infoMap[p.name] = p

  // Prefer data from local load() — it's refreshed after every action.
  // Fall back to the hook's polling state (e.g. before first load completes).
  const running = data?.running ?? parserStatus.running
  const stopRequested = data?.stop_requested ?? parserStatus.stopRequested

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
          <div className={styles.headerLinks}>
            <Link to="/admin/logs" className={styles.backLink}>Логи</Link>
            <Link to="/admin" className={styles.backLink}>Назад</Link>
          </div>
        </div>

        {/* Status bar */}
        <div className={`${styles.statusBar} ${running ? styles.statusRunning : styles.statusIdle}`}>
          <span className={`${styles.statusDot} ${running ? styles.statusDotRunning : styles.statusDotIdle}`} />
          {running ? (
            stopRequested
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
            {running ? (
              <button className={`${styles.btn} ${styles.btnWarn}`} onClick={stopNow} disabled={stopRequested}>
                {stopRequested ? 'Остановка…' : 'Остановить'}
              </button>
            ) : (
              <button className={`${styles.btn} ${styles.btnPrimary}`} onClick={runNow}>
                Запустить
              </button>
            )}
          </div>
        </div>

        <div className={styles.section}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>Трекеры</h2>
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
                  <th>Карточек</th>
                  <th>Статус</th>
                  <th title="Карточки этого трекера видны в каталоге">В каталоге</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {order.map((name, i) => {
                  const info = infoMap[name]
                  const isEnabled = enabled[name] ?? true
                  const isCurrentlyRunning = running && data?.current_tracker === name
                  return (
                    <tr key={name} className={!isEnabled ? styles.rowDisabled : undefined}>
                      <td className={styles.tdOrder}>
                        <button className={styles.arrowBtn} onClick={() => moveUp(i)} disabled={i === 0}>↑</button>
                        <button className={styles.arrowBtn} onClick={() => moveDown(i)} disabled={i === order.length - 1}>↓</button>
                      </td>
                      <td className={styles.tdName}>{TRACKER_LABELS[name] ?? name}</td>
                      <td className={styles.tdDate}>{formatDate(info?.last_parsed_at ?? '')}</td>
                      <td className={styles.tdCards}>{data?.tracker_cards?.[name]?.toLocaleString('ru-RU') ?? '—'}</td>
                      <td className={styles.tdStatus}>
                        {isCurrentlyRunning
                          ? <><span className={`${styles.dot} ${styles.statusDotRunning}`} />Парсится…</>
                          : <><span className={`${styles.dot} ${isEnabled ? styles.dotOn : styles.dotOff}`} />{isEnabled ? 'Активен' : 'Выключен'}</>
                        }
                      </td>
                      <td className={styles.tdCatalog}>
                        <input
                          type="checkbox"
                          className={styles.catalogCheck}
                          checked={catalogTrackers.has(name)}
                          onChange={e => toggleCatalogTracker(name, e.target.checked)}
                          title={catalogTrackers.has(name) ? 'Карточки видны в каталоге' : 'Карточки скрыты из каталога'}
                        />
                      </td>
                      <td className={styles.tdActions}>
                        <button
                          className={`${styles.btnSm} ${isEnabled ? styles.btnSmWarn : styles.btnSmOk}`}
                          onClick={() => toggleEnabled(name)}
                        >
                          {isEnabled ? 'Выключить' : 'Включить'}
                        </button>
                        <button
                          className={`${styles.btnSm} ${isCurrentlyRunning ? styles.btnSmRunning : styles.btnSmPrimary}`}
                          onClick={() => runTracker(name)}
                          disabled={running}
                          title="Запустить только этот парсер (игнорирует статус включён/выключен)"
                        >
                          {isCurrentlyRunning ? 'Идёт…' : 'Запустить'}
                        </button>
                        <button
                          className={styles.btnSm}
                          onClick={() => resetTracker(name)}
                          title="Сбросить дату — следующий запуск выполнит полное сканирование"
                        >
                          Сбросить
                        </button>
                        <input
                          type="date"
                          className={styles.trackerDateInput}
                          value={trackerDates[name] ?? ''}
                          onChange={e => setTrackerDates(d => ({ ...d, [name]: e.target.value }))}
                          onClick={e => (e.currentTarget as HTMLInputElement).showPicker?.()}
                        />
                        <button
                          className={styles.btnSm}
                          onClick={() => setTrackerDate(name)}
                          disabled={!trackerDates[name]}
                          title="Парсер остановится на этой дате с учётом перекрытия дней"
                        >
                          Установить
                        </button>
                        {CRED_TRACKERS.has(name) && (
                          <button
                            className={styles.btnSm}
                            onClick={() => openCredModal(name)}
                            title="Логин и пароль для авторизации на трекере"
                          >
                            Аккаунт
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}

        </div>

        <div className={styles.section}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>Настройки парсинга</h2>
            <span className={styles.sectionHint}>Применяются после нажатия «Сохранить»</span>
          </div>

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
          </div>

          <h2 className={styles.sectionTitle}>Повторные попытки</h2>
          <p className={styles.hint}>Применяется к листингам Kinozal и NNMClub. Пауза растёт по формуле: base × ratio^n, но не более max.</p>
          <div className={styles.retryGrid}>
            <label className={styles.label}>
              Попыток:
              <input type="number" min={1} max={30} className={styles.numInput}
                value={retryAttempts} onChange={e => setRetryAttempts(Number(e.target.value))} />
            </label>
            <label className={styles.label}>
              Первая пауза (сек):
              <input type="number" min={1} max={600} className={styles.numInput}
                value={retryBaseWait} onChange={e => setRetryBaseWait(Number(e.target.value))} />
            </label>
            <label className={styles.label}>
              Максимальная пауза (сек):
              <input type="number" min={10} max={3600} className={styles.numInput}
                value={retryMaxWait} onChange={e => setRetryMaxWait(Number(e.target.value))} />
            </label>
            <label className={styles.label}>
              Коэффициент роста:
              <input type="number" min={1.0} max={5.0} step={0.1} className={styles.numInput}
                value={retryRatio} onChange={e => setRetryRatio(e.target.value)} />
            </label>
          </div>

          <div className={styles.settingsRow}>
            <button className={styles.btn} onClick={saveSettings} disabled={saving}>
              {saving ? 'Сохранение…' : 'Сохранить'}
            </button>
          </div>
        </div>
      </div>
      {credModal && (
        <div className={styles.modalOverlay} onClick={() => setCredModal(null)}>
          <div className={styles.modalDialog} onClick={e => e.stopPropagation()}>
            <div className={styles.modalSiteHeader}>
              <div className={styles.modalSiteIcon}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
                  <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
                </svg>
              </div>
              <div>
                <div className={styles.modalSiteDomain}>
                  {TRACKER_DOMAINS[credModal.tracker] ?? credModal.tracker}
                </div>
                <div className={styles.modalSiteDesc}>Аккаунт для парсера</div>
              </div>
            </div>
            <form
              className={styles.modalForm}
              onSubmit={e => { e.preventDefault(); saveCredentials() }}
            >
              <label className={styles.modalLabel}>
                Логин
                <input
                  type="text"
                  name="username"
                  className={styles.modalInput}
                  value={credModal.login}
                  onChange={e => setCredModal(m => m && { ...m, login: e.target.value })}
                  autoComplete="off"
                  data-bwignore
                  data-lpignore="true"
                  data-1p-ignore
                  autoFocus
                />
              </label>
              <label className={styles.modalLabel}>
                Пароль
                <div className={styles.passwordWrap}>
                  <input
                    type={showPassword ? 'text' : 'password'}
                    name="password"
                    className={styles.modalInput}
                    value={credModal.password}
                    onChange={e => setCredModal(m => m && { ...m, password: e.target.value })}
                    autoComplete="current-password"
                    data-bwignore
                    data-lpignore="true"
                    data-1p-ignore
                  />
                  <button type="button" className={styles.passwordToggle} onClick={() => setShowPassword(s => !s)} tabIndex={-1}>
                    {showPassword ? (
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94"/>
                        <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19"/>
                        <line x1="1" y1="1" x2="23" y2="23"/>
                      </svg>
                    ) : (
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
                        <circle cx="12" cy="12" r="3"/>
                      </svg>
                    )}
                  </button>
                </div>
              </label>
              <div className={styles.modalFooter}>
                <button type="button" className={styles.btn} onClick={() => setCredModal(null)}>Отмена</button>
                <button type="submit" className={`${styles.btn} ${styles.btnPrimary}`} disabled={credSaving}>
                  {credSaving ? 'Сохранение…' : 'Сохранить'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </Layout>
  )
}
