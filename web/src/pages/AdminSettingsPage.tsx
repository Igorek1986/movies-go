import { useEffect, useState, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './AdminSettingsPage.module.scss'

type KWResult = { id: number; name: string }

const SUGGESTED_KEYWORDS: KWResult[] = [
  { id: 281741, name: 'nudity' },
  { id: 354470, name: 'sex scene' },
  { id: 329280, name: 'sexual content' },
  { id: 570,    name: 'rape' },
  { id: 312898, name: 'violence' },
  { id: 10292,  name: 'gore' },
  { id: 13006,  name: 'torture' },
  { id: 11494,  name: 'drug use' },
  { id: 919,    name: 'smoking' },
  { id: 567,    name: 'alcohol' },
  { id: 9826,   name: 'murder' },
  { id: 158718, name: 'lgbt' },
]

function ChildKeywords() {
  const [items, setItems] = useState<KWResult[]>([])
  const [initialLoading, setInitialLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [suggestions, setSuggestions] = useState<KWResult[]>([])
  const [searching, setSearching] = useState(false)
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)

  async function loadAndResolve(showLoading = false) {
    if (showLoading) setInitialLoading(true)
    try {
      const r = await fetch('/api/admin/child-keywords/resolve')
      if (r.ok) setItems(await r.json())
    } finally { setInitialLoading(false) }
  }

  useEffect(() => { loadAndResolve(true) }, [])

  // Close dropdown on outside click
  useEffect(() => {
    function onOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setSuggestions([])
        setSearch('')
      }
    }
    document.addEventListener('mousedown', onOutside)
    return () => document.removeEventListener('mousedown', onOutside)
  }, [])

  function handleSearchChange(val: string) {
    setSearch(val)
    setSuggestions([])
    if (searchTimer.current) clearTimeout(searchTimer.current)
    if (val.trim().length < 2) return
    searchTimer.current = setTimeout(async () => {
      setSearching(true)
      try {
        const r = await fetch(`/api/admin/child-keywords/search?q=${encodeURIComponent(val.trim())}`)
        if (r.ok) setSuggestions(await r.json())
      } finally { setSearching(false) }
    }, 400)
  }

  async function handleAdd(kw: KWResult) {
    setSearch('')
    setSuggestions([])
    // Optimistic update — add immediately, resolve names in background
    setItems(prev => prev.some(i => i.id === kw.id) ? prev : [...prev, kw])
    const r = await fetch('/api/admin/child-keywords', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids: String(kw.id) }),
    })
    if (r.ok) loadAndResolve()
  }

  async function handleDelete(id: number) {
    // Optimistic update — remove immediately
    setItems(prev => prev.filter(i => i.id !== id))
    const r = await fetch('/api/admin/child-keywords', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    })
    if (r.ok) loadAndResolve()
  }

  async function handleReset() {
    if (!confirm('Сбросить к значениям по умолчанию?')) return
    await fetch('/api/admin/child-keywords/reset', { method: 'POST' })
    loadAndResolve(true)
  }

  const listIds = new Set(items.map(i => i.id))
  const suggestedById = Object.fromEntries(SUGGESTED_KEYWORDS.map(k => [k.id, k.name]))
  const displayItems = items.map(i => ({
    ...i,
    name: i.name || suggestedById[i.id] || '',
  }))

  return (
    <details open>
      <summary className={styles.groupSummary}>
        <span className={styles.groupName}>Детский режим — заблокированные TMDB ключевые слова</span>
        <span className={styles.groupArrow}>▶</span>
      </summary>
      <div className={styles.groupBody} style={{ gridColumn: '1 / -1' }}>
        <div style={{ gridColumn: '1 / -1', display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem' }}>
            Каждое TMDB-слово имеет уникальный ID. Карточки с этими тегами скрываются в детских профилях.
            Поиск только на английском — TMDB не поддерживает другие языки для ключевых слов.
          </div>
          <div>
            <div style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)', marginBottom: '6px' }}>Быстрое добавление:</div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '5px' }}>
              {SUGGESTED_KEYWORDS.map(kw => {
                const added = listIds.has(kw.id)
                return (
                  <button
                    key={kw.id}
                    type="button"
                    onClick={() => !added && handleAdd(kw)}
                    disabled={added}
                    style={{
                      padding: '3px 10px', borderRadius: '4px', fontSize: '0.8rem', border: 'none',
                      cursor: added ? 'default' : 'pointer',
                      background: added ? 'rgba(255,255,255,0.07)' : 'rgba(124,140,248,0.15)',
                      color: added ? 'var(--color-text-muted)' : '#7c8cf8',
                    }}
                  >
                    {added ? `✓ ${kw.name}` : `+ ${kw.name}`}
                  </button>
                )
              })}
            </div>
          </div>
          <div ref={dropdownRef} style={{ position: 'relative' }}>
            <input
              type="text"
              className={styles.rowInput}
              placeholder="Поиск на английском: nudity, violence, drug use…"
              value={search}
              onChange={e => handleSearchChange(e.target.value)}
              autoComplete="off"
            />
            {(suggestions.length > 0 || searching) && (
              <div style={{
                position: 'absolute', top: '100%', left: 0, right: 0, zIndex: 200,
                background: '#1a1d27',
                border: '1px solid rgba(255,255,255,0.1)',
                borderRadius: '6px', marginTop: '4px',
                maxHeight: '260px', overflowY: 'auto',
                boxShadow: '0 8px 24px rgba(0,0,0,0.7)',
              }}>
                {searching && (
                  <div style={{ padding: '10px 14px', color: '#888', fontSize: '0.85rem' }}>Поиск…</div>
                )}
                {suggestions.map(kw => {
                  const added = listIds.has(kw.id)
                  return (
                    <button
                      key={kw.id}
                      type="button"
                      onClick={() => !added && handleAdd(kw)}
                      style={{
                        display: 'flex', width: '100%', alignItems: 'center', justifyContent: 'space-between',
                        padding: '9px 14px', background: 'none', border: 'none',
                        borderBottom: '1px solid rgba(255,255,255,0.05)',
                        cursor: added ? 'default' : 'pointer',
                        color: added ? '#666' : '#e0e0e0',
                        fontSize: '0.85rem', textAlign: 'left', gap: '12px',
                      }}
                    >
                      <span>{kw.name}</span>
                      <span style={{ color: added ? '#555' : '#7c8cf8', whiteSpace: 'nowrap', fontSize: '0.78rem', fontWeight: 500 }}>
                        {added ? '✓ в списке' : '+ добавить'}
                      </span>
                    </button>
                  )
                })}
              </div>
            )}
          </div>
          {initialLoading ? (
            <div style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem' }}>Загрузка…</div>
          ) : items.length === 0 ? (
            <div style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem' }}>Список пуст</div>
          ) : (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
              {displayItems.map(kw => (
                <span key={kw.id} style={{
                  display: 'inline-flex', alignItems: 'center', gap: '4px',
                  background: 'var(--color-warning, #e67e22)', color: '#fff',
                  borderRadius: '4px', padding: '3px 10px', fontSize: '0.82rem',
                }}>
                  {kw.name || `ID ${kw.id}`}
                  <button
                    type="button"
                    onClick={() => handleDelete(kw.id)}
                    style={{ background: 'none', border: 'none', color: '#fff', cursor: 'pointer', padding: '0 2px', lineHeight: 1 }}
                  >×</button>
                </span>
              ))}
            </div>
          )}
          <div>
            <button type="button" className={styles.btnReset} onClick={handleReset}>
              Сбросить к умолчаниям
            </button>
          </div>
        </div>
      </div>
    </details>
  )
}

const AGE_GROUPS = [
  { age: 0,  label: '0–5 лет' },
  { age: 6,  label: '6–11 лет' },
  { age: 12, label: '12–15 лет' },
  { age: 16, label: '16+ лет (дети)' },
  { age: 99, label: 'Взрослые профили' },
]

function ChildTextKeywords() {
  const [list, setList] = useState<string[]>([])
  const [ages, setAges] = useState<number[]>([0])
  const [input, setInput] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    fetch('/api/admin/child-text-keywords').then(r => r.json()).then(setList).catch(() => {})
    fetch('/api/admin/child-text-keyword-ages').then(r => r.json()).then(setAges).catch(() => {})
  }, [])

  async function handleAdd() {
    const val = input.trim()
    if (!val) return
    const r = await fetch('/api/admin/child-text-keywords', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ words: val }),
    })
    if (r.ok) { setList(await r.json()); setInput('') }
    inputRef.current?.focus()
  }

  async function handleDelete(word: string) {
    setList(prev => prev.filter(w => w !== word))
    const r = await fetch('/api/admin/child-text-keywords', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ word }),
    })
    if (r.ok) setList(await r.json())
  }

  async function toggleAge(age: number) {
    const next = ages.includes(age) ? ages.filter(a => a !== age) : [...ages, age]
    setAges(next)
    await fetch('/api/admin/child-text-keyword-ages', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ages: next }),
    })
  }

  return (
    <details open>
      <summary className={styles.groupSummary}>
        <span className={styles.groupName}>Детский режим — блокировка по словам в названии/описании</span>
        <span className={styles.groupArrow}>▶</span>
      </summary>
      <div className={styles.groupBody} style={{ gridColumn: '1 / -1' }}>
        <div style={{ gridColumn: '1 / -1', display: 'flex', flexDirection: 'column', gap: '12px' }}>
          <div style={{ color: 'var(--color-text-muted)', fontSize: '0.8rem' }}>
            Карточки, в названии или описании которых встречается слово, скрываются. Слова на русском — названия у нас переведены.
          </div>
          <div>
            <div style={{ fontSize: '0.78rem', color: 'var(--color-text-muted)', marginBottom: '6px' }}>
              Применять для возрастных групп:
            </div>
            <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
              {AGE_GROUPS.map(g => (
                <label key={g.age} style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.85rem' }}>
                  <input
                    type="checkbox"
                    checked={ages.includes(g.age)}
                    onChange={() => toggleAge(g.age)}
                  />
                  {g.label}
                </label>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', gap: '8px' }}>
            <input
              ref={inputRef}
              type="text"
              className={styles.rowInput}
              placeholder="Слово или фраза, например: секс, наркотик — через запятую"
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
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
              {list.map(w => (
                <span key={w} style={{
                  display: 'inline-flex', alignItems: 'center', gap: '4px',
                  background: 'rgba(192,57,43,0.8)', color: '#fff',
                  borderRadius: '4px', padding: '3px 8px', fontSize: '0.82rem',
                }}>
                  {w}
                  <button
                    type="button"
                    onClick={() => handleDelete(w)}
                    style={{ background: 'none', border: 'none', color: '#fff', cursor: 'pointer', padding: '0 2px', lineHeight: 1 }}
                  >×</button>
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
    </details>
  )
}

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

const PLACEHOLDERS: Record<string, string> = {
  base_url:           'https://yourdomain.com',
  plugin_url:         'https://yourdomain.com/np.js (пусто = base_url + /np.js)',
  donate_url:         'https://yourdomain.com/donate',
  popular_source_url: 'https://yourdomain.com/popular',
  myshows_api_url: 'https://myshows.me/v3/rpc/',
  myshows_auth_url:'https://myshows.me/api/session',
  contact_email:   'admin@example.com',
}

const SELECT_KEYS: Record<string, string[]> = {
  app_mode: ['parser', 'all'],
}

const CHECKBOX_KEYS: Record<string, string> = {
  yandex_metrika_enabled:   'yandex_metrika_id',
  google_analytics_enabled: 'google_analytics_id',
  catalog_require_poster:   '',
}

const DESCRIPTIONS: Record<string, string> = {
  popular_source_url: 'URL публичного парсера для категории «Популярное» и форвардинга play events. Если не задан — Популярное берётся из локальной БД.',
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
  base_url:                'Base URL сайта',
  plugin_url:              'Plugin URL (np.js)',
  donate_url:              'Donate URL',
  popular_source_url:      'Popular Source URL',
  myshows_api_url:         'MyShows API URL',
  myshows_auth_url:        'MyShows Auth URL',
  tmdb_refresh_new_year_delta: 'Новые карточки — глубина (лет)',
  tmdb_refresh_old_batch:      'Старые карточки — batch в сутки',
  tmdb_refresh_age_days:       'Старые карточки — мин. возраст для обновления (дней)',
  parser_overlap_days:     'Парсер — перекрытие дат (дней)',
  movies_new_year_delta:   'Новые фильмы — за сколько лет (YearDelta)',
  movies_4k_year_delta:    'Новые 4K фильмы — за сколько лет (YearDelta)',
  movies_new_min_quality:  'Новые фильмы — мин. качество (0=любое, 100=720p+, 200=1080p+, 300=4K+)',
  catalog_require_poster:  'Скрывать карточки без постера',
  catalog_actor_count:     'Актёры в каталоге — глобальных (0 = выкл)',
  catalog_actor_ru_count:  'Актёры в каталоге — русскоязычных (0 = выкл)',
  yandex_metrika_enabled:   'Яндекс.Метрика — включена',
  yandex_metrika_id:        'Яндекс.Метрика ID',
  google_analytics_enabled: 'Google Analytics — включена',
  google_analytics_id:      'Google Analytics ID',
  site_name:              'Название сервиса',
  contact_email:          'Контактный email',
  privacy_policy_content: 'Политика обработки персональных данных (HTML)',
  consent_content:        'Согласие на обработку персональных данных (HTML)',
  app_mode:               'Режим работы',
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
  { name: 'Сайт', keys: ['base_url', 'plugin_url', 'donate_url', 'popular_source_url'] },
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
  { name: 'MyShows', keys: ['myshows_api_url', 'myshows_auth_url'] },
  { name: 'TMDB обновление карточек', keys: [
    'tmdb_refresh_new_year_delta', 'tmdb_refresh_old_batch', 'tmdb_refresh_age_days',
  ]},
  { name: 'Категории парсера', keys: [
    'movies_new_year_delta', 'movies_new_min_quality', 'movies_4k_year_delta',
  ], requiresRestart: true },
  { name: 'Настройки каталога', keys: [
    'catalog_require_poster',
  ]},
  { name: 'Режим работы', keys: ['app_mode'], requiresRestart: true },
]

// ── Component ─────────────────────────────────────────────────────────────────

export default function AdminSettingsPage() {
  const [values, setValues] = useState<Record<string, string>>({})
  const [original, setOriginal] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [activeDesc, setActiveDesc] = useState<string | null>(null)
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
    <Layout wide>
      <form className={styles.page} onSubmit={handleSave}>

        <div className={styles.header}>
          <h1 className={styles.title}>Настройки приложения</h1>
          <div className={styles.headerActions}>
            <button type="button" className={styles.btnRestart} onClick={handleRestart} disabled={restarting}>
              {restarting ? 'Перезапуск…' : 'Перезапустить сервис'}
            </button>
            <Link to="/admin" className={styles.backLink}>Админ</Link>
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

                    const desc = DESCRIPTIONS[key]
                    return (
                      <>
                        <label key={key + '_label'} className={styles.rowLabel}>
                          {label}
                          {desc && (
                            <span style={{ position: 'relative', display: 'inline-flex', alignItems: 'center' }}>
                              <span
                                title={desc}
                                onClick={() => setActiveDesc(activeDesc === key ? null : key)}
                                style={{ marginLeft: '5px', cursor: 'help', opacity: 0.5, fontSize: '0.8em', userSelect: 'none' }}
                              >ⓘ</span>
                              {activeDesc === key && (
                                <span style={{
                                  position: 'absolute', left: '1.4em', top: '50%', transform: 'translateY(-50%)',
                                  background: 'var(--color-bg-elevated, #2a2a2a)', color: 'var(--color-text, #eee)',
                                  border: '1px solid var(--color-border, #444)', borderRadius: '6px',
                                  padding: '6px 10px', fontSize: '0.78rem', lineHeight: '1.4',
                                  whiteSpace: 'normal', width: '240px', zIndex: 10, boxShadow: '0 2px 8px rgba(0,0,0,0.4)',
                                }}>
                                  {desc}
                                </span>
                              )}
                            </span>
                          )}
                        </label>
                        <input
                          key={key}
                          type="text"
                          className={styles.rowInput}
                          value={val}
                          placeholder={PLACEHOLDERS[key] ?? ''}
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
            <ChildKeywords />
            <ChildTextKeywords />

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
