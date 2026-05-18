import { useEffect, useState, useRef, useCallback } from 'react'
import s from './TgMiniAppPage.module.scss'

// ── Telegram SDK ─────────────────────────────────────────────────────────────

declare global {
  interface Window { Telegram?: { WebApp: { ready(): void; expand(): void; initData: string } } }
}

function getInitData() { return window.Telegram?.WebApp?.initData || '' }

// ── API helper ────────────────────────────────────────────────────────────────

async function api(method: string, path: string, body?: unknown) {
  const r = await fetch('/tg-app/api' + path, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': getInitData() },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  })
  const json = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((json as { error?: string }).error || String(r.status))
  return json
}

// ── Types ─────────────────────────────────────────────────────────────────────

interface AuthResp {
  ok: boolean; is_admin: boolean; is_linked: boolean
  user: { id: number; first_name: string; username?: string }
}
interface Device { id: number; name: string; timecodes_count: number; created_at: string }
interface Profile { profile_id: string; name: string }
interface DeviceDetail { id: number; name: string; token: string; profile_limit: number | null; profiles: Profile[] }
interface MeResp { username: string; role: string; role_label: string; device_count: number; device_limit: number | null; devices: Device[] }
interface UserRow { id: number; username: string; role: string; created_at: string; blocked_at?: string; device_count: number; tg_username?: string; premium_until?: string }
interface MsgRow { id: number; direction: 'in' | 'out'; text: string; created_at: string }
interface Conversation { user_telegram_id: number; user_username: string; messages: MsgRow[]; has_unread: boolean }
interface Stats { total_users: number; total_devices: number; total_timecodes: number; tg_linked: number; new_users_today: number; tcs_today: number; unread_support: number; role_counts: Record<string, number> }
interface SettingItem { key: string; value: string; default: string }
interface SettingsGroup { name: string; items: SettingItem[] }

type TabId = 'me' | 'users' | 'msgs' | 'stats' | 'cfg'

// ── Me tab ────────────────────────────────────────────────────────────────────

function MeTab({ auth }: { auth: AuthResp }) {
  const [me, setMe] = useState<MeResp | null>(null)
  const [device, setDevice] = useState<DeviceDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    if (!auth.is_linked) { setLoading(false); return }
    try { setMe(await api('GET', '/me')) } catch (e) { setErr(String(e)) }
    setLoading(false)
  }, [auth.is_linked])

  useEffect(() => { load() }, [load])

  async function openDevice(id: number) {
    try { setDevice(await api('GET', '/me/devices/' + id)) } catch (e) { alert('Ошибка: ' + e) }
  }

  async function createDevice() {
    const name = prompt('Название устройства:'); if (!name) return
    try { await api('POST', '/me/devices/create', { name }); await load() } catch (e) { alert('Ошибка: ' + e) }
  }

  async function renameDevice(id: number) {
    const name = prompt('Новое название:'); if (!name) return
    try { await api('POST', '/me/devices/' + id + '/rename', { name }); setDevice(d => d ? { ...d, name } : d) } catch (e) { alert('Ошибка: ' + e) }
  }

  async function deleteDevice(id: number) {
    if (!confirm('Удалить устройство?')) return
    try { await api('POST', '/me/devices/' + id + '/delete'); setDevice(null); await load() } catch (e) { alert('Ошибка: ' + e) }
  }

  async function regenerateToken(id: number) {
    if (!confirm('Сгенерировать новый токен?')) return
    try {
      const r = await api('POST', '/me/devices/' + id + '/regenerate')
      alert('Новый токен: ' + r.token)
      setDevice(d => d ? { ...d, token: r.token } : d)
    } catch (e) { alert('Ошибка: ' + e) }
  }

  async function clearTimecodes(id: number) {
    if (!confirm('Очистить всю историю?')) return
    try { await api('POST', '/me/devices/' + id + '/clear-timecodes'); alert('История очищена') } catch (e) { alert('Ошибка: ' + e) }
  }

  async function createProfile(deviceId: number) {
    const name = prompt('Название профиля:'); if (!name) return
    try {
      await api('POST', '/me/devices/' + deviceId + '/profiles/create', { name })
      setDevice(await api('GET', '/me/devices/' + deviceId))
    } catch (e) { alert('Ошибка: ' + e) }
  }

  async function deleteProfile(deviceId: number, profileId: string) {
    if (!confirm('Удалить профиль?')) return
    try {
      await api('POST', '/me/devices/' + deviceId + '/profiles/' + profileId + '/delete')
      setDevice(await api('GET', '/me/devices/' + deviceId))
    } catch (e) { alert('Ошибка: ' + e) }
  }

  if (loading) return <div className={s.loading}>Загрузка…</div>
  if (err) return <div className={s.section}><p className={s.muted}>{err}</p></div>

  if (!auth.is_linked) return (
    <div className={s.section}>
      <div className={s.card}>
        <p>Telegram не привязан к аккаунту.</p>
        <p className={s.muted} style={{ marginTop: 6 }}>Перейдите в личный кабинет на сайте, чтобы привязать Telegram.</p>
      </div>
    </div>
  )

  if (device) return (
    <div className={s.section}>
      <button className={`${s.btn} ${s.btnSecondary} ${s.btnSm}`} onClick={() => setDevice(null)} style={{ marginBottom: 12 }}>← Назад</button>
      <h2 className={s.h2}>{device.name}</h2>
      <div className={s.card}>
        <div className={s.row}>
          <span>Токен</span>
          <code style={{ fontSize: 11, wordBreak: 'break-all' }}>{device.token}</code>
        </div>
      </div>
      <div className={s.btnGroup}>
        <button className={`${s.btn} ${s.btnSecondary} ${s.btnSm}`} onClick={() => renameDevice(device.id)}>Переименовать</button>
        <button className={`${s.btn} ${s.btnSm}`} onClick={() => regenerateToken(device.id)}>Новый токен</button>
        <button className={`${s.btn} ${s.btnSm} ${s.btnDanger}`} onClick={() => clearTimecodes(device.id)}>Очистить историю</button>
        <button className={`${s.btn} ${s.btnSm} ${s.btnDanger}`} onClick={() => deleteDevice(device.id)}>Удалить</button>
      </div>
      <h3 className={s.h3}>Профили ({device.profiles?.length ?? 0} / {device.profile_limit ?? '∞'})</h3>
      <div className={s.card}>
        {device.profiles?.length ? device.profiles.map(p => (
          <div key={p.profile_id} className={s.row}>
            <div>
              <div>{p.name}</div>
              <div className={s.muted}>{p.profile_id}</div>
            </div>
            <button className={`${s.btn} ${s.btnSm} ${s.btnDanger}`} onClick={() => deleteProfile(device.id, p.profile_id)}>Удалить</button>
          </div>
        )) : <p className={s.muted}>Нет профилей</p>}
      </div>
      <button className={`${s.btn} ${s.btnSecondary}`} style={{ width: '100%', marginTop: 8 }} onClick={() => createProfile(device.id)}>+ Добавить профиль</button>
    </div>
  )

  if (!me) return null
  const roleClass = me.role === 'premium' ? s.badgePremium : me.role === 'super' ? s.badgeSuper : s.badgeSimple

  return (
    <div className={s.section}>
      <h2 className={s.h2}>Мой аккаунт</h2>
      <div className={s.card}>
        <div className={s.row}><span>Логин</span><strong>{me.username}</strong></div>
        <div className={s.row}><span>Роль</span><span className={`${s.badge} ${roleClass}`}>{me.role_label}</span></div>
        <div className={s.row}><span>Устройств</span><span>{me.device_count} / {me.device_limit ?? '∞'}</span></div>
      </div>
      <h3 className={s.h3}>Устройства</h3>
      <div className={s.card}>
        {me.devices.length ? me.devices.map(d => (
          <div key={d.id} className={s.row}>
            <div>
              <div>{d.name}</div>
              <div className={s.muted}>{d.timecodes_count} таймкодов · {d.created_at}</div>
            </div>
            <button className={`${s.btn} ${s.btnSm} ${s.btnSecondary}`} onClick={() => openDevice(d.id)}>Управление</button>
          </div>
        )) : <p className={s.muted}>Нет устройств</p>}
      </div>
      <button className={s.btn} style={{ width: '100%', marginTop: 8 }} onClick={createDevice}>+ Добавить устройство</button>
    </div>
  )
}

// ── Users tab ─────────────────────────────────────────────────────────────────

function UsersActionsBlock() {
  const [parserDate, setParserDate] = useState('')

  async function run(path: string, label: string) {
    try {
      const d = await api('POST', path)
      if (d.message) alert(label + ': ' + d.message)
      else if (d.days) alert(label + ': +' + d.days + ' дней')
      else alert(label + ': готово')
    } catch (e) { alert('Ошибка: ' + e) }
  }

  async function resetParser() {
    if (!parserDate) return
    const [y, m, day] = parserDate.split('-')
    const formatted = `${day}.${m}.${y}`
    try { await api('POST', '/admin/reset-parser', { date: formatted }); alert('Дата парсера сброшена: ' + formatted) }
    catch (e) { alert('Ошибка: ' + e) }
  }

  return (
    <div className={s.section}>
      <h2 className={s.h2}>Действия</h2>
      <div className={s.card}>
        <div className={s.actionsGrid}>
          <button className={`${s.btn} ${s.btnSecondary}`} onClick={() => run('/admin/check-premium', 'Проверить Premium')}>Проверить Premium</button>
          <button className={`${s.btn} ${s.btnSecondary}`} onClick={() => run('/admin/extend-premium', 'Продлить всем')}>Продлить всем</button>
          <button className={`${s.btn} ${s.btnSecondary}`} onClick={() => run('/admin/refresh-episodes', 'Обновить эпизоды')}>Обновить эпизоды</button>
          <button className={`${s.btn} ${s.btnSecondary}`} onClick={() => run('/admin/fix-runtime', 'Обновить runtime')}>Обновить runtime</button>
        </div>
        <div className={s.parserReset}>
          <span className={s.muted}>Сброс парсера:</span>
          <input type="date" className={s.input} value={parserDate} onChange={e => setParserDate(e.target.value)} style={{ flex: 1 }} />
          <button className={`${s.btn} ${s.btnSecondary}`} onClick={resetParser} disabled={!parserDate}>Применить</button>
        </div>
      </div>
    </div>
  )
}

function UsersTab() {
  const [users, setUsers] = useState<UserRow[]>([])
  const [q, setQ] = useState('')
  const timer = useRef<number | undefined>(undefined)

  const load = useCallback(async (search: string) => {
    try { const d = await api('GET', '/users' + (search ? '?q=' + encodeURIComponent(search) : '')); setUsers(d.users) }
    catch {}
  }, [])

  useEffect(() => { load('') }, [load])

  function handleSearch(v: string) {
    setQ(v)
    clearTimeout(timer.current)
    timer.current = window.setTimeout(() => load(v), 400)
  }

  async function setRole(id: number, role: string) {
    try { await api('POST', '/users/' + id + '/role', { role }); load(q) }
    catch (e) { alert('Ошибка: ' + e) }
  }

  async function blockUser(id: number) {
    const reason = prompt('Причина блокировки (необязательно):'); if (reason === null) return
    try { await api('POST', '/users/' + id + '/block', { reason }); load(q) }
    catch (e) { alert('Ошибка: ' + e) }
  }

  async function unblockUser(id: number) {
    try { await api('POST', '/users/' + id + '/unblock'); load(q) }
    catch (e) { alert('Ошибка: ' + e) }
  }

  async function resetMyShows(id: number) {
    try { await api('POST', '/users/' + id + '/reset-myshows'); alert('Кулдаун MyShows сброшен') }
    catch (e) { alert('Ошибка: ' + e) }
  }

  async function cleanupLimits(id: number, username: string) {
    if (!confirm('Удалить устройства/таймкоды сверх лимита для ' + username + '?')) return
    try {
      const d = await api('POST', '/users/' + id + '/cleanup-limits')
      alert('Удалено устройств: ' + d.deleted)
      load(q)
    } catch (e) { alert('Ошибка: ' + e) }
  }

  async function deleteUser(id: number, username: string) {
    if (!confirm('Удалить пользователя ' + username + '? Это необратимо.')) return
    try { await api('POST', '/users/' + id + '/delete'); load(q) }
    catch (e) { alert('Ошибка: ' + e) }
  }

  return (
    <>
      <UsersActionsBlock />
    <div className={s.section}>
      <h2 className={s.h2}>Пользователи ({users.length})</h2>
      <input className={s.input} placeholder="Поиск по имени…" value={q} onChange={e => handleSearch(e.target.value)} style={{ marginBottom: 12 }} />
      <div className={s.card}>
        {users.length ? users.map(u => {
          const roleClass = u.role === 'premium' ? s.badgePremium : u.role === 'super' ? s.badgeSuper : s.badgeSimple
          return (
            <div key={u.id} className={s.userCard}>
              <div className={s.userCardHeader}>
                <div>
                  <strong>{u.username}</strong>
                  <span className={`${s.badge} ${roleClass}`} style={{ marginLeft: 4 }}>{u.role}</span>
                  {u.blocked_at && <span className={`${s.badge} ${s.badgeBlocked}`} style={{ marginLeft: 4 }}>blocked</span>}
                </div>
                <div className={s.muted}>
                  {u.tg_username ? '@' + u.tg_username + ' · ' : ''}{u.device_count} уст · {u.created_at}
                  {u.premium_until && <span> · до {u.premium_until}</span>}
                </div>
              </div>
              <div className={s.userCardActions}>
                <select className={s.selectSm} value={u.role} onChange={e => setRole(u.id, e.target.value)}>
                  {['simple', 'premium', 'super'].map(r => <option key={r}>{r}</option>)}
                </select>
                {u.blocked_at
                  ? <button className={`${s.btn} ${s.btnSm} ${s.btnSecondary}`} onClick={() => unblockUser(u.id)}>Разблокировать</button>
                  : <button className={`${s.btn} ${s.btnSm} ${s.btnDanger}`} onClick={() => blockUser(u.id)}>Заблокировать</button>}
                <button className={`${s.btn} ${s.btnSm} ${s.btnSecondary}`} onClick={() => setRole(u.id, 'super')}>Адм.</button>
                <button className={`${s.btn} ${s.btnSm} ${s.btnSecondary}`} onClick={() => resetMyShows(u.id)}>MyShows</button>
                <button className={`${s.btn} ${s.btnSm} ${s.btnSecondary}`} onClick={() => cleanupLimits(u.id, u.username)}>Лимиты</button>
                <button className={`${s.btn} ${s.btnSm} ${s.btnDanger}`} onClick={() => deleteUser(u.id, u.username)}>Удалить</button>
              </div>
            </div>
          )
        }) : <p className={s.muted}>Нет результатов</p>}
      </div>
    </div>
    </>
  )
}

// ── Messages tab ──────────────────────────────────────────────────────────────

function MsgsTab() {
  const [convs, setConvs] = useState<Conversation[]>([])
  const [open, setOpen] = useState<Conversation | null>(null)
  const [reply, setReply] = useState('')

  async function load() {
    try { const d = await api('GET', '/messages'); setConvs(d.conversations) }
    catch {}
  }

  useEffect(() => { load() }, []) // eslint-disable-line

  async function openConv(c: Conversation) {
    try { await api('POST', '/messages/' + c.user_telegram_id + '/read') } catch {}
    setOpen(c)
    setReply('')
  }

  async function sendReply() {
    if (!open || !reply.trim()) return
    try {
      await api('POST', '/messages/' + open.user_telegram_id + '/reply', { text: reply })
      setReply('')
      const d = await api('GET', '/messages')
      const updated = d.conversations.find((c: Conversation) => c.user_telegram_id === open.user_telegram_id)
      if (updated) setOpen(updated)
      setConvs(d.conversations)
    } catch (e) { alert('Ошибка: ' + e) }
  }

  if (open) return (
    <div className={s.section}>
      <button className={`${s.btn} ${s.btnSecondary} ${s.btnSm}`} onClick={() => { setOpen(null); load() }} style={{ marginBottom: 12 }}>← Назад</button>
      <h2 className={s.h2}>@{open.user_username || open.user_telegram_id}</h2>
      <div className={s.msgList}>
        {open.messages.map(m => (
          <div key={m.id} className={m.direction === 'out' ? s.msgOut : s.msgIn}>
            <div className={s.msgBubble}>{m.text}</div>
            <div className={s.muted} style={{ fontSize: 11, marginTop: 2 }}>{m.created_at}</div>
          </div>
        ))}
      </div>
      <div className={s.replyRow}>
        <input className={s.input} placeholder="Ответить…" value={reply} onChange={e => setReply(e.target.value)} onKeyDown={e => e.key === 'Enter' && sendReply()} />
        <button className={s.btn} onClick={sendReply}>Отправить</button>
      </div>
    </div>
  )

  return (
    <div className={s.section}>
      <h2 className={s.h2}>Сообщения</h2>
      <div className={s.card}>
        {convs.length ? convs.map(c => {
          const last = c.messages[c.messages.length - 1]
          return (
            <div key={c.user_telegram_id} className={`${s.row} ${s.rowClickable}`} onClick={() => openConv(c)}>
              <div>
                <div className={s.rowTitle}>
                  <strong>@{c.user_username || c.user_telegram_id}</strong>
                  {c.has_unread && <span className={s.unreadDot} />}
                </div>
                <div className={s.muted}>{last?.text?.slice(0, 50)}</div>
              </div>
              <div className={s.muted}>{last?.created_at}</div>
            </div>
          )
        }) : <p className={s.muted}>Нет сообщений</p>}
      </div>
    </div>
  )
}

// ── Stats tab ─────────────────────────────────────────────────────────────────

function StatsTab() {
  const [stats, setStats] = useState<Stats | null>(null)

  useEffect(() => {
    api('GET', '/stats').then(setStats).catch(() => {})
  }, [])

  if (!stats) return <div className={s.loading}>Загрузка…</div>

  return (
    <div className={s.section}>
      <h2 className={s.h2}>Статистика</h2>
      <div className={s.card}>
        <div className={s.row}><span>Всего пользователей</span><strong>{stats.total_users}</strong></div>
        <div className={s.row}><span>Premium</span><strong>{stats.role_counts?.premium ?? 0}</strong></div>
        <div className={s.row}><span>Simple</span><strong>{stats.role_counts?.simple ?? 0}</strong></div>
        <div className={s.row}><span>Super</span><strong>{stats.role_counts?.super ?? 0}</strong></div>
        <div className={s.row}><span>С Telegram</span><strong>{stats.tg_linked}</strong></div>
        <div className={s.row}><span>Новых сегодня</span><strong>{stats.new_users_today}</strong></div>
      </div>
      <div className={s.card} style={{ marginTop: 8 }}>
        <div className={s.row}><span>Устройств</span><strong>{stats.total_devices}</strong></div>
        <div className={s.row}><span>Таймкодов</span><strong>{stats.total_timecodes}</strong></div>
        <div className={s.row}><span>Активных сегодня</span><strong>{stats.tcs_today}</strong></div>
        <div className={s.row}><span>Непрочитанных</span><strong>{stats.unread_support}</strong></div>
      </div>
    </div>
  )
}

// ── Settings tab ──────────────────────────────────────────────────────────────

function CfgTab() {
  const [groups, setGroups] = useState<SettingsGroup[]>([])

  useEffect(() => {
    api('GET', '/settings').then(d => setGroups(d.groups)).catch(() => {})
  }, [])

  async function save(key: string, value: string) {
    try { await api('POST', '/settings', { key, value }) }
    catch (e) { alert('Ошибка: ' + e) }
  }

  return (
    <div className={s.section}>
      <h2 className={s.h2}>Настройки</h2>
      {groups.map(g => (
        <div key={g.name}>
          <h3 className={s.h3} style={{ marginTop: 12, marginBottom: 4 }}>{g.name}</h3>
          <div className={s.card}>
            {g.items.map(i => (
              <div key={i.key} className={s.row}>
                <span className={s.settingKey}>{i.key}</span>
                <input
                  className={s.inputSm}
                  defaultValue={i.value}
                  onBlur={e => save(i.key, e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && save(i.key, (e.target as HTMLInputElement).value)}
                />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

// ── Main ──────────────────────────────────────────────────────────────────────

export default function TgMiniAppPage() {
  const [sdkReady, setSdkReady] = useState(false)
  const [auth, setAuth] = useState<AuthResp | null>(null)
  const [authErr, setAuthErr] = useState('')
  const [tab, setTab] = useState<TabId>('me')

  useEffect(() => {
    if (document.querySelector('script[src*="telegram-web-app"]')) { setSdkReady(true); return }
    const sc = document.createElement('script')
    sc.src = 'https://telegram.org/js/telegram-web-app.js'
    sc.onload = () => setSdkReady(true)
    sc.onerror = () => setSdkReady(true)
    document.head.appendChild(sc)
  }, [])

  useEffect(() => {
    if (!sdkReady) return
    const tg = window.Telegram?.WebApp
    if (tg) { tg.ready(); tg.expand() }
    fetch('/tg-app/api/auth', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ initData: tg?.initData || '' }),
    })
      .then(r => r.json())
      .then(d => { if (d.ok) setAuth(d); else setAuthErr('Ошибка авторизации') })
      .catch(() => setAuthErr('Нет соединения'))
  }, [sdkReady])

  if (!sdkReady || (!auth && !authErr)) return <div className={s.loading}>Загрузка…</div>
  if (authErr) return <div className={s.section}><p>{authErr}</p></div>
  if (!auth?.ok) return <div className={s.section}><p>Ошибка</p></div>

  const tabs: Array<{ id: TabId; label: string }> = [
    { id: 'me', label: '👤 Мой аккаунт' },
    ...(auth.is_admin ? [
      { id: 'users' as TabId, label: '👥 Пользователи' },
      { id: 'msgs' as TabId, label: '💬 Сообщения' },
      { id: 'stats' as TabId, label: '📊 Статистика' },
      { id: 'cfg' as TabId, label: '⚙️ Настройки' },
    ] : []),
  ]

  return (
    <div className={s.root}>
      <div className={s.tabs}>
        {tabs.map(t => (
          <button key={t.id} className={`${s.tab}${tab === t.id ? ' ' + s.tabActive : ''}`} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'me'    && <MeTab auth={auth} />}
      {tab === 'users' && <UsersTab />}
      {tab === 'msgs'  && <MsgsTab />}
      {tab === 'stats' && <StatsTab />}
      {tab === 'cfg'   && <CfgTab />}
    </div>
  )
}
