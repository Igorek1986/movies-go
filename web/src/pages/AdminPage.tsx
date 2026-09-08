import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { loadTTLCache, saveTTLCache } from '@/utils/ttlCache'
import { ADMIN_STATS_CACHE_KEY, ADMIN_STATS_TTL_MS } from '@/utils/adminStatsCache'
import styles from './AdminPage.module.scss'

interface AdminUser {
  id: number
  username: string
  role: string
  is_admin: boolean
  created_at: string
  blocked_at: string | null
  block_reason: string | null
  premium_until: string | null
  device_count: number
}

interface StatRow {
  name: string
  requests: number
}

interface Stats {
  users: number
  users_today: number
  devices: number
  devices_today: number
  media_cards: number
  media_cards_today: number
  timecodes: number
  timecodes_today: number
  no_runtime_movies: number
  no_runtime_tv: number
  tmdb_refreshed_today: number
  tmdb_not_found: number
  actor_count: number
  director_count: number
  popular_cards: number
  popular_source_url: string
  popular_source_count: number
  image_cache_bytes: number
  image_cache_files: number
  api_ips_today: number
  api_reqs_today: number
  api_today: StatRow[]
  api_total: StatRow[]
  cats_today: StatRow[]
  cats_total: StatRow[]
  myshows_today: StatRow[]
  myshows_total: StatRow[]
}

// Non-breaking space keeps the number and unit on one line inside the
// narrow stat card (was wrapping "2859.2 МБ" onto two lines); switching to
// ГБ past 1024 МБ also keeps the string short enough to fit at 28px.
function formatCacheSize(bytes: number): string {
  const mb = bytes / 1024 / 1024
  if (mb >= 1024) return `${(mb / 1024).toFixed(2)} ГБ`
  return `${mb.toFixed(1)} МБ`
}

type RequestsTab = 'today' | 'all'

function RequestsTable({ rows, cols }: { rows: StatRow[]; cols: [string, string] }) {
  const total = rows.reduce((s, r) => s + r.requests, 0)
  if (rows.length === 0) {
    return <p className={styles.emptyText}>Нет данных</p>
  }
  return (
    <table className={styles.statTable}>
      <thead>
        <tr>
          <th>{cols[0]}</th>
          <th>{cols[1]}</th>
          <th>Доля</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={i}>
            <td>{r.name}</td>
            <td className={styles.muted}>{r.requests.toLocaleString('ru')}</td>
            <td className={styles.muted}>
              {total > 0 ? ((r.requests / total) * 100).toFixed(1) + '%' : '—'}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function RequestsSection({
  title, tab, onTab, todayContent, allContent,
}: {
  title: string
  tab: RequestsTab
  onTab: (t: RequestsTab) => void
  todayContent: React.ReactNode
  allContent: React.ReactNode
}) {
  return (
    <div className={styles.requestsBlock}>
      <div className={styles.sectionHeader}>
        <h3 className={styles.requestsTitle}>{title}</h3>
        <div className={styles.tabs}>
          <button className={`${styles.tab}${tab === 'today' ? ' ' + styles.tabActive : ''}`} onClick={() => onTab('today')}>Сегодня</button>
          <button className={`${styles.tab}${tab === 'all' ? ' ' + styles.tabActive : ''}`} onClick={() => onTab('all')}>Всё время</button>
        </div>
      </div>
      {tab === 'today' ? todayContent : allContent}
    </div>
  )
}

interface SystemStats {
  uptime_days: number
  uptime_hours: number
  uptime_minutes: number
  goroutines: number
  memory_mb: number
  num_cpu: number
}

interface Toast {
  id: number
  text: string
  ok: boolean
}

interface UsersPaged {
  total: number
  page: number
  per_page: number
  items: AdminUser[]
}

// Внешние источники runtime-фолбэков (see internal/tasks/fix_runtime.go).
// Токены хранятся отдельно от app_settings и не попадают в бэкап — см.
// db/postgres/schema.sql (external_source_tokens) и scripts/backup.sh.
const EXT_SOURCE_INFO: Record<string, { label: string; hint: string }> = {
  tvmaze: { label: 'TVmaze', hint: 'Сериалы, полностью бесплатно, ключ не нужен' },
  thetvdb: { label: 'TheTVDB', hint: 'Фильмы и сериалы, нужен API-ключ' },
  poiskkino: { label: 'poiskkino.dev', hint: 'Кинопоиск + TMDB + IMDb, нужен ключ, лимит 200 запросов/сутки' },
  kinopoisk_unofficial: { label: 'Kinopoisk Api Unofficial', hint: 'Только фильмы, нужен ключ, лимит 500 запросов/сутки' },
}

export default function AdminPage() {
  const [usersPaged, setUsersPaged] = useState<UsersPaged | null>(null)
  const [usersPage, setUsersPage]   = useState(1)
  const [usersSearch, setUsersSearch] = useState('')
  const [usersQuery, setUsersQuery]   = useState('')
  const [usersSortBy, setUsersSortBy]   = useState('created_at')
  const [usersSortDir, setUsersSortDir] = useState<'asc' | 'desc'>('desc')
  const [usersPerPage, setUsersPerPage] = useState(10)
  const [confirmingCreateUser, setConfirmingCreateUser] = useState(false)
  const [creatingUser, setCreatingUser] = useState(false)
  const [newUserCreds, setNewUserCreds] = useState<{ username: string; password: string } | null>(null)
  const usersTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Seeded from the shared localStorage cache (also warmed by Layout on any
  // page an admin visits) — shows the last known values instantly, even
  // slightly stale, rather than a blank skeleton. See adminStatsCache.ts.
  const [stats, setStats] = useState<Stats | null>(
    () => loadTTLCache<Stats>(ADMIN_STATS_CACHE_KEY, ADMIN_STATS_TTL_MS)?.data ?? null,
  )
  const [sysStats, setSysStats] = useState<SystemStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [statsRefreshing, setStatsRefreshing] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [fixRtStatus, setFixRtStatus] = useState<{ running: boolean; stage: string; current: number; total: number; fixed: number }>({
    running: false, stage: '', current: 0, total: 0, fixed: 0,
  })
  const fixRuntimePoll = useRef<ReturnType<typeof setInterval> | null>(null)
  const [refreshCardsStatus, setRefreshCardsStatus] = useState<{ running: boolean; current: number; total: number; updated: number }>({
    running: false, current: 0, total: 0, updated: 0,
  })
  const refreshCardsPoll = useRef<ReturnType<typeof setInterval> | null>(null)
  const [backfillCastStatus, setBackfillCastStatus] = useState<{ running: boolean; current: number; total: number; updated: number }>({
    running: false, current: 0, total: 0, updated: 0,
  })
  const backfillCastPoll = useRef<ReturnType<typeof setInterval> | null>(null)
  const meId = useRef<number | null>(null)
  const [backingUp, setBackingUp] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const restoreInput = useRef<HTMLInputElement | null>(null)
  const [apiKey, setApiKey] = useState<string>('')
  const [extSources, setExtSources] = useState<{ key: string; enabled: boolean; token: string }[]>([])
  const [extDrafts, setExtDrafts] = useState<Record<string, string>>({})
  const [extSaving, setExtSaving] = useState<string | null>(null)
  const [apiTab, setApiTab] = useState<RequestsTab>('today')
  const [catsTab, setCatsTab] = useState<RequestsTab>('today')
  const [myshowsTab, setMyshowsTab] = useState<RequestsTab>('today')

  function toast(text: string, ok = true) {
    const id = Date.now()
    setToasts(prev => [...prev, { id, text, ok }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 3000)
  }

  const fetchSysStats = useCallback(async () => {
    const res = await fetch('/api/admin/system-stats')
    if (res.ok) setSysStats(await res.json())
  }, [])

  const fetchUsers = useCallback(async (page: number, query: string, sortBy: string, sortDir: string, perPage: number) => {
    const params = new URLSearchParams({ page: String(page), sort_by: sortBy, sort_dir: sortDir, per_page: String(perPage) })
    if (query) params.set('search', query)
    const res = await fetch(`/api/admin/users?${params}`)
    if (res.ok) setUsersPaged(await res.json())
  }, [])

  // Silent refresh — stats only, no user list reset. Always hits the network
  // (used by explicit calls and the status-poll intervals below, which need
  // real-time data regardless of cache freshness); the TTL check lives at
  // the mount effect that decides whether to call this at all on page load.
  const refresh = useCallback(async () => {
    const res = await fetch('/api/admin/stats')
    if (res.ok) {
      const data = await res.json()
      saveTTLCache(ADMIN_STATS_CACHE_KEY, data)
      setStats(data)
    }
  }, [])

  async function fetchExtSources() {
    const res = await fetch('/api/admin/external-sources')
    if (!res.ok) return
    const data = await res.json()
    setExtSources(data.sources ?? [])
  }

  async function toggleExtSource(key: string, enabled: boolean) {
    try {
      await api(`/api/admin/external-sources/${key}`, 'POST', { enabled })
      await fetchExtSources()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function saveExtToken(key: string) {
    setExtSaving(key)
    try {
      await api(`/api/admin/external-sources/${key}`, 'POST', { token: extDrafts[key] ?? '' })
      setExtDrafts(d => { const next = { ...d }; delete next[key]; return next })
      toast('Токен сохранён')
      await fetchExtSources()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    } finally {
      setExtSaving(null)
    }
  }

  async function fetchFixRtStatus() {
    const res = await fetch('/api/admin/fix-runtime/status')
    if (!res.ok) return
    const data = await res.json()
    setFixRtStatus(data)
    return data
  }

  function startFixRuntimePoll() {
    if (fixRuntimePoll.current) return
    fixRuntimePoll.current = setInterval(async () => {
      const data = await fetchFixRtStatus()
      if (data && !data.running) {
        clearInterval(fixRuntimePoll.current!)
        fixRuntimePoll.current = null
      }
      refresh()
    }, 3000)
  }

  async function fetchRefreshCardsStatus() {
    const res = await fetch('/api/admin/refresh-cards/status')
    if (!res.ok) return
    const data = await res.json()
    setRefreshCardsStatus(data)
    return data
  }

  function startRefreshCardsPoll() {
    if (refreshCardsPoll.current) return
    refreshCardsPoll.current = setInterval(async () => {
      const data = await fetchRefreshCardsStatus()
      if (data && !data.running) {
        clearInterval(refreshCardsPoll.current!)
        refreshCardsPoll.current = null
      }
      refresh()
    }, 3000)
  }

  async function runRefreshCards() {
    try {
      const res = await api('/api/admin/refresh-cards', 'POST')
      if (res.status === 'already_running') {
        toast('Задача уже запущена')
      } else {
        toast('Обновление карточек из TMDB запущено в фоне')
        await fetchRefreshCardsStatus()
      }
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function stopRefreshCards() {
    try {
      await api('/api/admin/refresh-cards/stop', 'POST')
      toast('Задача остановлена')
      await fetchRefreshCardsStatus()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function fetchBackfillCastStatus() {
    const res = await fetch('/api/admin/backfill-cast/status')
    if (!res.ok) return
    const data = await res.json()
    setBackfillCastStatus(data)
    return data
  }

  function startBackfillCastPoll() {
    if (backfillCastPoll.current) return
    backfillCastPoll.current = setInterval(async () => {
      const data = await fetchBackfillCastStatus()
      if (data && !data.running) {
        clearInterval(backfillCastPoll.current!)
        backfillCastPoll.current = null
      }
      refresh()
    }, 3000)
  }

  async function runBackfillCast() {
    try {
      const res = await api('/api/admin/backfill-cast', 'POST')
      if (res.status === 'already_running') {
        toast('Задача уже запущена')
      } else {
        toast('Заполнение актёров запущено в фоне')
        await fetchBackfillCastStatus()
      }
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function stopBackfillCast() {
    try {
      await api('/api/admin/backfill-cast/stop', 'POST')
      toast('Задача остановлена')
      await fetchBackfillCastStatus()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  useEffect(() => {
    setLoading(true)
    // fetch meId once
    fetch('/api/me').then(r => r.ok ? r.json() : null).then(d => { if (d?.id) meId.current = d.id })
    // Stats render independently (skeleton vs cards gated by `stats`, seeded
    // from the shared cache) — kept out of this Promise.all so the users
    // table doesn't sit on "Загрузка…" waiting for the stats round-trip too.
    // Skipped entirely when the cache (possibly warmed by Layout on a
    // previous page) is still fresh — no point refetching on every visit.
    const cachedStats = loadTTLCache<Stats>(ADMIN_STATS_CACHE_KEY, ADMIN_STATS_TTL_MS)
    if (!cachedStats || cachedStats.stale) refresh()
    Promise.all([
      fetchUsers(1, '', 'created_at', 'desc', 10),
      fetchFixRtStatus(), fetchRefreshCardsStatus(), fetchBackfillCastStatus(), fetchSysStats(), fetchApiKey(), fetchExtSources(),
    ]).finally(() => setLoading(false))
    const sysInterval = setInterval(fetchSysStats, 5000)
    return () => clearInterval(sysInterval)
  }, [refresh, fetchSysStats, fetchUsers]) // eslint-disable-line react-hooks/exhaustive-deps

  // Reload users when pagination/sort/perPage changes
  useEffect(() => {
    fetchUsers(usersPage, usersQuery, usersSortBy, usersSortDir, usersPerPage)
  }, [usersPage, usersQuery, usersSortBy, usersSortDir, usersPerPage, fetchUsers]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (fixRtStatus.running) startFixRuntimePoll()
  }, [fixRtStatus.running]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (refreshCardsStatus.running) startRefreshCardsPoll()
  }, [refreshCardsStatus.running]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (backfillCastStatus.running) startBackfillCastPoll()
  }, [backfillCastStatus.running]) // eslint-disable-line react-hooks/exhaustive-deps

  async function runFixRuntime() {
    try {
      const res = await api('/api/admin/fix-runtime', 'POST')
      if (res.status === 'already_running') {
        toast('Задача уже запущена')
      } else {
        toast('Обновление runtime запущено в фоне')
        await fetchFixRtStatus()
      }
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function stopFixRuntime() {
    try {
      await api('/api/admin/fix-runtime/stop', 'POST')
      toast('Задача остановлена')
      await fetchFixRtStatus()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

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

  async function act(label: string, fn: () => Promise<unknown>) {
    try {
      await fn()
      toast(label)
      await Promise.all([refresh(), fetchUsers(usersPage, usersQuery, usersSortBy, usersSortDir, usersPerPage)])
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function createUser() {
    setCreatingUser(true)
    try {
      const data = await api('/api/admin/users', 'POST') as { username: string; password: string }
      setConfirmingCreateUser(false)
      setNewUserCreds(data)
      await Promise.all([refresh(), fetchUsers(usersPage, usersQuery, usersSortBy, usersSortDir, usersPerPage)])
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    } finally {
      setCreatingUser(false)
    }
  }

  function copyNewUserCreds() {
    if (!newUserCreds) return
    navigator.clipboard?.writeText(`Логин: ${newUserCreds.username}\nПароль: ${newUserCreds.password}`).catch(() => {})
    setNewUserCreds(null)
    toast('Логин и пароль скопированы')
  }

  async function setRole(id: number, role: string) {
    await act(`Роль изменена`, () => api(`/api/admin/users/${id}/role`, 'PATCH', { role }))
  }

  async function deleteUser(id: number, username: string) {
    if (!confirm(`Удалить пользователя ${username}?`)) return
    await act(`${username} удалён`, () => api(`/api/admin/users/${id}`, 'DELETE'))
  }

  async function toggleAdmin(id: number, username: string, isAdmin: boolean) {
    await act(
      isAdmin ? `${username}: права администратора сняты` : `${username}: назначен администратором`,
      () => api(`/api/admin/users/${id}/toggle-admin`, 'PATCH'),
    )
  }

  async function blockUser(id: number, username: string) {
    const reason = prompt('Причина блокировки (необязательно):')
    if (reason === null) return // отмена
    await act(`${username} заблокирован`, () => api(`/api/admin/users/${id}/block`, 'POST', { reason }))
  }

  async function unblockUser(id: number, username: string) {
    await act(`${username} разблокирован`, () => api(`/api/admin/users/${id}/unblock`, 'POST'))
  }

  async function resetSync(id: number, username: string) {
    await act(`${username}: кулдаун MyShows сброшен`, () => api(`/api/admin/users/${id}/reset-sync`, 'POST'))
  }

  async function cleanupLimits(id: number, username: string) {
    const data = await api(`/api/admin/users/${id}/cleanup-limits`, 'POST').catch((e: Error) => { toast(e.message, false); return null })
    if (data === null) return
    const msg = data.deleted_devices > 0
      ? `${username}: удалено устройств ${data.deleted_devices}`
      : `${username}: лимиты в порядке`
    toast(msg)
    await refresh()
  }

  async function globalAct(url: string, msg: string, body?: object) {
    try {
      await api(url, 'POST', body)
      toast(msg)
      await refresh()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function downloadBackup() {
    setBackingUp(true)
    try {
      const res = await fetch('/api/admin/backup')
      if (!res.ok) throw new Error(await res.text() || 'Ошибка бэкапа')
      const blob = await res.blob()
      const cd = res.headers.get('Content-Disposition') || ''
      const m = cd.match(/filename="?([^"]+)"?/)
      const name = m ? m[1] : `movies-backup-${Date.now()}.sql.gz`
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = name
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      toast('Бэкап скачан')
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    } finally {
      setBackingUp(false)
    }
  }

  async function restoreBackup(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = '' // позволяем выбрать тот же файл повторно
    if (!file) return
    if (!confirm(`Восстановить базу из «${file.name}»?\n\nВсе текущие данные (пользователи, токены, настройки, карточки) будут ЗАМЕНЕНЫ. После восстановления приложение перезапустится и вам нужно будет войти заново.`)) return
    setRestoring(true)
    try {
      const fd = new FormData()
      fd.append('backup', file)
      const res = await fetch('/api/admin/restore', { method: 'POST', body: fd })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(data.error || 'Ошибка восстановления')
      toast('База восстановлена, перезапуск…')
      setTimeout(() => window.location.reload(), 4000)
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
      setRestoring(false)
    }
  }

  const fetchApiKey = useCallback(async () => {
    const res = await fetch('/api/admin/api-key')
    if (!res.ok) return
    const data = await res.json()
    setApiKey(data.api_key || '')
  }, [])

  async function rotateApiKey() {
    if (apiKey && !confirm('Сгенерировать новый ключ? Старый перестанет работать.')) return
    try {
      const data = await api('/api/admin/api-key', 'POST')
      setApiKey(data.api_key || '')
      toast('Ключ сгенерирован')
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  async function revokeApiKey() {
    if (!confirm('Отозвать API-ключ? Запросы с ним перестанут работать.')) return
    try {
      await api('/api/admin/api-key', 'DELETE')
      setApiKey('')
      toast('Ключ отозван')
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
  }

  function copyApiKey() {
    navigator.clipboard?.writeText(apiKey).then(() => toast('Скопировано'), () => toast('Не удалось скопировать', false))
  }

  const roleOrder = ['simple', 'premium', 'super']

  function RoleSelect({ u }: { u: AdminUser }) {
    if (u.is_admin) {
      return <span className={`${styles.roleBadge} ${styles[u.role] ?? styles.simple}`}>{u.role}</span>
    }
    return (
      <select
        className={`${styles.roleSelect} ${styles[u.role] ?? styles.simple}`}
        value={u.role}
        onChange={e => setRole(u.id, e.target.value)}
      >
        {roleOrder.map(r => <option key={r} value={r}>{r}</option>)}
      </select>
    )
  }

  function UserActions({ u }: { u: AdminUser }) {
    const isSelf = u.id === meId.current
    return (
      <>
        {!isSelf && (
          <button
            className={`${styles.btnSm} ${u.is_admin ? styles.warning : ''}`}
            onClick={() => toggleAdmin(u.id, u.username, u.is_admin)}
          >
            {u.is_admin ? 'Снять адм.' : 'Дать адм.'}
          </button>
        )}
        {!u.is_admin && (
          u.blocked_at
            ? <button className={styles.btnSm} onClick={() => unblockUser(u.id, u.username)}>Разблок.</button>
            : <button className={`${styles.btnSm} ${styles.warning}`} onClick={() => blockUser(u.id, u.username)}>Блок</button>
        )}
        <button className={styles.btnSm} onClick={() => resetSync(u.id, u.username)} title="Сбросить кулдаун MyShows">MyShows</button>
        <button className={styles.btnSm} onClick={() => cleanupLimits(u.id, u.username)} title="Удалить устройства сверх лимита">Лимиты</button>
        {!u.is_admin && (
          <button className={`${styles.btnSm} ${styles.danger}`} onClick={() => deleteUser(u.id, u.username)}>
            Удалить
          </button>
        )}
      </>
    )
  }

  return (
    <Layout wide>
      {/* ── Toasts ─────────────────────────────────────────────────────────── */}
      {toasts.length > 0 && (
        <div className={styles.toasts}>
          {toasts.map(t => (
            <div key={t.id} className={`${styles.toast} ${t.ok ? styles.toastOk : styles.toastErr}`}>
              {t.text}
            </div>
          ))}
        </div>
      )}

      {/* ── Создать пользователя: подтверждение ──────────────────────────────── */}
      {confirmingCreateUser && (
        <div
          style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
          onClick={() => !creatingUser && setConfirmingCreateUser(false)}
        >
          <div
            style={{ background: '#181818', border: '1px solid #444', borderRadius: 10, padding: 24, width: 360, maxWidth: '90vw' }}
            onClick={e => e.stopPropagation()}
          >
            <h2 style={{ margin: '0 0 12px', fontSize: '1.05rem' }}>Создать пользователя?</h2>
            <p className={styles.empty} style={{ margin: '0 0 20px' }}>
              Логин и пароль будут сгенерированы автоматически. Пользователь обязан сменить пароль при первом входе.
            </p>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8 }}>
              <button className={styles.actionBtn} onClick={() => setConfirmingCreateUser(false)} disabled={creatingUser}>Отмена</button>
              <button className={styles.actionBtn} onClick={createUser} disabled={creatingUser}>
                {creatingUser ? 'Создание…' : 'Создать'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Новый пользователь: реквизиты (показываются один раз) ────────────── */}
      {newUserCreds && (
        <div
          style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
          onClick={copyNewUserCreds}
        >
          <div
            style={{ background: '#181818', border: '1px solid #444', borderRadius: 10, padding: 24, width: 360, maxWidth: '90vw' }}
            onClick={e => e.stopPropagation()}
          >
            <h2 style={{ margin: '0 0 12px', fontSize: '1.05rem' }}>Пользователь создан</h2>
            <p className={styles.empty} style={{ margin: '0 0 16px' }}>
              Сохраните и передайте пользователю — пароль больше нигде не показывается. При первом входе он обязан сменить его на свой.
            </p>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginBottom: 20 }}>
              <div>
                <div style={{ color: '#888', fontSize: '0.75rem', marginBottom: 2 }}>Логин</div>
                <input readOnly value={newUserCreds.username} onFocus={e => e.target.select()}
                  style={{ width: '100%', background: '#111', border: '1px solid #444', borderRadius: 6, color: '#fff', padding: '6px 10px', fontSize: '0.9rem' }} />
              </div>
              <div>
                <div style={{ color: '#888', fontSize: '0.75rem', marginBottom: 2 }}>Пароль</div>
                <input readOnly value={newUserCreds.password} onFocus={e => e.target.select()}
                  style={{ width: '100%', background: '#111', border: '1px solid #444', borderRadius: 6, color: '#fff', padding: '6px 10px', fontSize: '0.9rem' }} />
              </div>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8 }}>
              <button className={styles.actionBtn} onClick={() => setNewUserCreds(null)}>Закрыть</button>
              <button className={styles.actionBtn} onClick={copyNewUserCreds}>Копировать и закрыть</button>
            </div>
          </div>
        </div>
      )}

      <div className={styles.page}>
        <div className={styles.pageHeader}>
          <h1 className={styles.title}>Администрирование</h1>
          <div className={styles.headerNav}>
            <button
              type="button"
              className={styles.navBtn}
              title="Статистика кешируется на 5 минут — если только что менял что-то на сервере (например прогревал кеш картинок), обнови вручную"
              disabled={statsRefreshing}
              onClick={async () => {
                setStatsRefreshing(true)
                try {
                  await refresh()
                } finally {
                  setStatsRefreshing(false)
                }
              }}
            >
              {statsRefreshing ? 'Обновление…' : 'Обновить статистику'}
            </button>
            <Link to="/admin/parsers" className={styles.navBtn}>Парсеры</Link>
            <Link to="/admin/proxies" className={styles.navBtn}>Прокси</Link>
            <Link to="/admin/bot" className={styles.navBtn}>Бот</Link>
            <Link to="/admin/logs" className={styles.navBtn}>Логи</Link>
            <Link to="/admin/tmdb-missing" className={styles.navBtn}>TMDB проблемы</Link>
            <Link to="/admin/settings" className={styles.navBtn}>Настройки</Link>
          </div>
        </div>

        {sysStats && (
          <div className={styles.sysBar}>
            <div className={styles.sysItem}>
              <span className={styles.sysValue}>
                {sysStats.uptime_days > 0 && `${sysStats.uptime_days}д `}
                {sysStats.uptime_hours > 0 && `${sysStats.uptime_hours}ч `}
                {sysStats.uptime_minutes}м
              </span>
              <span className={styles.sysLabel}>Аптайм</span>
            </div>
            <div className={styles.sysItem}>
              <span className={styles.sysValue}>{sysStats.goroutines}</span>
              <span className={styles.sysLabel}>Горутины</span>
            </div>
            <div className={styles.sysItem}>
              <span className={styles.sysValue}>{sysStats.memory_mb} MB</span>
              <span className={styles.sysLabel}>Память</span>
            </div>
            <div className={styles.sysItem}>
              <span className={styles.sysValue}>{sysStats.num_cpu}</span>
              <span className={styles.sysLabel}>CPU</span>
            </div>
          </div>
        )}

        {!stats && (
          <div className={styles.stats}>
            {Array.from({ length: 8 }).map((_, i) => (
              <div key={i} className={styles.statCard}>
                <div className={styles.statSkeletonValue} />
                <div className={styles.statSkeletonLabel} />
              </div>
            ))}
          </div>
        )}

        {stats && (
          <div className={styles.stats}>
            <div className={`${styles.statCard} ${styles.statCardClickable}`}>
              <Link to="/admin/users-list" className={styles.statLink}>
                <p className={styles.statValue}>{stats.users}</p>
                <p className={styles.statLabel}>Пользователей</p>
              </Link>
            </div>
            {stats.users_today > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/users-today" className={styles.statLink}>
                  <p className={styles.statValue}>+{stats.users_today}</p>
                  <p className={styles.statLabel}>Новых сегодня</p>
                </Link>
              </div>
            )}
            <div className={`${styles.statCard} ${styles.statCardClickable}`}>
              <Link to="/admin/devices-list" className={styles.statLink}>
                <p className={styles.statValue}>{stats.devices}</p>
                <p className={styles.statLabel}>Устройств</p>
              </Link>
            </div>
            {stats.devices_today > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/devices-today" className={styles.statLink}>
                  <p className={styles.statValue}>+{stats.devices_today}</p>
                  <p className={styles.statLabel}>Устройств сегодня</p>
                </Link>
              </div>
            )}
            <div className={`${styles.statCard} ${styles.statCardClickable}`}>
              <Link to="/admin/all-cards" className={styles.statLink}>
                <p className={styles.statValue}>{stats.media_cards.toLocaleString()}</p>
                <p className={styles.statLabel}>Медиакарточек</p>
              </Link>
            </div>
            {stats.media_cards_today > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/cards-today" className={styles.statLink}>
                  <p className={styles.statValue}>+{stats.media_cards_today.toLocaleString()}</p>
                  <p className={styles.statLabel}>Карточек сегодня</p>

                </Link>
              </div>
            )}
            <div className={`${styles.statCard} ${styles.statCardClickable}`}>
              <Link to="/admin/timecodes-list" className={styles.statLink}>
                <p className={styles.statValue}>{stats.timecodes.toLocaleString()}</p>
                <p className={styles.statLabel}>Таймкодов</p>
              </Link>
            </div>
            {stats.timecodes_today > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/timecodes-today" className={styles.statLink}>
                  <p className={styles.statValue}>+{stats.timecodes_today.toLocaleString()}</p>
                  <p className={styles.statLabel}>Таймкодов сегодня</p>
                </Link>
              </div>
            )}
            {stats.api_reqs_today > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>{stats.api_reqs_today.toLocaleString()}</p>
                <p className={styles.statLabel}>Запросов API сегодня</p>
              </div>
            )}
            {stats.api_ips_today > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>{stats.api_ips_today.toLocaleString()}</p>
                <p className={styles.statLabel}>IP сегодня</p>
              </div>
            )}
            {stats.no_runtime_movies > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/no-runtime-movies" className={styles.statLink}>
                  <p className={`${styles.statValue} ${styles.statWarn}`}>{stats.no_runtime_movies.toLocaleString()}</p>
                  <p className={styles.statLabel}>Фильмов без runtime</p>
                </Link>
              </div>
            )}
            {stats.no_runtime_tv > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/no-runtime-tv" className={styles.statLink}>
                  <p className={`${styles.statValue} ${styles.statWarn}`}>{stats.no_runtime_tv.toLocaleString()}</p>
                  <p className={styles.statLabel}>Сериалов без runtime</p>
                </Link>
              </div>
            )}
            {stats.tmdb_refreshed_today > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/tmdb-refreshed-today" className={styles.statLink}>
                  <p className={styles.statValue}>{stats.tmdb_refreshed_today.toLocaleString()}</p>
                  <p className={styles.statLabel}>Обновлено из TMDB сегодня</p>
                </Link>
              </div>
            )}
            {stats.tmdb_not_found > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/tmdb-missing" className={styles.statLink}>
                  <p className={`${styles.statValue} ${styles.statWarn}`}>{stats.tmdb_not_found.toLocaleString()}</p>
                  <p className={styles.statLabel}>Не найдено в TMDB</p>

                </Link>
              </div>
            )}
            {stats.popular_cards > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/popular" className={styles.statLink}>
                  <p className={styles.statValue}>{stats.popular_cards.toLocaleString()}</p>
                  <p className={styles.statLabel}>{stats.popular_source_url ? 'Популярных (локально)' : 'Популярных карточек'}</p>
                </Link>
              </div>
            )}
            {stats.popular_source_url && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/popular-source" className={styles.statLink}>
                  <p className={styles.statValue}>{stats.popular_source_count >= 0 ? stats.popular_source_count.toLocaleString() : '—'}</p>
                  <p className={styles.statLabel}>Популярное (источник)</p>
                </Link>
              </div>
            )}
            {stats.actor_count > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/actors" className={styles.statLink}>
                  <p className={styles.statValue}>{stats.actor_count.toLocaleString()}</p>
                  <p className={styles.statLabel}>Актёров</p>

                </Link>
              </div>
            )}
            {stats.director_count > 0 && (
              <div className={`${styles.statCard} ${styles.statCardClickable}`}>
                <Link to="/admin/directors" className={styles.statLink}>
                  <p className={styles.statValue}>{stats.director_count.toLocaleString()}</p>
                  <p className={styles.statLabel}>Режиссёров</p>

                </Link>
              </div>
            )}
            {stats.image_cache_files > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>{formatCacheSize(stats.image_cache_bytes)}</p>
                <p className={styles.statLabel}>Кеш картинок ({stats.image_cache_files.toLocaleString()} файлов)</p>
              </div>
            )}
          </div>
        )}

        {/* ── Global actions ─────────────────────────────────────────────────── */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>Действия</h2>
          <div className={styles.actionsGrid}>
            <button className={styles.actionBtn} title="Проверить истёкшие подписки и понизить роль пользователей" onClick={() => globalAct('/api/admin/run-expiry-check', 'Premium проверен')}>
              Проверить Premium
            </button>
            <button className={styles.actionBtn} title="Продлить Premium всем активным пользователям на количество дней из настроек" onClick={() => globalAct('/api/admin/extend-all-premium', 'Premium продлён')}>
              Продлить всем
            </button>
            <button className={styles.actionBtn} title="Обновить список эпизодов онгоинг-сериалов из MyShows (статус: Returning Series, In Production, Pilot)" onClick={() => globalAct('/api/admin/episodes-refresh', 'Обновление эпизодов запущено')}>
              Обновить эпизоды
            </button>
            {fixRtStatus.running
              ? <button className={`${styles.actionBtn} ${styles.danger}`} title="Остановить фоновую задачу обновления runtime" onClick={stopFixRuntime}>Остановить runtime</button>
              : <button className={styles.actionBtn} title="Запустить фоновое обновление runtime/episode_run_time из TMDB для карточек с нулевым значением" onClick={runFixRuntime}>Обновить runtime</button>
            }
            {refreshCardsStatus.running
              ? <button className={`${styles.actionBtn} ${styles.danger}`} title="Остановить обновление карточек из TMDB" onClick={stopRefreshCards}>Остановить TMDB</button>
              : <button className={styles.actionBtn} title="Обновить метаданные карточек из TMDB (пакетно, по tmdb_refresh_batch карточек)" onClick={runRefreshCards}>Обновить TMDB</button>
            }
            {backfillCastStatus.running
              ? <button className={`${styles.actionBtn} ${styles.danger}`} onClick={stopBackfillCast}>Остановить актёров</button>
              : <button className={styles.actionBtn} title="Заполнить актёров и режиссёров из TMDB для карточек без каста" onClick={runBackfillCast}>Заполнить актёров и режиссёров</button>
            }
          </div>
          {fixRtStatus.running && fixRtStatus.total > 0 && (
            <div className={styles.fixRtProgress}>
              <div className={styles.fixRtLabel}>
                <span>{fixRtStatus.stage === 'movie' ? 'Фильмы' : 'Сериалы'}: {fixRtStatus.current} / {fixRtStatus.total}</span>
                <span>Обновлено: {fixRtStatus.fixed}</span>
                <span>{Math.round(fixRtStatus.current / fixRtStatus.total * 100)}%</span>
              </div>
              <div className={styles.fixRtBar}>
                <div className={styles.fixRtBarFill} style={{ width: `${Math.round(fixRtStatus.current / fixRtStatus.total * 100)}%` }} />
              </div>
            </div>
          )}
          {refreshCardsStatus.running && refreshCardsStatus.total > 0 && (
            <div className={styles.fixRtProgress}>
              <div className={styles.fixRtLabel}>
                <span>TMDB refresh: {refreshCardsStatus.current} / {refreshCardsStatus.total}</span>
                <span>Обновлено: {refreshCardsStatus.updated}</span>
                <span>{Math.round(refreshCardsStatus.current / refreshCardsStatus.total * 100)}%</span>
              </div>
              <div className={styles.fixRtBar}>
                <div className={styles.fixRtBarFill} style={{ width: `${Math.round(refreshCardsStatus.current / refreshCardsStatus.total * 100)}%` }} />
              </div>
            </div>
          )}
          {backfillCastStatus.running && backfillCastStatus.total > 0 && (
            <div className={styles.fixRtProgress}>
              <div className={styles.fixRtLabel}>
                <span>Актёры: {backfillCastStatus.current} / {backfillCastStatus.total}</span>
                <span>Заполнено: {backfillCastStatus.updated}</span>
                <span>{Math.round(backfillCastStatus.current / backfillCastStatus.total * 100)}%</span>
              </div>
              <div className={styles.fixRtBar}>
                <div className={styles.fixRtBarFill} style={{ width: `${Math.round(backfillCastStatus.current / backfillCastStatus.total * 100)}%` }} />
              </div>
            </div>
          )}

        </div>

        {/* ── External runtime sources ───────────────────────────────────────── */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>Внешние источники runtime</h2>
          <p className={styles.empty}>
            Фолбэки для «Обновить runtime», когда у TMDB нет данных. Ключи хранятся отдельно от остальных
            настроек и не попадают в бэкап (см. <code>scripts/backup.sh</code>).
          </p>
          <div className={styles.extSourceList}>
            {extSources.map(src => {
              const info = EXT_SOURCE_INFO[src.key] ?? { label: src.key, hint: '' }
              const draft = extDrafts[src.key]
              return (
                <div key={src.key} className={styles.extSourceRow}>
                  <label className={styles.extSourceToggle}>
                    <input
                      type="checkbox"
                      checked={src.enabled}
                      onChange={e => toggleExtSource(src.key, e.target.checked)}
                    />
                    <span className={styles.extSourceLabel}>{info.label}</span>
                  </label>
                  <span className={styles.extSourceHint}>{info.hint}</span>
                  <div className={styles.apiKeyRow}>
                    <input
                      className={styles.apiKeyInput}
                      type="text"
                      placeholder="ключ не задан"
                      value={draft ?? src.token}
                      onChange={e => setExtDrafts(d => ({ ...d, [src.key]: e.target.value }))}
                      autoComplete="off"
                      data-bwignore
                      data-lpignore="true"
                      data-1p-ignore
                    />
                    <button
                      className={styles.actionBtn}
                      disabled={draft === undefined || draft === src.token || extSaving === src.key}
                      onClick={() => saveExtToken(src.key)}
                    >
                      {extSaving === src.key ? 'Сохранение…' : 'Сохранить'}
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        </div>

        {/* ── Backup & restore ───────────────────────────────────────────────── */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>Бэкап и восстановление</h2>
          <p className={styles.empty}>
            Полный дамп всей базы (пользователи, токены, настройки, карточки) для переезда на другой сервер.
            Файл содержит секреты — храните его в надёжном месте.
          </p>
          <div className={styles.actionsGrid}>
            <button
              className={styles.actionBtn}
              title="Скачать полный дамп всей БД (.sql.gz)"
              onClick={downloadBackup}
              disabled={backingUp || restoring}
            >
              {backingUp ? 'Подготовка…' : 'Скачать бэкап'}
            </button>
            <button
              className={`${styles.actionBtn} ${styles.danger}`}
              title="Загрузить .sql.gz и заменить все данные. Приложение перезапустится."
              onClick={() => restoreInput.current?.click()}
              disabled={restoring || backingUp}
            >
              {restoring ? 'Восстановление…' : 'Восстановить из файла'}
            </button>
            <input
              ref={restoreInput}
              type="file"
              accept=".gz,.sql.gz,application/gzip"
              style={{ display: 'none' }}
              onChange={restoreBackup}
            />
          </div>
        </div>

        {/* ── API key ────────────────────────────────────────────────────────── */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>API-ключ</h2>
          <p className={styles.empty}>
            Даёт полный доступ к <code>/api/admin/*</code> по заголовку <code>X-API-Key</code> — для скриптов
            бэкапа/восстановления и миграции без логина. Храните в секрете; новая генерация отзывает старый ключ.
          </p>
          <div className={styles.apiKeyRow}>
            <input
              className={styles.apiKeyInput}
              type="text"
              readOnly
              value={apiKey || ''}
              placeholder="ключ не задан"
              onFocus={e => e.target.select()}
            />
            {apiKey && <button className={styles.btnSm} onClick={copyApiKey}>Копировать</button>}
            <button className={styles.actionBtn} onClick={rotateApiKey}>
              {apiKey ? 'Перегенерировать' : 'Сгенерировать'}
            </button>
            {apiKey && <button className={`${styles.actionBtn} ${styles.danger}`} onClick={revokeApiKey}>Отозвать</button>}
          </div>
        </div>

        {/* ── Users ──────────────────────────────────────────────────────────── */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>
            Пользователи{usersPaged ? ` (${usersPaged.total.toLocaleString()})` : ''}
          </h2>

          <div style={{ display: 'flex', gap: 8, marginBottom: 12, alignItems: 'center', flexWrap: 'wrap' }}>
            <div style={{ position: 'relative', display: 'inline-flex', alignItems: 'center' }}>
              <input
                placeholder="Поиск по имени…"
                value={usersSearch}
                onChange={e => {
                  setUsersSearch(e.target.value)
                  if (usersTimer.current) clearTimeout(usersTimer.current)
                  usersTimer.current = setTimeout(() => { setUsersPage(1); setUsersQuery(e.target.value.trim()) }, 300)
                }}
                style={{ background: '#111', border: '1px solid #444', borderRadius: 6, color: '#fff', padding: '5px 28px 5px 10px', fontSize: '0.82rem', outline: 'none', width: 200 }}
              />
              {usersSearch && (
                <button
                  onClick={() => {
                    setUsersSearch('')
                    if (usersTimer.current) clearTimeout(usersTimer.current)
                    setUsersPage(1)
                    setUsersQuery('')
                  }}
                  style={{ position: 'absolute', right: 6, background: 'none', border: 'none', color: '#666', cursor: 'pointer', fontSize: '1rem', lineHeight: 1, padding: 0 }}
                >×</button>
              )}
            </div>
            <select
              value={usersPerPage}
              onChange={e => { setUsersPerPage(Number(e.target.value)); setUsersPage(1) }}
              style={{ background: '#111', border: '1px solid #444', borderRadius: 6, color: '#ccc', padding: '5px 8px', fontSize: '0.82rem', cursor: 'pointer' }}
            >
              <option value={10}>10</option>
              <option value={30}>30</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={0}>Все</option>
            </select>
            <button className={styles.actionBtn} onClick={() => setConfirmingCreateUser(true)}>
              + Создать пользователя
            </button>
          </div>

          {loading && <p className={styles.empty}>Загрузка…</p>}
          {!loading && usersPaged?.items.length === 0 && <p className={styles.empty}>Нет пользователей</p>}

          {usersPaged && usersPaged.items.length > 0 && (
            <>
              {/* Desktop table */}
              <table className={styles.table}>
                <thead>
                  <tr>
                    {(['id', 'username', 'role', 'devices', 'created_at', 'premium_until'] as const).map(col => {
                      const labels: Record<string, string> = { id: 'ID', username: 'Имя', role: 'Роль', devices: 'Устройств', created_at: 'Создан', premium_until: 'Premium до' }
                      const active = usersSortBy === col
                      return (
                        <th key={col} onClick={() => {
                          if (active) setUsersSortDir(d => d === 'desc' ? 'asc' : 'desc')
                          else { setUsersSortBy(col); setUsersSortDir('desc') }
                          setUsersPage(1)
                        }} style={{ cursor: 'pointer', userSelect: 'none', color: active ? '#4a90e2' : undefined, whiteSpace: 'nowrap' }}>
                          {labels[col]}{active ? (usersSortDir === 'asc' ? ' ↑' : ' ↓') : ''}
                        </th>
                      )
                    })}
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {usersPaged.items.map(u => (
                    <tr key={u.id} className={u.blocked_at ? styles.rowBlocked : undefined}>
                      <td>{u.id}</td>
                      <td>
                        {u.username}{u.is_admin && ' 👑'}
                        {u.blocked_at && <span className={styles.blockedBadge}> 🔒</span>}
                      </td>
                      <td><RoleSelect u={u} /></td>
                      <td>{u.device_count}</td>
                      <td>{new Date(u.created_at).toLocaleDateString('ru-RU')}</td>
                      <td>{u.premium_until ?? '—'}</td>
                      <td>
                        <div className={styles.tableActions}>
                          <UserActions u={u} />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              {/* Mobile sort — hidden on desktop via CSS */}
              <div className={styles.mobileSort}>
                <span style={{ color: '#888', fontSize: '0.8rem' }}>Сортировка:</span>
                <select
                  value={usersSortBy}
                  onChange={e => { setUsersSortBy(e.target.value); setUsersPage(1) }}
                  style={{ background: '#1a1a1a', border: '1px solid #444', borderRadius: 5, color: '#ccc', padding: '4px 8px', fontSize: '0.8rem', flex: 1 }}
                >
                  {([['id','ID'],['username','Имя'],['role','Роль'],['devices','Устройств'],['created_at','Дата'],['premium_until','Premium до']] as const).map(([val, label]) => (
                    <option key={val} value={val}>{label}</option>
                  ))}
                </select>
                <button
                  onClick={() => { setUsersSortDir(d => d === 'desc' ? 'asc' : 'desc'); setUsersPage(1) }}
                  style={{ background: '#1a1a1a', border: '1px solid #444', borderRadius: 5, color: '#ccc', padding: '4px 10px', fontSize: '0.85rem', cursor: 'pointer' }}
                >
                  {usersSortDir === 'desc' ? '↓' : '↑'}
                </button>
              </div>

              {/* Mobile cards */}
              <div className={styles.cards}>
                {usersPaged.items.map(u => (
                  <div key={u.id} className={`${styles.userCard} ${u.blocked_at ? styles.userCardBlocked : ''}`}>
                    <div className={styles.cardTop}>
                      <div className={styles.cardName}>
                        {u.username}{u.is_admin && ' 👑'}
                        {u.blocked_at && <span className={styles.blockedBadge}> 🔒</span>}
                      </div>
                      <div className={styles.cardMeta}>
                        #{u.id} · {new Date(u.created_at).toLocaleDateString('ru-RU')}
                      </div>
                    </div>
                    <div className={styles.cardDevices}>
                      Устройств: {u.device_count}
                      {u.premium_until && <> · Premium до {u.premium_until}</>}
                    </div>
                    <div className={styles.cardActions}>
                      <RoleSelect u={u} />
                      <UserActions u={u} />
                    </div>
                  </div>
                ))}
              </div>

              {/* Pagination */}
              {usersPerPage > 0 && usersPaged.total > usersPaged.per_page && (
                <AdminPagination
                  page={usersPage}
                  total={Math.ceil(usersPaged.total / usersPaged.per_page)}
                  onChange={setUsersPage}
                />
              )}
            </>
          )}
        </div>

        {/* ── Requests breakdown ─────────────────────────────────────────────── */}
        {stats && (
          <>
            <h2 className={styles.groupTitle}>Запросы</h2>
            <div className={styles.section}>
              <RequestsSection
                title="API пользователи (IP)"
                tab={apiTab}
                onTab={setApiTab}
                todayContent={<RequestsTable rows={stats.api_today} cols={['IP', 'Запросов']} />}
                allContent={<RequestsTable rows={stats.api_total} cols={['IP', 'Запросов']} />}
              />
            </div>
            <div className={styles.section}>
              <RequestsSection
                title="Категории"
                tab={catsTab}
                onTab={setCatsTab}
                todayContent={<RequestsTable rows={stats.cats_today} cols={['Категория', 'Запросов']} />}
                allContent={<RequestsTable rows={stats.cats_total} cols={['Категория', 'Запросов']} />}
              />
            </div>
            <div className={styles.section}>
              <RequestsSection
                title="MyShows"
                tab={myshowsTab}
                onTab={setMyshowsTab}
                todayContent={<RequestsTable rows={stats.myshows_today} cols={['Логин', 'Синхронизаций']} />}
                allContent={<RequestsTable rows={stats.myshows_total} cols={['Логин', 'Синхронизаций']} />}
              />
            </div>
          </>
        )}
      </div>
    </Layout>
  )
}

function AdminPagination({ page, total, onChange }: { page: number; total: number; onChange: (p: number) => void }) {
  const pages = Array.from({ length: total }, (_, i) => i + 1)
    .filter(p => p === 1 || p === total || Math.abs(p - page) <= 2)
    .reduce<(number | '…')[]>((acc, p, i, arr) => {
      if (i > 0 && p - (arr[i - 1] as number) > 1) acc.push('…')
      acc.push(p)
      return acc
    }, [])
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginTop: 12, flexWrap: 'wrap' }}>
      {(['«', '‹'] as const).map((ch, i) => (
        <APgBtn key={ch} disabled={page === 1} onClick={() => onChange(i === 0 ? 1 : page - 1)}>{ch}</APgBtn>
      ))}
      {pages.map((p, i) => p === '…'
        ? <span key={`e${i}`} style={{ color: '#666', padding: '0 3px' }}>…</span>
        : <APgBtn key={p} active={p === page} disabled={false} onClick={() => onChange(p as number)}>{p}</APgBtn>
      )}
      {(['›', '»'] as const).map((ch, i) => (
        <APgBtn key={ch} disabled={page === total} onClick={() => onChange(i === 0 ? page + 1 : total)}>{ch}</APgBtn>
      ))}
    </div>
  )
}

function APgBtn({ children, disabled, active, onClick }: {
  children: React.ReactNode; disabled: boolean; active?: boolean; onClick: () => void
}) {
  return (
    <button onClick={onClick} disabled={disabled} style={{
      padding: '2px 7px', borderRadius: 4, border: '1px solid',
      borderColor: active ? '#4a90e2' : '#444',
      background: active ? '#4a90e2' : 'none',
      color: disabled ? '#555' : active ? '#fff' : '#ccc',
      cursor: disabled ? 'default' : 'pointer',
      fontSize: '0.8rem', minWidth: 26,
    }}>{children}</button>
  )
}
