import { useEffect, useState, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './AdminSettingsPage.module.scss'

function BannedPatterns() {
  const [list, setList] = useState<string[]>([])
  const [input, setInput] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    fetch('/api/admin/banned-patterns').then(r => r.json()).then(setList).catch(() => {})
  }, [])

  async function handleAdd() {
    const val = input.trim()
    if (!val) return
    const r = await fetch('/api/admin/banned-patterns', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ patterns: val }),
    })
    if (r.ok) { setList(await r.json()); setInput('') }
    inputRef.current?.focus()
  }

  async function handleDelete(pattern: string) {
    if (!confirm(`Удалить «${pattern}»?`)) return
    const r = await fetch('/api/admin/banned-patterns', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pattern }),
    })
    if (r.ok) setList(await r.json())
  }

  async function handleClearAll() {
    if (!confirm('Очистить весь список заблокированных доменов?')) return
    for (const p of list) {
      await fetch('/api/admin/banned-patterns', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pattern: p }),
      })
    }
    setList([])
  }

  return (
    <details open>
      <summary className={styles.groupSummary}>
        <span className={styles.groupName}>Безопасность — заблокированные домены</span>
        <span className={styles.groupArrow}>▶</span>
      </summary>
      <div className={styles.groupBody} style={{ gridColumn: '1 / -1' }}>
        <div style={{ gridColumn: '1 / -1', display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div style={{ display: 'flex', gap: '8px' }}>
            <input
              ref={inputRef}
              type="text"
              className={styles.rowInput}
              placeholder="example.ru example — через пробел или запятую. example заблокирует всё содержащее это слово"
              value={input}
              onChange={e => setInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && (e.preventDefault(), handleAdd())}
              autoComplete="off"
            />
            <button type="button" className={styles.btnSave} style={{ whiteSpace: 'nowrap' }} onClick={handleAdd}>
              Добавить
            </button>
          </div>
          {list.length === 0 ? (
            <div style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem' }}>Список пуст</div>
          ) : (
            <>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
                {list.map(p => (
                  <span key={p} style={{
                    display: 'inline-flex', alignItems: 'center', gap: '4px',
                    background: 'var(--color-danger, #c0392b)', color: '#fff',
                    borderRadius: '4px', padding: '3px 8px', fontSize: '0.82rem',
                  }}>
                    {p}
                    <button
                      type="button"
                      onClick={() => handleDelete(p)}
                      style={{ background: 'none', border: 'none', color: '#fff', cursor: 'pointer', padding: '0 2px', lineHeight: 1 }}
                    >×</button>
                  </span>
                ))}
              </div>
              <div>
                <button type="button" className={styles.btnReset} onClick={handleClearAll}>
                  Очистить список
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </details>
  )
}

// ── Schema (mirrors Go settingsGroupDefs / settingLabels) ─────────────────────

const TEXTAREA_KEYS = new Set(['privacy_policy_content', 'consent_content'])

const SELECT_KEYS: Record<string, string[]> = {
  app_mode: ['parser', 'all'],
}

const CHECKBOX_KEYS: Record<string, string> = {
  yandex_metrika_enabled:   'yandex_metrika_id',
  google_analytics_enabled: 'google_analytics_id',
}

const LABELS: Record<string, string> = {
  simple_device_limit:   'Simple — устройств',
  simple_profile_limit:  'Simple — профилей',
  simple_timecode_limit: 'Simple — таймкодов на профиль',
  simple_favorite_limit: 'Simple — закладок на категорию',
  simple_import_daily:   'Simple — импортов в сутки',
  premium_device_limit:    'Premium — устройств',
  premium_profile_limit:   'Premium — профилей',
  premium_timecode_limit:  'Premium — таймкодов на профиль',
  premium_favorite_limit:  'Premium — закладок на категорию',
  premium_import_daily:    'Premium — импортов в сутки',
  premium_myshows_daily:   'Premium — MyShows синков в сутки',
  premium_duration_days:   'Premium — длительность (дней)',
  super_device_limit:   'Super — устройств (0=∞)',
  super_profile_limit:  'Super — профилей (0=∞)',
  super_timecode_limit: 'Super — таймкодов на профиль (0=∞)',
  super_favorite_limit: 'Super — закладок на категорию (0=∞)',
  super_import_daily:   'Super — импортов в сутки (0=∞)',
  super_myshows_daily:  'Super — MyShows синков в сутки (0=∞)',
  episodes_future_threshold: 'Порог будущих серий (меньше — обновляем)',
  episodes_refresh_batch:    'Размер пачки при обновлении',
  episodes_refresh_delay:    'Пауза между пачками (сек)',
  inactive_delete_days:     'Автоудаление неактивных аккаунтов (дней, 0 = выкл)',
  inactive_warn_days:       'Предупреждение об удалении аккаунта (дней до удаления)',
  timecode_grace_days:      'Грейс-период таймкодов (дней)',
  premium_warn_days:        'Предупреждение об истечении Premium (дней)',
  premium_extend_all_days:  'Продлить всем Premium (дней)',
  watched_threshold:        'Порог «просмотрено» (%)',
  popular_period_days:      'Популярное — период (дней)',
  daily_task_hour:          'Час запуска ежедневной задачи (0–23)',
  default_timezone:         'Таймзона по умолчанию',
  session_ttl_days:          'Срок сессии (дней)',
  session_renew_days:        'Продление сессии (дней до истечения)',
  device_token_ttl_days:     'Срок токена устройства (дней)',
  device_code_ttl_minutes:   'TTL кода устройства (мин)',
  telegram_link_ttl_minutes: 'TTL кода Telegram (мин)',
  reset_code_ttl_minutes:    'TTL кода сброса пароля (мин)',
  pending_2fa_ttl_sec:       'Ожидание 2FA (сек)',
  rate_login_max:          'Rate: login — попыток',
  rate_login_window_sec:   'Rate: login — окно (сек)',
  rate_register_max:       'Rate: register — попыток',
  rate_register_window_sec: 'Rate: register — окно (сек)',
  rate_forgot_max:         'Rate: forgot — попыток',
  rate_forgot_window_sec:  'Rate: forgot — окно (сек)',
  rate_2fa_max:            'Rate: 2FA — попыток',
  rate_2fa_window_sec:     'Rate: 2FA — окно (сек)',
  sync_cooldown_sec:       'MyShows cooldown (сек)',
  parser_overlap_days:     'Парсер — перекрытие дат (дней)',
  movies_new_year_delta:   'Новые фильмы — за сколько лет (YearDelta)',
  movies_4k_year_delta:    'Новые 4K фильмы — за сколько лет (YearDelta)',
  movies_new_min_quality:  'Новые фильмы — мин. качество (0=любое, 100=720p+, 200=1080p+, 300=4K+)',
  yandex_metrika_enabled:   'Яндекс.Метрика — включена',
  yandex_metrika_id:        'Яндекс.Метрика ID',
  google_analytics_enabled: 'Google Analytics — включена',
  google_analytics_id:      'Google Analytics ID',
  site_name:              'Название сервиса',
  contact_email:          'Контактный email',
  privacy_policy_content: 'Политика обработки персональных данных (HTML)',
  consent_content:        'Согласие на обработку персональных данных (HTML)',
  app_mode: 'Режим работы',
}

const GROUPS: { name: string; keys: string[]; requiresRestart?: boolean }[] = [
  { name: 'Лимиты Simple', keys: [
    'simple_device_limit', 'simple_profile_limit', 'simple_timecode_limit',
    'simple_favorite_limit', 'simple_import_daily',
  ]},
  { name: 'Лимиты Premium', keys: [
    'premium_device_limit', 'premium_profile_limit', 'premium_timecode_limit',
    'premium_favorite_limit', 'premium_import_daily',
    'premium_myshows_daily', 'premium_duration_days',
  ]},
  { name: 'Лимиты Super (0 = без ограничений)', keys: [
    'super_device_limit', 'super_profile_limit', 'super_timecode_limit',
    'super_favorite_limit', 'super_import_daily', 'super_myshows_daily',
  ]},
  { name: 'Обновление эпизодов', keys: [
    'episodes_future_threshold', 'episodes_refresh_batch', 'episodes_refresh_delay',
  ]},
  { name: 'Общие настройки', keys: [
    'inactive_delete_days', 'inactive_warn_days', 'timecode_grace_days',
    'premium_warn_days', 'premium_extend_all_days', 'watched_threshold',
    'popular_period_days', 'daily_task_hour', 'parser_overlap_days',
    'session_ttl_days', 'session_renew_days', 'device_token_ttl_days',
    'device_code_ttl_minutes', 'telegram_link_ttl_minutes',
    'reset_code_ttl_minutes', 'pending_2fa_ttl_sec',
  ]},
  { name: 'Уведомления', keys: ['default_timezone'] },
  { name: 'Аналитика', keys: [
    'yandex_metrika_enabled', 'yandex_metrika_id',
    'google_analytics_enabled', 'google_analytics_id',
  ]},
  { name: 'Юридические', keys: [
    'site_name', 'contact_email',
    'privacy_policy_content', 'consent_content',
  ]},
  { name: 'Rate Limits', keys: [
    'rate_login_max', 'rate_login_window_sec',
    'rate_register_max', 'rate_register_window_sec',
    'rate_forgot_max', 'rate_forgot_window_sec',
    'rate_2fa_max', 'rate_2fa_window_sec',
    'sync_cooldown_sec',
  ]},
  { name: 'Категории парсера', keys: [
    'movies_new_year_delta', 'movies_new_min_quality', 'movies_4k_year_delta',
  ], requiresRestart: true },
  { name: 'Режим работы', keys: ['app_mode'], requiresRestart: true },
]

// ── Component ─────────────────────────────────────────────────────────────────

export default function AdminSettingsPage() {
  const [values, setValues] = useState<Record<string, string>>({})
  const [original, setOriginal] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [success, setSuccess] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await fetch('/api/admin/settings')
      if (!r.ok) throw new Error('HTTP ' + r.status)
      const data: Record<string, string> = await r.json()
      setValues(data)
      setOriginal(data)
    } catch {
      setError('Ошибка загрузки настроек')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  function setValue(key: string, val: string) {
    setValues(prev => ({ ...prev, [key]: val }))
  }

  function handleReset() {
    setValues(original)
    setSuccess('')
    setError('')
  }

  async function handleRestart() {
    setRestarting(true)
    setSuccess('')
    setError('')
    try {
      await fetch('/api/admin/restart', { method: 'POST' })
      setSuccess('Сервис перезапускается…')
      // Poll /health until the server comes back up
      const goToAdmin = original['app_mode'] === 'parser'
      const poll = async () => {
        for (let i = 0; i < 30; i++) {
          await new Promise(r => setTimeout(r, 1000))
          try {
            const res = await fetch('/health')
            if (res.ok) {
              if (goToAdmin) { window.location.href = '/admin'; return }
              setSuccess('Сервис запущен'); setTimeout(() => setSuccess(''), 3000); return
            }
          } catch { /* still down */ }
        }
        setSuccess('Проверьте статус сервиса вручную')
      }
      poll()
    } catch {
      setError('Ошибка перезапуска')
    } finally {
      setRestarting(false)
    }
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setSuccess('')
    setError('')
    try {
      const r = await fetch('/api/admin/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      })
      if (!r.ok) throw new Error('HTTP ' + r.status)
      setOriginal(values)
      setSuccess('Настройки сохранены')
      window.scrollTo({ top: 0, behavior: 'smooth' })
    } catch {
      setError('Ошибка сохранения')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Layout>
      <form className={styles.page} onSubmit={handleSave}>

        <div className={styles.header}>
          <h1 className={styles.title}>Настройки приложения</h1>
          <div className={styles.headerActions}>
            <button type="button" className={styles.btnRestart} onClick={handleRestart} disabled={restarting}>
              {restarting ? 'Перезапуск…' : 'Перезапустить сервис'}
            </button>
            <Link to="/admin" className={styles.backLink}>← Пользователи</Link>
          </div>
        </div>

        {success && (
          <div className={`${styles.alert} ${styles.alertSuccess}`}>
            <span>{success}</span>
            <button type="button" className={styles.alertClose} onClick={() => setSuccess('')}>×</button>
          </div>
        )}
        {error && (
          <div className={`${styles.alert} ${styles.alertError}`}>
            <span>{error}</span>
            <button type="button" className={styles.alertClose} onClick={() => setError('')}>×</button>
          </div>
        )}

        {loading ? (
          <div style={{ color: 'var(--color-text-muted)', fontSize: '0.875rem' }}>Загрузка…</div>
        ) : (
          <>
            {GROUPS.map(group => (
              <details key={group.name} open>
                <summary className={styles.groupSummary}>
                  <span className={styles.groupName}>
                    {group.name}
                    {group.requiresRestart && (
                      <span className={styles.restartBadge}>⚠ требует перезапуска</span>
                    )}
                  </span>
                  <span className={styles.groupArrow}>▶</span>
                </summary>
                <div className={styles.groupBody}>
                  {group.keys.map(key => {
                    const label = LABELS[key] ?? key
                    const val = values[key] ?? ''
                    const showsKey = CHECKBOX_KEYS[key]

                    if (showsKey !== undefined) {
                      // Checkbox that controls visibility of another field
                      return (
                        <label key={key} className={styles.checkboxRow}>
                          <input
                            type="checkbox"
                            className={styles.checkboxInput}
                            checked={val === '1'}
                            onChange={e => setValue(key, e.target.checked ? '1' : '0')}
                          />
                          {label}
                        </label>
                      )
                    }

                    if (SELECT_KEYS[key]) {
                      return (
                        <>
                          <label key={key + '_label'} className={styles.rowLabel}>{label}</label>
                          <select
                            key={key}
                            className={styles.rowInput}
                            value={val}
                            onChange={e => setValue(key, e.target.value)}
                          >
                            {SELECT_KEYS[key].map(opt => (
                              <option key={opt} value={opt}>{opt}</option>
                            ))}
                          </select>
                        </>
                      )
                    }

                    if (TEXTAREA_KEYS.has(key)) {
                      return (
                        <div key={key} style={{ gridColumn: '1 / -1', display: 'flex', flexDirection: 'column', gap: '4px' }}>
                          <label className={styles.rowLabelFull}>{label}</label>
                          <textarea
                            className={styles.rowTextarea}
                            value={val}
                            rows={10}
                            onChange={e => setValue(key, e.target.value)}
                            autoComplete="off"
                          />
                        </div>
                      )
                    }

                    // Check if this field is controlled by a checkbox
                    const controllingKey = Object.keys(CHECKBOX_KEYS).find(k => CHECKBOX_KEYS[k] === key)
                    const hidden = controllingKey !== undefined && values[controllingKey] !== '1'

                    if (hidden) return null

                    return (
                      <>
                        <label key={key + '_label'} className={styles.rowLabel}>{label}</label>
                        <input
                          key={key}
                          type="text"
                          className={styles.rowInput}
                          value={val}
                          onChange={e => setValue(key, e.target.value)}
                          autoComplete="off"
                        />
                      </>
                    )
                  })}
                </div>
              </details>
            ))}

            <BannedPatterns />

            <div className={styles.footer}>
              <button type="button" className={styles.btnReset} onClick={handleReset}>
                Сбросить изменения
              </button>
              <button type="submit" className={styles.btnSave} disabled={saving}>
                {saving ? 'Сохранение…' : 'Сохранить настройки'}
              </button>
            </div>
          </>
        )}
      </form>
    </Layout>
  )
}
