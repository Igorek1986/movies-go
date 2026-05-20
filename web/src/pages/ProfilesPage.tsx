import { useEffect, useState, useCallback, useRef, useLayoutEffect } from 'react'
import { useAuth } from '@/hooks/useAuth'
import Layout from '@/components/Layout'
import PasswordInput from '@/components/PasswordInput'
import styles from './ProfilesPage.module.scss'

interface TelegramStatus {
  linked: boolean
  username?: string
  telegram_id?: number
}

interface NotificationSettings {
  enabled: boolean
  timezone: string
  notify_start: number
  notify_end: number
}

interface Device {
  id: number
  name: string
  token: string
  created_at: string
  timecodes_count: number
}

interface Profile {
  profile_id: string
  name: string
  icon: string
  child: boolean
  params: Record<string, unknown>
  timecodes_count: number
}

interface SyncLogEntry {
  type: 'status' | 'error' | 'stage'
  message?: string
  stage?: string
  current?: number
  total?: number
  name?: string
}

const PROFILE_ICON_EXTS: Record<string, string> = {
  id1: 'png', id2: 'png', id3: 'png', id4: 'png', id5: 'png', id6: 'png', id7: 'png',
  id8: 'svg', id9: 'svg', id10: 'svg', id11: 'svg', id12: 'svg', id13: 'png',
  id14: 'svg', id15: 'svg', id16: 'svg', id17: 'svg', id18: 'svg',
}
const PROFILE_ICON_IDS = Object.keys(PROFILE_ICON_EXTS)

function profileIconSrc(id: string) {
  const ext = PROFILE_ICON_EXTS[id] ?? 'svg'
  return `/static/profileIcons/${id}.${ext}`
}

function IconPicker({ current, onSelect, onClose }: { current: string; onSelect: (id: string) => void; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [onClose])

  return (
    <div ref={ref} className={styles.iconPicker}>
      {PROFILE_ICON_IDS.map(id => (
        <button
          key={id}
          className={`${styles.iconPickerBtn}${id === current ? ' ' + styles.iconPickerBtnActive : ''}`}
          onClick={() => onSelect(id)}
        >
          <img src={profileIconSrc(id)} alt={id} />
        </button>
      ))}
    </div>
  )
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
  const [linkedToken, setLinkedToken] = useState<string | null>(null)
  const [tokenCopied, setTokenCopied] = useState(false)
  // Profiles per device
  const [openProfilesFor, setOpenProfilesFor] = useState<number | null>(null)
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [profilesLimit, setProfilesLimit] = useState<number>(0)
  const [newProfileName, setNewProfileName] = useState('')
  const [newProfileId, setNewProfileId] = useState('')
  const [profileError, setProfileError] = useState('')
  const [iconPickerFor, setIconPickerFor] = useState<string | null>(null)
  // Telegram
  const [tgStatus, setTgStatus] = useState<TelegramStatus | null>(null)
  const [tgCode, setTgCode] = useState<{ code: string; link: string; ttl_min: number } | null>(null)
  const [tgLoading, setTgLoading] = useState(false)
  // Notifications
  const [notifSettings, setNotifSettings] = useState<NotificationSettings | null>(null)
  const [notifSaving, setNotifSaving] = useState(false)
  const [notifMsg, setNotifMsg] = useState('')
  // 2FA disable
  const [disable2faPw, setDisable2faPw] = useState('')
  const [disable2faCode, setDisable2faCode] = useState('')
  const [disable2faLoading, setDisable2faLoading] = useState(false)
  const [disable2faMsg, setDisable2faMsg] = useState('')
  // Change password
  const [pwCurrent, setPwCurrent] = useState('')
  const [pwNew, setPwNew] = useState('')
  const [pwNew2, setPwNew2] = useState('')
  const [pwTotp, setPwTotp] = useState('')
  const [pwLoading, setPwLoading] = useState(false)
  const [pwMsg, setPwMsg] = useState('')
  // Delete account
  const [delPw, setDelPw] = useState('')
  const [delTotp, setDelTotp] = useState('')
  const [delLoading, setDelLoading] = useState(false)
  // Backup
  const [backupMsg, setBackupMsg] = useState('')
  const [backupError, setBackupError] = useState('')
  // MyShows sync
  const [syncDeviceId, setSyncDeviceId] = useState<number | '' | 'new'>('')
  const [syncNewDeviceName, setSyncNewDeviceName] = useState('')
  const [syncProfileId, setSyncProfileId] = useState('')
  const [syncNewProfileName, setSyncNewProfileName] = useState('')
  const [syncDeviceProfiles, setSyncDeviceProfiles] = useState<Profile[]>([])
  const [syncLogin, setSyncLogin] = useState('')
  const [syncPassword, setSyncPassword] = useState('')
  const [syncLoading, setSyncLoading] = useState(false)
  const [syncDone, setSyncDone] = useState(false)
  const [syncLog, setSyncLog] = useState<SyncLogEntry[]>([])
  const syncLogRef = useRef<HTMLDivElement>(null)
  // LampaC import
  const [importDeviceId, setImportDeviceId] = useState<number | '' | 'new'>('')
  const [importNewDeviceName, setImportNewDeviceName] = useState('')
  const [importProfileId, setImportProfileId] = useState('')
  const [importNewProfileName, setImportNewProfileName] = useState('')
  const [importDeviceProfiles, setImportDeviceProfiles] = useState<Profile[]>([])
  const [importJson, setImportJson] = useState('')
  const [importLoading, setImportLoading] = useState(false)
  const [importMsg, setImportMsg] = useState('')
  const [importError, setImportError] = useState('')
  // Lampa import (file_view format)
  const [fileDeviceId, setFileDeviceId] = useState<number | '' | 'new'>('')
  const [fileNewDeviceName, setFileNewDeviceName] = useState('')
  const [fileProfileId, setFileProfileId] = useState('')
  const [fileNewProfileName, setFileNewProfileName] = useState('')
  const [fileDeviceProfiles, setFileDeviceProfiles] = useState<Profile[]>([])
  const [fileJson, setFileJson] = useState('')
  const [fileLoading, setFileLoading] = useState(false)
  const [fileMsg, setFileMsg] = useState('')
  const [fileError, setFileError] = useState('')

  const fetchDevices = useCallback(async () => {
    const res = await fetch('/api/devices')
    if (!res.ok) return
    const data: Device[] = await res.json()
    data.sort((a, b) => a.id - b.id)
    setDevices(data)
    if (data.length === 0) {
      setSyncDeviceId(v => v === '' ? 'new' : v)
      setImportDeviceId(v => v === '' ? 'new' : v)
      setFileDeviceId(v => v === '' ? 'new' : v)
      return
    }
    const firstId = data[0].id
    const profileRes = await fetch(`/api/devices/${firstId}/profiles`)
    const profileData = profileRes.ok ? await profileRes.json() : {}
    const firstProfiles: Profile[] = (profileData.profiles || []).filter((p: Profile) => p.profile_id !== '')
    setSyncDeviceId(id => (id === '' || id === 'new') ? firstId : id)
    setImportDeviceId(id => (id === '' || id === 'new') ? firstId : id)
    setFileDeviceId(id => (id === '' || id === 'new') ? firstId : id)
    setSyncDeviceProfiles(p => p.length === 0 ? firstProfiles : p)
    setImportDeviceProfiles(p => p.length === 0 ? firstProfiles : p)
    setFileDeviceProfiles(p => p.length === 0 ? firstProfiles : p)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const fetchTgStatus = useCallback(async () => {
    const res = await fetch('/api/telegram/status')
    if (res.ok) setTgStatus(await res.json())
  }, [])

  const fetchNotifSettings = useCallback(async () => {
    const res = await fetch('/api/notification-settings')
    if (res.ok) setNotifSettings(await res.json())
  }, [])

  useEffect(() => {
    fetchDevices()
    fetchTgStatus()
    fetchNotifSettings()
  }, [fetchDevices, fetchTgStatus, fetchNotifSettings])

  useEffect(() => {
    if (syncDeviceProfiles.length > 0 && (syncProfileId === '' || syncProfileId === 'new'))
      setSyncProfileId(syncDeviceProfiles[0].profile_id)
  }, [syncDeviceProfiles]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (importDeviceProfiles.length > 0 && (importProfileId === '' || importProfileId === 'new'))
      setImportProfileId(importDeviceProfiles[0].profile_id)
  }, [importDeviceProfiles]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (fileDeviceProfiles.length > 0 && (fileProfileId === '' || fileProfileId === 'new'))
      setFileProfileId(fileDeviceProfiles[0].profile_id)
  }, [fileDeviceProfiles]) // eslint-disable-line react-hooks/exhaustive-deps


  async function fetchProfilesForDevice(deviceId: number): Promise<Profile[]> {
    const res = await fetch(`/api/devices/${deviceId}/profiles`)
    if (!res.ok) return []
    const data = await res.json()
    return (data.profiles || []).filter((p: Profile) => p.profile_id !== '')
  }

  async function handleSyncDeviceChange(id: number) {
    setSyncDeviceId(id)
    const p = await fetchProfilesForDevice(id)
    setSyncDeviceProfiles(p)
    setSyncProfileId(p.length > 0 ? p[0].profile_id : '')
  }

  async function handleImportDeviceChange(id: number) {
    setImportDeviceId(id)
    const p = await fetchProfilesForDevice(id)
    setImportDeviceProfiles(p)
    setImportProfileId(p.length > 0 ? p[0].profile_id : '')
  }

  async function ensureDevice(deviceId: number | '' | 'new', newName: string): Promise<{ id: number; token: string } | null> {
    if (deviceId === 'new') {
      const res = await fetch('/api/devices', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName.trim() || 'Устройство' }),
      })
      if (!res.ok) return null
      const d = await res.json()
      fetchDevices()
      return { id: d.id, token: d.token }
    }
    if (!deviceId) return null
    const device = devices.find(dev => dev.id === deviceId)
    return device ? { id: device.id, token: device.token } : null
  }

  async function ensureProfile(deviceId: number, profileId: string, newName: string): Promise<string> {
    if (profileId !== 'new') return profileId
    const res = await fetch(`/api/devices/${deviceId}/profiles`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: newName.trim() || 'Профиль' }),
    })
    if (!res.ok) return ''
    const d = await res.json()
    return d.profile_id ?? ''
  }

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
    const body: Record<string, string> = { code: linkCode }
    if (linkDeviceId === 'new') {
      body.name = linkNewName || 'Устройство'
    } else {
      body.name = devices.find(d => d.id === linkDeviceId)?.name || 'Устройство'
    }
    const res = await fetch('/api/device/link', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    setLinkLoading(false)
    if (res.ok) {
      const data = await res.json().catch(() => ({}))
      setLinkCode('')
      setLinkSuccess('Устройство успешно привязано!')
      if (data.token) setLinkedToken(data.token)
      fetchDevices()
    } else {
      const d = await res.json().catch(() => ({}))
      setLinkError(d.error || 'Ошибка привязки')
    }
  }

  function copyLinkedToken() {
    if (!linkedToken) return
    navigator.clipboard.writeText(linkedToken).then(() => {
      setTokenCopied(true)
      setTimeout(() => setTokenCopied(false), 2000)
    }).catch(() => {})
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
    if (!confirm(`Старый токен устройства «${name}» перестанет работать. Продолжить?`)) return
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
    if (syncDeviceId === id) { setSyncDeviceId(''); setSyncDeviceProfiles([]); setSyncProfileId('') }
    if (importDeviceId === id) { setImportDeviceId(''); setImportDeviceProfiles([]); setImportProfileId('') }
    if (fileDeviceId === id) { setFileDeviceId(''); setFileDeviceProfiles([]); setFileProfileId('') }
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
    if (!r.ok) return
    const d = await r.json()
    const updated: Profile[] = d.profiles || []
    setProfiles(updated)
    const filtered = updated.filter(p => p.profile_id !== '')
    if (syncDeviceId === openProfilesFor) setSyncDeviceProfiles(filtered)
    if (importDeviceId === openProfilesFor) setImportDeviceProfiles(filtered)
    if (fileDeviceId === openProfilesFor) setFileDeviceProfiles(filtered)
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

  async function handleToggleChild(p: Profile) {
    await fetch(`/api/devices/${openProfilesFor}/profiles/${p.profile_id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ child: !p.child }),
    })
    reloadProfiles()
  }

  async function handleEditParams(p: Profile) {
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
    const url = profileId === ''
      ? `/api/devices/${openProfilesFor}/default-timecodes`
      : `/api/devices/${openProfilesFor}/profiles/${profileId}/timecodes`
    await fetch(url, { method: 'DELETE' })
    reloadProfiles()
    fetchDevices()
  }

  async function handleDeleteProfile(profileId: string, name: string) {
    if (!confirm(`Удалить профиль «${name}» и все его таймкоды?`)) return
    const url = profileId === ''
      ? `/api/devices/${openProfilesFor}/default-timecodes`
      : `/api/devices/${openProfilesFor}/profiles/${profileId}`
    await fetch(url, { method: 'DELETE' })
    if (syncProfileId === profileId) setSyncProfileId('')
    if (importProfileId === profileId) setImportProfileId('')
    if (fileProfileId === profileId) setFileProfileId('')
    if (openProfilesFor !== null) {
      fetchProfilesForDevice(openProfilesFor).then(refreshed => {
        if (syncDeviceId === openProfilesFor) setSyncDeviceProfiles(refreshed)
        if (importDeviceId === openProfilesFor) setImportDeviceProfiles(refreshed)
        if (fileDeviceId === openProfilesFor) setFileDeviceProfiles(refreshed)
      })
    }
    reloadProfiles()
    fetchDevices()
  }

  async function handleSetIcon(profileId: string, icon: string) {
    setIconPickerFor(null)
    await fetch(`/api/devices/${openProfilesFor}/profiles/${profileId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ icon }),
    })
    reloadProfiles()
  }

  async function handleGenerateTgCode() {
    setTgLoading(true)
    setTgCode(null)
    const res = await fetch('/api/telegram/generate-link-code', { method: 'POST' })
    setTgLoading(false)
    if (res.ok) setTgCode(await res.json())
    else { const d = await res.json().catch(() => ({})); alert(d.error || 'Ошибка') }
  }

  async function handleTgUnlink() {
    if (!confirm('Отвязать Telegram от аккаунта?')) return
    await fetch('/api/telegram/unlink', { method: 'DELETE' })
    setTgStatus(null)
    setTgCode(null)
    fetchTgStatus()
  }

  async function handleSaveNotif(e: React.FormEvent) {
    e.preventDefault()
    if (!notifSettings) return
    setNotifSaving(true)
    setNotifMsg('')
    const res = await fetch('/api/notification-settings', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(notifSettings),
    })
    setNotifSaving(false)
    setNotifMsg(res.ok ? 'Сохранено' : 'Ошибка сохранения')
  }

  async function handleDisable2FA(e: React.FormEvent) {
    e.preventDefault()
    setDisable2faMsg('')
    setDisable2faLoading(true)
    const res = await fetch('/api/disable-2fa', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: disable2faPw, totp_code: disable2faCode }),
    })
    setDisable2faLoading(false)
    if (res.ok) {
      setDisable2faPw(''); setDisable2faCode('')
      setDisable2faMsg('2FA отключена')
      window.location.reload()
    } else {
      const d = await res.json().catch(() => ({}))
      setDisable2faMsg(d.error || 'Ошибка отключения 2FA')
    }
  }

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault()
    setPwMsg('')
    if (pwNew !== pwNew2) {
      setPwMsg('Пароли не совпадают')
      return
    }
    setPwLoading(true)
    const body: Record<string, string> = { current_password: pwCurrent, new_password: pwNew }
    if (user?.totp_enabled && pwTotp) body.totp_code = pwTotp
    const res = await fetch('/api/change-password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    setPwLoading(false)
    if (res.ok) {
      setPwCurrent(''); setPwNew(''); setPwNew2(''); setPwTotp('')
      setPwMsg('Пароль изменён')
    } else {
      const d = await res.json().catch(() => ({}))
      setPwMsg(d.error || 'Ошибка смены пароля')
    }
  }

  async function handleExport() {
    setBackupMsg('')
    setBackupError('')
    try {
      const res = await fetch('/api/export')
      if (!res.ok) throw new Error('HTTP ' + res.status)
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `movies-backup-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
      URL.revokeObjectURL(url)
      setBackupMsg('Экспорт завершён')
    } catch {
      setBackupError('Ошибка экспорта')
    }
  }

  async function handleImportFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    e.target.value = ''
    if (!confirm('Импорт полностью заменит все устройства, профили и таймкоды. Продолжить?')) return
    setBackupMsg('')
    setBackupError('')
    try {
      const text = await file.text()
      const res = await fetch('/api/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: text,
      })
      if (!res.ok) throw new Error('HTTP ' + res.status)
      setBackupMsg('Импорт завершён. Страница обновится.')
      setTimeout(() => window.location.reload(), 1500)
    } catch {
      setBackupError('Ошибка импорта')
    }
  }

  async function handleDeleteAccount(e: React.FormEvent) {
    e.preventDefault()
    if (!confirm('Удалить аккаунт и все данные? Это необратимо!')) return
    setDelLoading(true)
    const body: Record<string, string> = { password: delPw }
    if (user?.totp_enabled && delTotp) body.totp_code = delTotp
    const res = await fetch('/api/account', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    setDelLoading(false)
    if (res.ok) {
      window.location.href = '/login'
    } else {
      const d = await res.json().catch(() => ({}))
      alert(d.error || 'Ошибка удаления аккаунта')
    }
  }

  async function handleMyShowsSync(e: React.FormEvent) {
    e.preventDefault()
    if (!syncDeviceId) return
    setSyncLoading(true)
    setSyncDone(false)
    setSyncLog([])
    const dev = await ensureDevice(syncDeviceId, syncNewDeviceName)
    if (!dev) { setSyncLoading(false); return }
    const profileId = await ensureProfile(dev.id, syncProfileId, syncNewProfileName)

    const form = new FormData()
    form.append('device_id', String(dev.id))
    form.append('profile_id', profileId)
    form.append('login', syncLogin)
    form.append('password', syncPassword)

    try {
      const res = await fetch('/myshows/sync', { method: 'POST', body: form })
      if (!res.ok || !res.body) {
        const d = await res.json().catch(() => ({}))
        setSyncLog([{ type: 'error', message: d.error || 'Ошибка запроса' }])
        setSyncLoading(false)
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() ?? ''
        for (const line of lines) {
          if (!line.startsWith('data:')) continue
          const json = line.slice(5).trim()
          if (!json) continue
          try {
            const entry: SyncLogEntry = JSON.parse(json)
            setSyncLog(prev => [...prev, entry])
            setTimeout(() => {
              if (syncLogRef.current) {
                syncLogRef.current.scrollTop = syncLogRef.current.scrollHeight
              }
            }, 0)
          } catch { /* skip malformed */ }
        }
      }
      setSyncDone(true)
      fetchDevices()
      reloadProfiles()
      setTimeout(() => { setSyncDone(false); setSyncLog([]) }, 4000)
    } catch (err) {
      const msg = String(err).includes('Load failed') || String(err).includes('Failed to fetch')
        ? 'Соединение прервано'
        : String(err)
      setSyncLog(prev => [...prev, { type: 'error', message: msg }])
    }
    setSyncLoading(false)
  }

  async function handleLampacImport(e: React.FormEvent) {
    e.preventDefault()
    setImportError('')
    setImportMsg('')
    if (!importDeviceId) return

    let parsed: unknown
    try {
      parsed = JSON.parse(importJson)
    } catch {
      setImportError('Неверный JSON')
      return
    }

    setImportLoading(true)
    const dev = await ensureDevice(importDeviceId, importNewDeviceName)
    if (!dev) { setImportError('Ошибка создания устройства'); setImportLoading(false); return }
    const profileId = await ensureProfile(dev.id, importProfileId, importNewProfileName)
    const params = new URLSearchParams({ token: dev.token })
    if (profileId) params.set('profile_id', profileId)

    const res = await fetch(`/timecode/import/lampac?${params}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(parsed),
    })
    setImportLoading(false)
    if (res.ok) {
      const d = await res.json().catch(() => ({}))
      setImportMsg(`Импортировано: ${d.imported ?? 0}`)
      setImportJson('')
      fetchDevices()
      reloadProfiles()
    } else {
      const d = await res.json().catch(() => ({}))
      setImportError(d.error || 'Ошибка импорта')
    }
  }

  async function handleFileImport(e: React.FormEvent) {
    e.preventDefault()
    setFileError('')
    setFileMsg('')
    if (!fileDeviceId) return

    let raw: Record<string, Record<string, unknown>>
    try {
      raw = JSON.parse(fileJson)
    } catch {
      setFileError('Неверный JSON')
      return
    }

    // Convert Lampa file_view format: values may be numbers/objects — stringify them
    const converted: Record<string, Record<string, string>> = {}
    for (const [cardId, items] of Object.entries(raw)) {
      if (typeof items !== 'object' || items === null) continue
      converted[cardId] = {}
      for (const [key, value] of Object.entries(items)) {
        converted[cardId][key] = typeof value === 'string' ? value : JSON.stringify(value)
      }
    }

    setFileLoading(true)
    const dev = await ensureDevice(fileDeviceId, fileNewDeviceName)
    if (!dev) { setFileError('Ошибка создания устройства'); setFileLoading(false); return }
    const profileId = await ensureProfile(dev.id, fileProfileId, fileNewProfileName)
    const params = new URLSearchParams({ token: dev.token })
    if (profileId) params.set('profile_id', profileId)

    const res = await fetch(`/timecode/import/lampac?${params}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(converted),
    })
    setFileLoading(false)
    if (res.ok) {
      const d = await res.json().catch(() => ({}))
      setFileMsg(`Импортировано: ${d.imported ?? 0}`)
      setFileJson('')
      fetchDevices()
      reloadProfiles()
    } else {
      const d = await res.json().catch(() => ({}))
      setFileError(d.error || 'Ошибка импорта')
    }
  }

  function formatSyncEntry(entry: SyncLogEntry): string {
    if (entry.type === 'stage') {
      const label = entry.stage === 'movies' ? 'Фильмы' : 'Сериалы'
      const name = entry.name ? ` — ${entry.name}` : ''
      return `${label}: ${entry.current}/${entry.total}${name}`
    }
    return entry.message ?? ''
  }

  const stageEntries = syncLog.filter(e => e.type === 'stage')
  const lastStage = stageEntries[stageEntries.length - 1]
  const statusEntries = syncLog.filter(e => e.type === 'status')
  const lastStatus = statusEntries[statusEntries.length - 1]
  const errors = syncLog.filter(e => e.type === 'error')

  const isPremium = user?.role === 'premium' || user?.role === 'super'
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
                <div key={d.id} id={`device-row-${d.id}`} className={styles.deviceRow}>
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
                      <h4 className={styles.profilesTitle}>Профили</h4>
                      {profiles.length === 0 && <p className={styles.empty}>Нет профилей</p>}
                      {profiles.map(p => (
                        <div key={p.profile_id || '__default__'} className={styles.profileCard}>
                          <div className={styles.profileCardTop}>
                            <div className={styles.profileCardLeft}>
                              <div className={styles.profileNameRow}>
                                <div className={styles.profileIconWrap}>
                                  <button
                                    className={styles.profileIconBtn}
                                    title="Изменить иконку"
                                    onClick={() => setIconPickerFor(iconPickerFor === p.profile_id ? null : p.profile_id)}
                                  >
                                    {p.icon
                                      ? <img src={profileIconSrc(p.icon)} alt="" />
                                      : <img src={profileIconSrc('id1')} alt="" />}
                                  </button>
                                  {iconPickerFor === p.profile_id && (
                                    <IconPicker
                                      current={p.icon || 'id1'}
                                      onSelect={icon => handleSetIcon(p.profile_id, icon)}
                                      onClose={() => setIconPickerFor(null)}
                                    />
                                  )}
                                </div>
                                <strong className={styles.profileName}>{p.profile_id === '' ? 'Основной' : p.name}</strong>
                              </div>
                              <div className={styles.profileMeta}>
                                {p.profile_id !== '' && <code className={styles.profileId}>ID: {p.profile_id}</code>}
                                <span>{p.profile_id !== '' ? '· ' : ''}таймкодов: {p.timecodes_count}</span>
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
                        <form className={`${styles.formCol} ${styles.newProfileForm}`} onSubmit={handleCreateProfile}>
                          <input
                            className={styles.input}
                            placeholder="Название профиля"
                            value={newProfileName}
                            onChange={e => setNewProfileName(e.target.value)}
                            required
                          />
                          <input
                            className={styles.input}
                            placeholder="ID профиля (авто если пусто)"
                            value={newProfileId}
                            onChange={e => setNewProfileId(e.target.value.replace(/[^a-zA-Z0-9_-]/g, '').slice(0, 32))}
                            maxLength={32}
                          />
                          <button className={styles.btnPrimary} type="submit">Добавить профиль</button>
                        </form>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {(maxDevices === null || devices.length < maxDevices) ? (
            <form id="create-device-form" className={styles.formCol} onSubmit={handleCreate}>
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

        {/* ── 2-column grid of details sections ── */}
        <div className={styles.detailsGrid}>

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

          {/* ── MyShows sync ── */}
          <details className={styles.details}>
            <summary className={styles.summary}>Синхронизация MyShows</summary>
            <div className={styles.detailsBody}>
              {!isPremium ? (
                <div className={styles.premiumGate}>
                  <p className={styles.hint}>Синхронизация с MyShows доступна для подписчиков Premium.</p>
                  <span className={styles.premiumBadge}>Premium</span>
                </div>
              ) : (
                <form className={styles.formCol} onSubmit={handleMyShowsSync}>
                  <div className={styles.formGrid}>
                    <label className={styles.fieldLabel}>
                      Устройство
                      <select
                        className={styles.select}
                        value={syncDeviceId}
                        onChange={e => {
                          const v = e.target.value
                          if (v === 'new') { setSyncDeviceId('new'); setSyncDeviceProfiles([]); setSyncProfileId('') }
                          else handleSyncDeviceChange(Number(v))
                        }}
                        required
                      >
                        {syncDeviceId === '' && devices.length > 0 && <option value="">— выберите —</option>}
                        {devices.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                        {(maxDevices === null || devices.length < (maxDevices ?? 99)) && <option value="new">＋ Новое устройство</option>}
                      </select>
                    </label>
                    <label className={styles.fieldLabel}>
                      Профиль
                      <select
                        className={styles.select}
                        value={syncProfileId}
                        onChange={e => setSyncProfileId(e.target.value)}
                        disabled={syncDeviceId === ''}
                      >
                        {syncDeviceId !== '' && syncDeviceProfiles.length === 0 && <option value="">Основной</option>}
                        {syncDeviceProfiles.map(p => (
                          <option key={p.profile_id} value={p.profile_id}>{p.name}</option>
                        ))}
                        {syncDeviceId !== '' && <option value="new">＋ Новый профиль</option>}
                      </select>
                    </label>
                  </div>
                  {syncDeviceId === 'new' && (
                    <input className={styles.input} placeholder="Название устройства" value={syncNewDeviceName} onChange={e => setSyncNewDeviceName(e.target.value)} maxLength={100} required />
                  )}
                  {syncDeviceId !== '' && syncProfileId === 'new' && (
                    <input className={styles.input} placeholder="Название профиля" value={syncNewProfileName} onChange={e => setSyncNewProfileName(e.target.value)} maxLength={100} />
                  )}
                  <div className={styles.formRow}>
                    <input
                      className={styles.input}
                      placeholder="Логин MyShows"
                      value={syncLogin}
                      onChange={e => setSyncLogin(e.target.value)}
                      autoComplete="username"
                      required
                    />
                    <PasswordInput
                      className={styles.input}
                      placeholder="Пароль MyShows"
                      value={syncPassword}
                      onChange={e => setSyncPassword(e.target.value)}
                      autoComplete="current-password"
                      required
                    />
                  </div>
                  <button className={styles.btnPrimary} type="submit" disabled={syncLoading || !syncDeviceId}>
                    {syncLoading ? 'Синхронизация…' : 'Синхронизировать'}
                  </button>
                  {(syncLog.length > 0 || syncDone) && (
                    <div className={styles.syncLog} ref={syncLogRef}>
                      {lastStage && (
                        <div className={styles.syncLogLine}>{formatSyncEntry(lastStage)}</div>
                      )}
                      {!lastStage && lastStatus && (
                        <div className={styles.syncLogLine}>{formatSyncEntry(lastStatus)}</div>
                      )}
                      {errors.map((entry, i) => (
                        <div key={i} className={styles.syncLogError}>{formatSyncEntry(entry)}</div>
                      ))}
                      {syncDone && errors.length === 0 && (
                        <div className={styles.syncLogDone}>Синхронизация завершена</div>
                      )}
                    </div>
                  )}
                </form>
              )}
            </div>
          </details>

          {/* ── Telegram ── */}
          <details className={styles.details}>
            <summary className={styles.summary}>Telegram</summary>
            <div className={styles.detailsBody}>
              {tgStatus?.linked ? (
                <div>
                  <p className={styles.hint}>
                    Аккаунт привязан{tgStatus.username ? ` к @${tgStatus.username}` : ''}.
                  </p>
                  <button className={`${styles.btnSm} ${styles.danger}`} onClick={handleTgUnlink} style={{ marginTop: 8 }}>
                    Отвязать Telegram
                  </button>
                </div>
              ) : (
                <div>
                  <p className={styles.hint}>Telegram не привязан. Привяжите, чтобы получать уведомления и сбрасывать пароль через бота.</p>
                  {tgCode ? (
                    <div style={{ marginTop: 8 }}>
                      <p>Отправьте боту команду или перейдите по ссылке:</p>
                      <a href={tgCode.link} target="_blank" rel="noreferrer" className={styles.btnPrimary} style={{ display: 'inline-block', marginTop: 6 }}>
                        Открыть @{tgCode.link.split('t.me/')[1]?.split('?')[0]}
                      </a>
                      <p className={styles.hint} style={{ marginTop: 6 }}>
                        Код действителен {tgCode.ttl_min} минут
                      </p>
                    </div>
                  ) : (
                    <button className={styles.btnPrimary} onClick={handleGenerateTgCode} disabled={tgLoading} style={{ marginTop: 8 }}>
                      {tgLoading ? 'Генерация…' : 'Привязать Telegram'}
                    </button>
                  )}
                </div>
              )}
            </div>
          </details>

          {/* ── LampaC import ── */}
          <details className={styles.details}>
            <summary className={styles.summary}>Импорт таймкодов из LampaC</summary>
            <div className={styles.detailsBody}>
              <p className={styles.hint}>Вставьте JSON-экспорт таймкодов из LampaC.</p>
              {importError && <p className={styles.errorText}>{importError}</p>}
              {importMsg && <p className={styles.successText}>{importMsg}</p>}
              <form className={styles.formCol} onSubmit={handleLampacImport}>
                <div className={styles.formGrid}>
                  <label className={styles.fieldLabel}>
                    Устройство
                    <select
                      className={styles.select}
                      value={importDeviceId}
                      onChange={e => {
                        const v = e.target.value
                        if (v === 'new') { setImportDeviceId('new'); setImportDeviceProfiles([]); setImportProfileId('') }
                        else handleImportDeviceChange(Number(v))
                      }}
                      required
                    >
                      {importDeviceId === '' && devices.length > 0 && <option value="">— выберите —</option>}
                      {devices.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                      {(maxDevices === null || devices.length < (maxDevices ?? 99)) && <option value="new">＋ Новое устройство</option>}
                    </select>
                  </label>
                  <label className={styles.fieldLabel}>
                    Профиль
                    <select
                      className={styles.select}
                      value={importProfileId}
                      onChange={e => setImportProfileId(e.target.value)}
                      disabled={importDeviceId === ''}
                    >
                      {importDeviceId !== '' && importDeviceProfiles.length === 0 && <option value="">Основной</option>}
                      {importDeviceProfiles.map(p => (
                        <option key={p.profile_id} value={p.profile_id}>{p.name}</option>
                      ))}
                      {importDeviceId !== '' && <option value="new">＋ Новый профиль</option>}
                    </select>
                  </label>
                </div>
                {importDeviceId === 'new' && (
                  <input className={styles.input} placeholder="Название устройства" value={importNewDeviceName} onChange={e => setImportNewDeviceName(e.target.value)} maxLength={100} required />
                )}
                {importDeviceId !== '' && importProfileId === 'new' && (
                  <input className={styles.input} placeholder="Название профиля" value={importNewProfileName} onChange={e => setImportNewProfileName(e.target.value)} maxLength={100} />
                )}
                <textarea
                  className={styles.jsonTextarea}
                  placeholder={'{"card_id":{"item":"data"}}'}
                  value={importJson}
                  onChange={e => { setImportJson(e.target.value); setImportError('') }}
                  rows={5}
                  required
                />
                <button className={styles.btnPrimary} type="submit" disabled={importLoading || !importDeviceId}>
                  {importLoading ? 'Импорт…' : 'Импортировать'}
                </button>
              </form>
            </div>
          </details>

          {/* ── Lampa import ── */}
          <details className={styles.details}>
            <summary className={styles.summary}>Импорт таймкодов из Lampa</summary>
            <div className={styles.detailsBody}>
              <p className={styles.hint}>
                В консоли браузера на странице Lampa выполните:{' '}
                <code
                  className={styles.codeSnippet}
                  title="Нажмите, чтобы скопировать"
                  onClick={() => navigator.clipboard.writeText("copy(localStorage.getItem('file_view'))").catch(() => {})}
                >
                  copy(localStorage.getItem('file_view'))
                </code>
                {' '}— затем вставьте JSON ниже.
              </p>
              {fileError && <p className={styles.errorText}>{fileError}</p>}
              {fileMsg && <p className={styles.successText}>{fileMsg}</p>}
              <form className={styles.formCol} onSubmit={handleFileImport}>
                <div className={styles.formGrid}>
                  <label className={styles.fieldLabel}>
                    Устройство
                    <select
                      className={styles.select}
                      value={fileDeviceId}
                      onChange={e => {
                        const v = e.target.value
                        if (v === 'new') { setFileDeviceId('new'); setFileDeviceProfiles([]); setFileProfileId('') }
                        else { const id = Number(v); setFileDeviceId(id); fetchProfilesForDevice(id).then(p => { setFileDeviceProfiles(p); setFileProfileId(p.length > 0 ? p[0].profile_id : '') }) }
                      }}
                      required
                    >
                      {fileDeviceId === '' && devices.length > 0 && <option value="">— выберите —</option>}
                      {devices.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                      {(maxDevices === null || devices.length < (maxDevices ?? 99)) && <option value="new">＋ Новое устройство</option>}
                    </select>
                  </label>
                  <label className={styles.fieldLabel}>
                    Профиль
                    <select
                      className={styles.select}
                      value={fileProfileId}
                      onChange={e => setFileProfileId(e.target.value)}
                      disabled={fileDeviceId === ''}
                    >
                      {fileDeviceId !== '' && fileDeviceProfiles.length === 0 && <option value="">Основной</option>}
                      {fileDeviceProfiles.map(p => (
                        <option key={p.profile_id} value={p.profile_id}>{p.name}</option>
                      ))}
                      {fileDeviceId !== '' && <option value="new">＋ Новый профиль</option>}
                    </select>
                  </label>
                </div>
                {fileDeviceId === 'new' && (
                  <input className={styles.input} placeholder="Название устройства" value={fileNewDeviceName} onChange={e => setFileNewDeviceName(e.target.value)} maxLength={100} required />
                )}
                {fileDeviceId !== '' && fileProfileId === 'new' && (
                  <input className={styles.input} placeholder="Название профиля" value={fileNewProfileName} onChange={e => setFileNewProfileName(e.target.value)} maxLength={100} />
                )}
                <textarea
                  className={styles.jsonTextarea}
                  placeholder={'{"571234":{"percent":95,"time":3600}}'}
                  value={fileJson}
                  onChange={e => { setFileJson(e.target.value); setFileError('') }}
                  rows={5}
                  required
                />
                <button className={styles.btnPrimary} type="submit" disabled={fileLoading || !fileDeviceId}>
                  {fileLoading ? 'Импорт…' : 'Импортировать'}
                </button>
              </form>
            </div>
          </details>

          {/* ── Notifications (visible only when TG linked) ── */}
          <details className={styles.details} style={{ visibility: tgStatus?.linked && notifSettings ? 'visible' : 'hidden' }}>
            <summary className={styles.summary}>Уведомления</summary>
            <div className={styles.detailsBody}>
              <p className={styles.hint}>Уведомления об истечении подписки и неактивности отправляются в Telegram.</p>
              {notifMsg && <p className={notifMsg === 'Сохранено' ? styles.successText : styles.errorText}>{notifMsg}</p>}
              {notifSettings && (
                <form className={styles.formCol} onSubmit={handleSaveNotif}>
                  <label className={styles.checkLabel}>
                    <input
                      type="checkbox"
                      checked={notifSettings.enabled}
                      onChange={e => setNotifSettings(s => s ? { ...s, enabled: e.target.checked } : s)}
                    />
                    Включить уведомления
                  </label>
                  <label className={styles.fieldLabel}>
                    Часовой пояс
                    <select
                      className={styles.select}
                      value={notifSettings.timezone}
                      onChange={e => setNotifSettings(s => s ? { ...s, timezone: e.target.value } : s)}
                    >
                      {['Europe/Moscow', 'Europe/Kaliningrad', 'Asia/Yekaterinburg', 'Asia/Omsk',
                        'Asia/Krasnoyarsk', 'Asia/Irkutsk', 'Asia/Yakutsk', 'Asia/Vladivostok',
                        'Asia/Magadan', 'Asia/Kamchatka', 'Europe/Kiev', 'Europe/Minsk',
                        'Asia/Almaty', 'Asia/Tashkent', 'Europe/London', 'Europe/Berlin'].map(tz => (
                        <option key={tz} value={tz}>{tz}</option>
                      ))}
                    </select>
                  </label>
                  <div className={styles.formRow}>
                    <label style={{ flex: 1 }}>
                      С (час)
                      <input
                        className={styles.input}
                        type="number" min={0} max={23}
                        value={notifSettings.notify_start}
                        onChange={e => setNotifSettings(s => s ? { ...s, notify_start: Number(e.target.value) } : s)}
                      />
                    </label>
                    <label style={{ flex: 1 }}>
                      До (час)
                      <input
                        className={styles.input}
                        type="number" min={0} max={23}
                        value={notifSettings.notify_end}
                        onChange={e => setNotifSettings(s => s ? { ...s, notify_end: Number(e.target.value) } : s)}
                      />
                    </label>
                  </div>
                  <button className={styles.btnPrimary} type="submit" disabled={notifSaving}>
                    {notifSaving ? 'Сохранение…' : 'Сохранить'}
                  </button>
                </form>
              )}
            </div>
          </details>

          {/* ── Account settings ── */}
          <details className={styles.details}>
            <summary className={styles.summary}>Настройки аккаунта</summary>
            <div className={styles.detailsBody}>

              <h4 className={styles.subTitle}>Двухфакторная аутентификация (2FA)</h4>
              {user?.totp_enabled ? (
                <>
                  <p className={styles.hint}>
                    2FA включена. Резервных кодов осталось: <strong>{user.backup_codes_count}</strong>
                  </p>
                  {disable2faMsg && (
                    <p className={disable2faMsg === '2FA отключена' ? styles.successText : styles.errorText}>
                      {disable2faMsg}
                    </p>
                  )}
                  <form className={styles.formCol} onSubmit={handleDisable2FA}>
                    <div className={styles.formRow}>
                      <PasswordInput
                        className={styles.input}
                        placeholder="Текущий пароль"
                        value={disable2faPw}
                        onChange={e => setDisable2faPw(e.target.value)}
                        required
                      />
                      <input
                        className={`${styles.input} ${styles.inputMono}`}
                        type="text"
                        placeholder="Код из приложения"
                        value={disable2faCode}
                        onChange={e => setDisable2faCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                        inputMode="numeric"
                        maxLength={6}
                        required
                      />
                    </div>
                    <button className={`${styles.btnSm} ${styles.warning}`} type="submit" disabled={disable2faLoading}>
                      {disable2faLoading ? 'Отключение…' : 'Отключить 2FA'}
                    </button>
                  </form>
                </>
              ) : (
                <>
                  <p className={styles.hint}>
                    Защитите аккаунт кодом из приложения-аутентификатора (Google Authenticator, Aegis и др.).
                  </p>
                  <a href="/setup-2fa" className={styles.btnPrimary}>Включить 2FA</a>
                </>
              )}

              <hr className={styles.hr} />

              <h4 className={styles.subTitle}>Сменить пароль</h4>
              {pwMsg && <p className={pwMsg === 'Пароль изменён' ? styles.successText : styles.errorText}>{pwMsg}</p>}
              <form className={styles.formCol} onSubmit={handleChangePassword}>
                <PasswordInput className={styles.input} placeholder="Текущий пароль" value={pwCurrent} onChange={e => setPwCurrent(e.target.value)} required />
                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                  <PasswordInput className={styles.input} placeholder="Новый пароль" value={pwNew} onChange={e => setPwNew(e.target.value)} minLength={6} required />
                  {pwNew.length > 0 && pwNew.length < 6 && (
                    <span className={styles.errorText}>минимум 6 символов</span>
                  )}
                </div>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                  <PasswordInput className={styles.input} placeholder="Повторите новый пароль" value={pwNew2} onChange={e => setPwNew2(e.target.value)} required />
                  {pwNew2.length > 0 && (
                    <span className={pwNew === pwNew2 ? styles.successText : styles.errorText}>
                      {pwNew === pwNew2 ? 'Пароли совпадают' : 'Пароли не совпадают'}
                    </span>
                  )}
                </div>
                {user?.totp_enabled && (
                  <input
                    className={`${styles.input} ${styles.inputMono}`}
                    type="text"
                    placeholder="Код 2FA"
                    value={pwTotp}
                    onChange={e => setPwTotp(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    inputMode="numeric"
                    maxLength={6}
                  />
                )}
                <button className={styles.btnPrimary} type="submit" disabled={pwLoading}>{pwLoading ? 'Сохранение…' : 'Сменить пароль'}</button>
              </form>

              <hr className={styles.hr} />

              <h4 className={styles.subTitle} style={{ color: 'var(--danger, #e05252)' }}>Удалить аккаунт</h4>
              <p className={styles.hint}>Все устройства и таймкоды будут удалены безвозвратно.</p>
              <form className={styles.formCol} onSubmit={handleDeleteAccount}>
                <div className={styles.formRow}>
                  <PasswordInput className={styles.input} placeholder="Введите пароль для подтверждения" value={delPw} onChange={e => setDelPw(e.target.value)} required />
                  {user?.totp_enabled && (
                    <input
                      className={`${styles.input} ${styles.inputMono}`}
                      type="text"
                      placeholder="Код 2FA"
                      value={delTotp}
                      onChange={e => setDelTotp(e.target.value.replace(/\D/g, '').slice(0, 6))}
                      inputMode="numeric"
                      maxLength={6}
                    />
                  )}
                </div>
                <button className={`${styles.btnPrimary} ${styles.danger}`} type="submit" disabled={delLoading}>{delLoading ? 'Удаление…' : 'Удалить аккаунт'}</button>
              </form>
            </div>
          </details>

          {/* ── Backup ── */}
          <details className={styles.details}>
            <summary className={styles.summary}>Резервная копия</summary>
            <div className={styles.detailsBody}>
              <p className={styles.hint}>Экспортирует все устройства, профили, таймкоды и настройки плагинов. Импорт полностью заменяет текущие данные.</p>
              <div className={styles.backupActions}>
                <button className={styles.btnPrimary} onClick={handleExport}>Экспортировать</button>
                <label className={styles.btnSm} style={{ cursor: 'pointer' }}>
                  Импортировать
                  <input type="file" accept=".json" style={{ display: 'none' }} onChange={handleImportFile} />
                </label>
              </div>
              {backupMsg && <p className={styles.successText}>{backupMsg}</p>}
              {backupError && <p className={styles.errorText}>{backupError}</p>}
            </div>
          </details>

        </div>

      </div>

      {linkedToken && (
        <div className={styles.modalOverlay} onClick={() => setLinkedToken(null)}>
          <div className={styles.modal} onClick={e => e.stopPropagation()}>
            <h3 className={styles.modalTitle}>Устройство привязано</h3>
            <p className={styles.modalHint}>Сохраните токен — он нужен для авторизации в плагине. В любой момент его можно посмотреть в разделе устройств.</p>
            <div className={styles.modalToken}>{linkedToken}</div>
            <div className={styles.modalActions}>
              <button className={styles.btnPrimary} onClick={copyLinkedToken}>
                {tokenCopied ? '✓ Скопировано' : 'Копировать токен'}
              </button>
              <button className={`${styles.btnSm}`} onClick={() => setLinkedToken(null)}>Закрыть</button>
            </div>
          </div>
        </div>
      )}
    </Layout>
  )
}
