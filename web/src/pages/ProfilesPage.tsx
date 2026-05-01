import { useEffect, useState, useCallback } from 'react'
import { useAuth } from '@/hooks/useAuth'
import Layout from '@/components/Layout'
import styles from './ProfilesPage.module.scss'

interface Device {
  id: number
  name: string
  token: string
  created_at: string
  timecodes_count: number
}

interface LampaProfile {
  profile_id: string
  name: string
  icon: string
  child: boolean
  params: Record<string, unknown>
  timecodes_count: number
}

export default function ProfilesPage() {
  const { user } = useAuth()
  const [devices, setDevices] = useState<Device[]>([])
  const [visibleTokens, setVisibleTokens] = useState<Set<number>>(new Set())
  const [copied, setCopied] = useState<number | null>(null)
  // Create device
  const [newDeviceName, setNewDeviceName] = useState('')
  const [createLoading, setCreateLoading] = useState(false)
  // Link by code
  const [linkCode, setLinkCode] = useState('')
  const [linkDeviceId, setLinkDeviceId] = useState<number | 'new'>('new')
  const [linkNewName, setLinkNewName] = useState('')
  const [linkLoading, setLinkLoading] = useState(false)
  const [linkError, setLinkError] = useState('')
  const [linkSuccess, setLinkSuccess] = useState('')
  // Profiles per device
  const [openProfilesFor, setOpenProfilesFor] = useState<number | null>(null)
  const [profiles, setProfiles] = useState<LampaProfile[]>([])
  const [profilesLimit, setProfilesLimit] = useState<number>(0)
  const [newProfileName, setNewProfileName] = useState('')
  const [newProfileId, setNewProfileId] = useState('')
  const [profileError, setProfileError] = useState('')
  // Change password
  const [pwCurrent, setPwCurrent] = useState('')
  const [pwNew, setPwNew] = useState('')
  const [pwLoading, setPwLoading] = useState(false)
  const [pwMsg, setPwMsg] = useState('')
  // Delete account
  const [delPw, setDelPw] = useState('')
  const [delLoading, setDelLoading] = useState(false)

  const fetchDevices = useCallback(async () => {
    const res = await fetch('/api/devices')
    if (res.ok) setDevices(await res.json())
  }, [])

  useEffect(() => { fetchDevices() }, [fetchDevices])

  function toggleToken(id: number) {
    setVisibleTokens(s => {
      const n = new Set(s)
      n.has(id) ? n.delete(id) : n.add(id)
      return n
    })
  }

  async function copyToken(id: number, token: string) {
    await navigator.clipboard.writeText(token).catch(() => {})
    setCopied(id)
    setTimeout(() => setCopied(null), 1500)
  }

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    setCreateLoading(true)
    const res = await fetch('/api/devices', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newDeviceName }),
    })
    setCreateLoading(false)
    if (res.ok) {
      setNewDeviceName('')
      fetchDevices()
    } else {
      const d = await res.json().catch(() => ({}))
      alert(d.error || 'Ошибка создания устройства')
    }
  }

  async function handleLink(e: React.FormEvent) {
    e.preventDefault()
    setLinkError('')
    setLinkSuccess('')
    setLinkLoading(true)
    const body: Record<string, any> = { code: linkCode }
    if (linkDeviceId === 'new') {
      body.name = linkNewName || 'Lampa'
    } else {
      // link to existing device — not supported by backend directly,
      // but we can create device code association via /api/device/link
      body.name = devices.find(d => d.id === linkDeviceId)?.name || 'Lampa'
    }
    const res = await fetch('/api/device/link', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    setLinkLoading(false)
    if (res.ok) {
      setLinkCode('')
      setLinkSuccess('Устройство успешно привязано!')
      fetchDevices()
    } else {
      const d = await res.json().catch(() => ({}))
      setLinkError(d.error || 'Ошибка привязки')
    }
  }

  async function handleRename(id: number, currentName: string) {
    const name = window.prompt('Новое название:', currentName)
    if (!name || name.trim() === currentName) return
    await fetch(`/api/devices/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.trim() }),
    })
    fetchDevices()
  }

  async function handleRegenToken(id: number, name: string) {
    if (!confirm(`Старый токен устройства «${name}» перестанет работать в Lampa. Продолжить?`)) return
    const res = await fetch(`/api/devices/${id}/regenerate-token`, { method: 'POST' })
    if (res.ok) {
      fetchDevices()
      setVisibleTokens(s => { const n = new Set(s); n.add(id); return n })
    }
  }

  async function handleClearTimecodes(id: number, name: string) {
    if (!confirm(`Удалить все таймкоды устройства «${name}»?`)) return
    await fetch(`/api/devices/${id}/timecodes`, { method: 'DELETE' })
    fetchDevices()
  }

  async function handleDeleteDevice(id: number, name: string) {
    if (!confirm(`Удалить устройство «${name}» и все его таймкоды?`)) return
    await fetch(`/api/devices/${id}`, { method: 'DELETE' })
    fetchDevices()
    if (openProfilesFor === id) setOpenProfilesFor(null)
  }

  async function openProfiles(id: number) {
    if (openProfilesFor === id) { setOpenProfilesFor(null); return }
    setOpenProfilesFor(id)
    setProfileError('')
    setNewProfileName('')
    const res = await fetch(`/api/devices/${id}/profiles`)
    if (res.ok) {
      const data = await res.json()
      setProfiles(data.profiles || [])
      setProfilesLimit(data.limit || 0)
    }
  }

  async function reloadProfiles() {
    if (!openProfilesFor) return
    const r = await fetch(`/api/devices/${openProfilesFor}/profiles`)
    if (r.ok) { const d = await r.json(); setProfiles(d.profiles || []) }
  }

  async function handleCreateProfile(e: React.FormEvent) {
    e.preventDefault()
    if (!openProfilesFor) return
    setProfileError('')
    const res = await fetch(`/api/devices/${openProfilesFor}/profiles`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newProfileName, profile_id: newProfileId || undefined }),
    })
    if (res.ok) {
      setNewProfileName('')
      setNewProfileId('')
      reloadProfiles()
    } else {
      const d = await res.json().catch(() => ({}))
      setProfileError(d.error || 'Ошибка создания профиля')
    }
  }

  async function handleRenameProfile(profileId: string, currentName: string) {
    const name = window.prompt('Новое название профиля:', currentName)
    if (!name || name.trim() === currentName) return
    await fetch(`/api/devices/${openProfilesFor}/profiles/${profileId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.trim() }),
    })
    reloadProfiles()
  }

  async function handleToggleChild(p: LampaProfile) {
    await fetch(`/api/devices/${openProfilesFor}/profiles/${p.profile_id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ child: !p.child }),
    })
    reloadProfiles()
  }

  async function handleEditParams(p: LampaProfile) {
    const current = JSON.stringify(p.params ?? {}, null, 2)
    const input = window.prompt('Параметры профиля (JSON):', current)
    if (input === null) return
    try {
      const params = JSON.parse(input)
      await fetch(`/api/devices/${openProfilesFor}/profiles/${p.profile_id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ params }),
      })
      reloadProfiles()
    } catch {
      alert('Неверный JSON')
    }
  }

  async function handleClearProfileTimecodes(profileId: string, name: string) {
    if (!confirm(`Удалить все таймкоды профиля «${name}»?`)) return
    await fetch(`/api/devices/${openProfilesFor}/profiles/${profileId}/timecodes`, { method: 'DELETE' })
    reloadProfiles()
  }

  async function handleDeleteProfile(profileId: string, name: string) {
    if (!confirm(`Удалить профиль «${name}»?`)) return
    await fetch(`/api/devices/${openProfilesFor}/profiles/${profileId}`, { method: 'DELETE' })
    reloadProfiles()
  }

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault()
    setPwMsg('')
    setPwLoading(true)
    const res = await fetch('/api/change-password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ current_password: pwCurrent, new_password: pwNew }),
    })
    setPwLoading(false)
    if (res.ok) {
      setPwCurrent(''); setPwNew('')
      setPwMsg('Пароль изменён')
    } else {
      const d = await res.json().catch(() => ({}))
      setPwMsg(d.error || 'Ошибка смены пароля')
    }
  }

  async function handleDeleteAccount(e: React.FormEvent) {
    e.preventDefault()
    if (!confirm('Удалить аккаунт и все данные? Это необратимо!')) return
    setDelLoading(true)
    const res = await fetch('/api/account', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: delPw }),
    })
    setDelLoading(false)
    if (res.ok) {
      window.location.href = '/login'
    } else {
      const d = await res.json().catch(() => ({}))
      alert(d.error || 'Ошибка удаления аккаунта')
    }
  }

  const roleLabel: Record<string, string> = { simple: 'Базовый', premium: 'Премиум', super: 'Супер' }
  const maxDevices = user?.role === 'super' ? null : user?.role === 'premium' ? 8 : 3

  return (
    <Layout>
      <div className={styles.page}>

        {/* ── Devices ── */}
        <section className={styles.section}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>
              Мои устройства
              <span className={styles.roleBadge}>{roleLabel[user?.role ?? ''] ?? user?.role}</span>
            </h2>
            {maxDevices !== null && (
              <span className={styles.limitHint}>{devices.length} / {maxDevices}</span>
            )}
          </div>

          {devices.length === 0 ? (
            <p className={styles.empty}>Устройств ещё нет</p>
          ) : (
            <div className={styles.deviceTable}>
              {devices.map(d => (
                <div key={d.id} className={styles.deviceRow}>
                  <div className={styles.deviceInfo}>
                    <strong className={styles.deviceName}>{d.name}</strong>
                    <div className={styles.tokenRow}>
                      <code
                        className={styles.tokenCode}
                        title="Нажмите, чтобы скопировать"
                        onClick={() => copyToken(d.id, d.token)}
                      >
                        {visibleTokens.has(d.id) ? d.token : d.token.slice(0, 4) + '-••••-••••-••••'}
                      </code>
                      <button className={styles.btnIcon} onClick={() => toggleToken(d.id)} title="Показать/скрыть">
                        {visibleTokens.has(d.id) ? '🙈' : '👁'}
                      </button>
                      {copied === d.id && <span className={styles.copiedHint}>Скопировано!</span>}
                    </div>
                    <span className={styles.deviceMeta}>
                      Таймкодов: {d.timecodes_count} · {new Date(d.created_at).toLocaleDateString('ru-RU')}
                    </span>
                  </div>
                  <div className={styles.deviceActions}>
                    <button className={styles.btnSm} onClick={() => openProfiles(d.id)}>
                      Профили{openProfilesFor === d.id ? ' ▲' : ' ▼'}
                    </button>
                    <button className={styles.btnSm} onClick={() => handleRename(d.id, d.name)}>Переименовать</button>
                    <button className={styles.btnSm} onClick={() => handleRegenToken(d.id, d.name)}>Новый токен</button>
                    <button className={`${styles.btnSm} ${styles.warning}`} onClick={() => handleClearTimecodes(d.id, d.name)}>Очистить</button>
                    <button className={`${styles.btnSm} ${styles.danger}`} onClick={() => handleDeleteDevice(d.id, d.name)}>Удалить</button>
                  </div>

                  {openProfilesFor === d.id && (
                    <div className={styles.profilesPanel}>
                      <h4 className={styles.profilesTitle}>Профили Lampa</h4>
                      {profiles.length === 0 && <p className={styles.empty}>Нет профилей</p>}
                      {profiles.map(p => (
                        <div key={p.profile_id} className={styles.profileCard}>
                          <div className={styles.profileCardTop}>
                            <div className={styles.profileCardLeft}>
                              <strong className={styles.profileName}>{p.name}</strong>
                              <div className={styles.profileMeta}>
                                <code className={styles.profileId}>ID: {p.profile_id}</code>
                                <span>· таймкодов: {p.timecodes_count}</span>
                              </div>
                            </div>
                            <div className={styles.profileCardActions}>
                              <button className={styles.btnIcon} title="Переименовать" onClick={() => handleRenameProfile(p.profile_id, p.name)}>✏️</button>
                              <button className={`${styles.btnSm} ${styles.warning}`} onClick={() => handleClearProfileTimecodes(p.profile_id, p.name)}>Очистить</button>
                              <button className={`${styles.btnSm} ${styles.danger}`} onClick={() => handleDeleteProfile(p.profile_id, p.name)}>Удалить</button>
                            </div>
                          </div>
                          <div className={styles.profileCardBottom}>
                            <button
                              className={`${styles.btnSm} ${p.child ? styles.active : ''}`}
                              onClick={() => handleToggleChild(p)}
                            >
                              Детский {p.child ? '✓' : ''}
                            </button>
                            <button className={styles.btnSm} onClick={() => handleEditParams(p)}>
                              Параметры
                            </button>
                          </div>
                        </div>
                      ))}
                      {profileError && <p className={styles.errorText}>{profileError}</p>}
                      {(profilesLimit === 0 || profiles.length < profilesLimit) && (
                        <form className={styles.inlineForm} onSubmit={handleCreateProfile}>
                          <input
                            className={styles.input}
                            placeholder="Название профиля"
                            value={newProfileName}
                            onChange={e => setNewProfileName(e.target.value)}
                            required
                          />
                          <input
                            className={`${styles.input} ${styles.inputMono}`}
                            placeholder="ID (авто если пусто)"
                            value={newProfileId}
                            onChange={e => setNewProfileId(e.target.value.replace(/[^a-zA-Z0-9_-]/g, '').slice(0, 32))}
                            maxLength={32}
                          />
                          <button className={styles.btnPrimary} type="submit">Добавить</button>
                        </form>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {(maxDevices === null || devices.length < maxDevices) ? (
            <form className={styles.inlineForm} onSubmit={handleCreate}>
              <input
                className={styles.input}
                placeholder="Название нового устройства"
                value={newDeviceName}
                onChange={e => setNewDeviceName(e.target.value)}
                required maxLength={100}
              />
              <button className={styles.btnPrimary} type="submit" disabled={createLoading}>
                {createLoading ? 'Создание…' : 'Добавить устройство'}
              </button>
            </form>
          ) : (
            <p className={styles.limitReached}>Достигнут лимит устройств ({maxDevices})</p>
          )}
        </section>

        {/* ── Link by code ── */}
        <details className={styles.details}>
          <summary className={styles.summary}>Привязать устройство по коду</summary>
          <div className={styles.detailsBody}>
            <p className={styles.hint}>В настройках плагина нажмите «Привязать устройство» — на экране появится 6-значный код.</p>
            {linkError && <p className={styles.errorText}>{linkError}</p>}
            {linkSuccess && <p className={styles.successText}>{linkSuccess}</p>}
            <form className={styles.linkForm} onSubmit={handleLink}>
              <div className={styles.formGrid}>
                <input
                  className={styles.input}
                  placeholder="Код (6 цифр)"
                  value={linkCode}
                  onChange={e => setLinkCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  pattern="[0-9]{6}" inputMode="numeric" maxLength={6} required
                />
                <select
                  className={styles.select}
                  value={linkDeviceId}
                  onChange={e => setLinkDeviceId(e.target.value === 'new' ? 'new' : Number(e.target.value))}
                >
                  {devices.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                  {(maxDevices === null || devices.length < (maxDevices ?? 99)) && (
                    <option value="new">+ Новое устройство</option>
                  )}
                </select>
              </div>
              {linkDeviceId === 'new' && (
                <input
                  className={styles.input}
                  placeholder="Название нового устройства"
                  value={linkNewName}
                  onChange={e => setLinkNewName(e.target.value)}
                  maxLength={100}
                />
              )}
              <button className={styles.btnPrimary} type="submit" disabled={linkLoading || linkCode.length < 6}>
                {linkLoading ? 'Привязка…' : 'Привязать'}
              </button>
            </form>
          </div>
        </details>

        {/* ── Change password ── */}
        <details className={styles.details}>
          <summary className={styles.summary}>Настройки аккаунта</summary>
          <div className={styles.detailsBody}>
            <h4 className={styles.subTitle}>Сменить пароль</h4>
            {pwMsg && <p className={pwMsg === 'Пароль изменён' ? styles.successText : styles.errorText}>{pwMsg}</p>}
            <form className={styles.formCol} onSubmit={handleChangePassword}>
              <input className={styles.input} type="password" placeholder="Текущий пароль" value={pwCurrent} onChange={e => setPwCurrent(e.target.value)} required />
              <input className={styles.input} type="password" placeholder="Новый пароль (мин. 6 символов)" value={pwNew} onChange={e => setPwNew(e.target.value)} minLength={6} required />
              <button className={styles.btnPrimary} type="submit" disabled={pwLoading}>{pwLoading ? 'Сохранение…' : 'Сменить пароль'}</button>
            </form>

            <hr className={styles.hr} />

            <h4 className={styles.subTitle} style={{ color: 'var(--danger, #e05252)' }}>Удалить аккаунт</h4>
            <p className={styles.hint}>Все устройства и таймкоды будут удалены безвозвратно.</p>
            <form className={styles.formCol} onSubmit={handleDeleteAccount}>
              <input className={styles.input} type="password" placeholder="Введите пароль для подтверждения" value={delPw} onChange={e => setDelPw(e.target.value)} required />
              <button className={`${styles.btnPrimary} ${styles.danger}`} type="submit" disabled={delLoading}>{delLoading ? 'Удаление…' : 'Удалить аккаунт'}</button>
            </form>
          </div>
        </details>

      </div>
    </Layout>
  )
}
