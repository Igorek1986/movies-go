import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
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

export default function AdminPage() {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [stats, setStats] = useState<Stats | null>(null)
  const [sysStats, setSysStats] = useState<SystemStats | null>(null)
  const [loading, setLoading] = useState(true)
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

  function toast(text: string, ok = true) {
    const id = Date.now()
    setToasts(prev => [...prev, { id, text, ok }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 3000)
  }

  const fetchSysStats = useCallback(async () => {
    const res = await fetch('/api/admin/system-stats')
    if (res.ok) setSysStats(await res.json())
  }, [])

  // Silent refresh — no loading spinner, no flash
  const refresh = useCallback(async () => {
    const [uRes, sRes] = await Promise.all([
      fetch('/api/admin/users'),
      fetch('/api/admin/stats'),
    ])
    if (uRes.ok) {
      const data: AdminUser[] = await uRes.json()
      setUsers(data)
      if (meId.current === null) {
        const me = data.find(u => u.is_admin)
        if (me) meId.current = me.id
      }
    }
    if (sRes.ok) setStats(await sRes.json())
  }, [])

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
    Promise.all([refresh(), fetchFixRtStatus(), fetchRefreshCardsStatus(), fetchBackfillCastStatus(), fetchSysStats()]).finally(() => setLoading(false))
    const sysInterval = setInterval(fetchSysStats, 5000)
    return () => clearInterval(sysInterval)
  }, [refresh, fetchSysStats]) // eslint-disable-line react-hooks/exhaustive-deps

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
      await refresh()
    } catch (e: unknown) {
      toast(e instanceof Error ? e.message : String(e), false)
    }
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

      <div className={styles.page}>
        <div className={styles.pageHeader}>
          <h1 className={styles.title}>Администрирование</h1>
          <div className={styles.headerNav}>
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

        {stats && (
          <div className={styles.stats}>
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.users}</p>
              <p className={styles.statLabel}>Пользователей</p>
            </div>
            {stats.users_today > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>+{stats.users_today}</p>
                <p className={styles.statLabel}>Новых сегодня</p>
              </div>
            )}
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.devices}</p>
              <p className={styles.statLabel}>Устройств</p>
            </div>
            {stats.devices_today > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>+{stats.devices_today}</p>
                <p className={styles.statLabel}>Устройств сегодня</p>
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
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.timecodes.toLocaleString()}</p>
              <p className={styles.statLabel}>Таймкодов</p>
            </div>
            {stats.timecodes_today > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>+{stats.timecodes_today.toLocaleString()}</p>
                <p className={styles.statLabel}>Таймкодов сегодня</p>
              </div>
            )}
            {stats.no_runtime_movies > 0 && (
              <div className={styles.statCard}>
                <p className={`${styles.statValue} ${styles.statWarn}`}>{stats.no_runtime_movies.toLocaleString()}</p>
                <p className={styles.statLabel}>Фильмов без runtime</p>
              </div>
            )}
            {stats.no_runtime_tv > 0 && (
              <div className={styles.statCard}>
                <p className={`${styles.statValue} ${styles.statWarn}`}>{stats.no_runtime_tv.toLocaleString()}</p>
                <p className={styles.statLabel}>Сериалов без runtime</p>
              </div>
            )}
            {stats.tmdb_refreshed_today > 0 && (
              <div className={styles.statCard}>
                <p className={styles.statValue}>{stats.tmdb_refreshed_today.toLocaleString()}</p>
                <p className={styles.statLabel}>Обновлено из TMDB сегодня</p>
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

        {/* ── Users ──────────────────────────────────────────────────────────── */}
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>Пользователи</h2>

          {loading && <p className={styles.empty}>Загрузка…</p>}
          {!loading && users.length === 0 && <p className={styles.empty}>Нет пользователей</p>}

          {!loading && users.length > 0 && (
            <>
              {/* Desktop table */}
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Имя</th>
                    <th>Роль</th>
                    <th>Устройств</th>
                    <th>Создан</th>
                    <th>Premium до</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {users.map(u => (
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

              {/* Mobile cards */}
              <div className={styles.cards}>
                {users.map(u => (
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
            </>
          )}
        </div>
      </div>
    </Layout>
  )
}
