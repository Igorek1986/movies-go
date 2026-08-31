import { useEffect, useState, useCallback, useRef } from 'react'
import { useAuth } from '@/hooks/useAuth'
import { useActiveProfile, profileUrlParam } from '@/contexts/ActiveProfileContext'

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
  child_birth_year?: number | null
  params: Record<string, unknown>
  timecodes_count: number
}

interface DevicePluginItem {
  id: number
  url: string
  name: string
  enabled: boolean
  created_at: string
}

interface ProfilePluginItem {
  url: string
  name: string
  in_device_list: boolean
  device_enabled: boolean
  override: boolean | null
}

interface SyncLogEntry {
  type: 'status' | 'error' | 'stage'
  message?: string
  stage?: string
  current?: number
  total?: number
  name?: string
}

export interface ConfirmPromptDeps {
  // Classic (no deps passed) uses the real native dialogs, byte-for-byte
  // today's behavior. Remote passes useRemoteDialog()'s confirmDialog/
  // promptDialog instead — every call site below stays a one-line edit
  // (await confirmFn(...)/promptFn(...)) regardless of which is active.
  confirmFn?: (msg: string) => boolean | Promise<boolean>
  promptFn?: (msg: string, initial?: string, opts?: { multiline?: boolean }) => string | null | Promise<string | null>
}

// All of ProfilesPage's state and handlers, shared verbatim between
// ProfilesClassicView and ProfilesRemoteView — the two views differ only in
// which JSX they render against this same data/callbacks. See
// utils/settingsLayout.ts for how a view is chosen.
export function useProfilesPageState(deps: ConfirmPromptDeps = {}) {
  const confirmFn = deps.confirmFn ?? ((msg: string) => window.confirm(msg))
  const promptFn = deps.promptFn ?? ((msg: string, initial?: string) => window.prompt(msg, initial))
  // window.alert() has no Remote-friendly equivalent worth a modal for a
  // plain fire-and-forget notice — instead every call site below sets this,
  // and each view decides how to surface it (Classic: a tiny effect turns it
  // back into a real window.alert(); Remote: inline .errorText/RemoteDialog).
  const [genericAlert, setGenericAlertState] = useState<string | null>(null)
  function alertFn(msg: string) { setGenericAlertState(msg) }
  function clearGenericAlert() { setGenericAlertState(null) }

  const { user } = useAuth()
  const { refresh: refreshActiveProfile } = useActiveProfile()
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
  const [yearPickerProfile, setYearPickerProfile] = useState<Profile | null>(null)
  const [newProfileName, setNewProfileName] = useState('')
  const [newProfileId, setNewProfileId] = useState('')
  const [profileError, setProfileError] = useState('')
  const [iconPickerFor, setIconPickerFor] = useState<string | null>(null)
  // Plugins per device (global)
  const [openPluginsFor, setOpenPluginsFor] = useState<number | null>(null)
  const [devicePlugins, setDevicePlugins] = useState<DevicePluginItem[]>([])
  const [newPluginUrl, setNewPluginUrl] = useState('')
  const [newPluginName, setNewPluginName] = useState('')
  const [pluginError, setPluginError] = useState('')
  // Plugin overrides per profile
  const [profilePluginsFor, setProfilePluginsFor] = useState<string | null>(null)
  const [profilePlugins, setProfilePlugins] = useState<ProfilePluginItem[]>([])
  const [newProfilePluginUrl, setNewProfilePluginUrl] = useState('')
  const [newProfilePluginName, setNewProfilePluginName] = useState('')
  const [profilePluginError, setProfilePluginError] = useState('')
  // Telegram
  const [tgStatus, setTgStatus] = useState<TelegramStatus | null>(null)
  const [tgCode, setTgCode] = useState<{ code: string; link: string; ttl_min: number } | null>(null)
  const [tgLoading, setTgLoading] = useState(false)
  const [tgCodeCopied, setTgCodeCopied] = useState(false)
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
    refreshActiveProfile()
  }, [refreshActiveProfile])

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

  // Poll telegram status while linking code is active
  useEffect(() => {
    if (!tgCode) return
    const id = setInterval(async () => {
      const res = await fetch('/api/telegram/status')
      if (!res.ok) return
      const status = await res.json()
      if (status.linked) {
        setTgStatus(status)
        setTgCode(null)
      }
    }, 3000)
    return () => clearInterval(id)
  }, [tgCode])

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
      alertFn(d.error || 'Ошибка создания устройства')
    }
  }

  async function handleLink(e: React.FormEvent) {
    e.preventDefault()
    setLinkError('')
    setLinkSuccess('')
    setLinkLoading(true)
    const body: Record<string, string | number> = { code: linkCode }
    if (linkDeviceId === 'new') {
      body.name = linkNewName || 'Устройство'
    } else {
      body.device_id = linkDeviceId
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
    const name = await promptFn('Новое название:', currentName)
    if (!name || name.trim() === currentName) return
    await fetch(`/api/devices/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.trim() }),
    })
    fetchDevices()
  }

  async function handleRegenToken(id: number, name: string) {
    if (!(await confirmFn(`Старый токен устройства «${name}» перестанет работать. Продолжить?`))) return
    const res = await fetch(`/api/devices/${id}/regenerate-token`, { method: 'POST' })
    if (res.ok) {
      fetchDevices()
      setVisibleTokens(s => { const n = new Set(s); n.add(id); return n })
    }
  }

  async function handleClearTimecodes(id: number, name: string) {
    if (!(await confirmFn(`Удалить все таймкоды устройства «${name}»?`))) return
    await fetch(`/api/devices/${id}/timecodes`, { method: 'DELETE' })
    fetchDevices()
  }

  async function handleDeleteDevice(id: number, name: string) {
    if (!(await confirmFn(`Удалить устройство «${name}» и все его таймкоды?`))) return
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
    setProfilePluginsFor(null)
    setOpenPluginsFor(null)
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
    refreshActiveProfile()
  }

  async function openDevicePlugins(id: number) {
    if (openPluginsFor === id) { setOpenPluginsFor(null); return }
    setOpenPluginsFor(id)
    setPluginError('')
    setNewPluginUrl('')
    setNewPluginName('')
    setProfilePluginsFor(null)
    setOpenProfilesFor(null)
    const res = await fetch(`/api/devices/${id}/plugins`)
    if (res.ok) {
      const data = await res.json()
      setDevicePlugins(data.plugins || [])
    }
  }

  async function reloadDevicePlugins() {
    if (!openPluginsFor) return
    const res = await fetch(`/api/devices/${openPluginsFor}/plugins`)
    if (!res.ok) return
    const data = await res.json()
    setDevicePlugins(data.plugins || [])
  }

  async function handleAddPlugin(e: React.FormEvent) {
    e.preventDefault()
    if (!openPluginsFor) return
    setPluginError('')
    const res = await fetch(`/api/devices/${openPluginsFor}/plugins`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: newPluginUrl.trim(), name: newPluginName.trim() }),
    })
    if (res.ok) {
      setNewPluginUrl('')
      setNewPluginName('')
      reloadDevicePlugins()
    } else {
      const d = await res.json().catch(() => ({}))
      setPluginError(d.error || 'Ошибка добавления плагина')
    }
  }

  async function handleToggleDevicePlugin(p: DevicePluginItem) {
    if (!openPluginsFor) return
    await fetch(`/api/devices/${openPluginsFor}/plugins/${p.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: !p.enabled }),
    })
    reloadDevicePlugins()
  }

  async function handleRenamePlugin(p: DevicePluginItem) {
    if (!openPluginsFor) return
    const name = await promptFn('Название плагина:', p.name)
    if (name === null || name.trim() === p.name) return
    await fetch(`/api/devices/${openPluginsFor}/plugins/${p.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.trim() }),
    })
    reloadDevicePlugins()
  }

  async function handleEditPluginUrl(p: DevicePluginItem) {
    if (!openPluginsFor) return
    const url = await promptFn('URL плагина:', p.url)
    if (url === null || url.trim() === p.url) return
    const res = await fetch(`/api/devices/${openPluginsFor}/plugins/${p.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: url.trim() }),
    })
    if (!res.ok) {
      const d = await res.json().catch(() => ({}))
      alertFn(d.error || 'Ошибка изменения URL')
    }
    reloadDevicePlugins()
  }

  async function handleDeletePlugin(p: DevicePluginItem) {
    if (!openPluginsFor) return
    if (!(await confirmFn(`Удалить плагин «${p.name || p.url}»?`))) return
    await fetch(`/api/devices/${openPluginsFor}/plugins/${p.id}`, { method: 'DELETE' })
    reloadDevicePlugins()
  }

  async function openProfilePlugins(profileId: string) {
    if (profilePluginsFor === profileId) { setProfilePluginsFor(null); return }
    if (!openProfilesFor) return
    setProfilePluginsFor(profileId)
    setNewProfilePluginUrl('')
    setNewProfilePluginName('')
    setProfilePluginError('')
    const res = await fetch(`/api/devices/${openProfilesFor}/profiles/${profileUrlParam(profileId)}/plugins`)
    if (res.ok) {
      const data = await res.json()
      setProfilePlugins(data.plugins || [])
    }
  }

  async function reloadProfilePlugins() {
    if (!openProfilesFor || profilePluginsFor === null) return
    const res = await fetch(`/api/devices/${openProfilesFor}/profiles/${profileUrlParam(profilePluginsFor)}/plugins`)
    if (!res.ok) return
    const data = await res.json()
    setProfilePlugins(data.plugins || [])
  }

  // Returns null on success, an error message otherwise.
  async function handleSetProfileOverride(url: string, enabled: boolean, name?: string): Promise<string | null> {
    if (!openProfilesFor || profilePluginsFor === null) return null
    const res = await fetch(`/api/devices/${openProfilesFor}/profiles/${profileUrlParam(profilePluginsFor)}/plugins`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(name === undefined ? { url, enabled } : { url, enabled, name }),
    })
    if (!res.ok) {
      const d = await res.json().catch(() => ({}))
      return d.error || 'Ошибка добавления плагина'
    }
    reloadProfilePlugins()
    return null
  }

  async function handleClearProfileOverride(url: string) {
    if (!openProfilesFor || profilePluginsFor === null) return
    await fetch(`/api/devices/${openProfilesFor}/profiles/${profileUrlParam(profilePluginsFor)}/plugins?url=${encodeURIComponent(url)}`, {
      method: 'DELETE',
    })
    reloadProfilePlugins()
  }

  async function handleRenameProfilePlugin(pp: ProfilePluginItem) {
    const value = await promptFn('Название плагина (только для этого профиля):', pp.name)
    if (value === null || value.trim() === pp.name) return
    const effective = pp.override ?? pp.device_enabled
    const err = await handleSetProfileOverride(pp.url, effective, value.trim())
    if (err) alertFn(err)
  }

  async function handleEditProfilePluginUrl(pp: ProfilePluginItem) {
    const value = await promptFn('URL плагина (только для этого профиля):', pp.url)
    if (value === null || value.trim() === pp.url || !value.trim()) return
    const effective = pp.override ?? pp.device_enabled
    // Validate/add the new URL before dropping the old override, so a bad
    // replacement URL doesn't leave the profile with neither.
    const err = await handleSetProfileOverride(value.trim(), effective, pp.name)
    if (err) { alertFn(err); return }
    await handleClearProfileOverride(pp.url)
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
    const name = await promptFn('Новое название профиля:', currentName)
    if (!name || name.trim() === currentName) return
    await fetch(`/api/devices/${openProfilesFor}/profiles/${profileId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: name.trim() }),
    })
    reloadProfiles()
  }

  async function handleToggleChild(p: Profile) {
    const newChild = !p.child
    await fetch(`/api/devices/${openProfilesFor}/profiles/${p.profile_id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ child: newChild, child_birth_year: newChild ? p.child_birth_year ?? null : 0 }),
    })
    reloadProfiles()
  }

  function handleSetBirthYear(p: Profile) {
    setYearPickerProfile(p)
  }

  async function handleSaveBirthYear(year: number | null) {
    if (!yearPickerProfile || !openProfilesFor) return
    setYearPickerProfile(null)
    await fetch(`/api/devices/${openProfilesFor}/profiles/${yearPickerProfile.profile_id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ child_birth_year: year ?? 0 }),
    })
    reloadProfiles()
  }

  async function handleEditParams(p: Profile) {
    const current = JSON.stringify(p.params ?? {}, null, 2)
    const input = await promptFn('Параметры профиля (JSON):', current, { multiline: true })
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
      alertFn('Неверный JSON')
    }
  }

  async function handleClearProfileTimecodes(profileId: string, name: string) {
    if (!(await confirmFn(`Удалить все таймкоды профиля «${name}»?`))) return
    const url = profileId === ''
      ? `/api/devices/${openProfilesFor}/default-timecodes`
      : `/api/devices/${openProfilesFor}/profiles/${profileId}/timecodes`
    await fetch(url, { method: 'DELETE' })
    reloadProfiles()
    fetchDevices()
  }

  async function handleDeleteProfile(profileId: string, name: string) {
    if (!(await confirmFn(`Удалить профиль «${name}» и все его таймкоды?`))) return
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
    else { const d = await res.json().catch(() => ({})); alertFn(d.error || 'Ошибка') }
  }

  async function handleTgUnlink() {
    if (!(await confirmFn('Отвязать Telegram от аккаунта?'))) return
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
    if (!(await confirmFn('Импорт полностью заменит все устройства, профили и таймкоды. Продолжить?'))) return
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
    if (!(await confirmFn('Удалить аккаунт и все данные? Это необратимо!'))) return
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
      alertFn(d.error || 'Ошибка удаления аккаунта')
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


  return {
    user, isPremium, roleLabel, maxDevices,
    genericAlert, clearGenericAlert,

    // Devices
    devices, visibleTokens, toggleToken, copied, copyToken,
    newDeviceName, setNewDeviceName, createLoading, handleCreate,
    handleRename, handleRegenToken, handleClearTimecodes, handleDeleteDevice,

    // Device profiles panel
    openProfilesFor, openProfiles, profiles, profilesLimit, profileError,
    newProfileName, setNewProfileName, newProfileId, setNewProfileId, handleCreateProfile,
    iconPickerFor, setIconPickerFor, handleSetIcon,
    handleRenameProfile, handleToggleChild, handleSetBirthYear, handleEditParams,
    handleClearProfileTimecodes, handleDeleteProfile,
    yearPickerProfile, setYearPickerProfile, handleSaveBirthYear,

    // Device plugins panel
    openPluginsFor, openDevicePlugins, devicePlugins, pluginError,
    newPluginUrl, setNewPluginUrl, newPluginName, setNewPluginName, handleAddPlugin,
    handleToggleDevicePlugin, handleRenamePlugin, handleEditPluginUrl, handleDeletePlugin,

    // Profile plugins panel
    profilePluginsFor, openProfilePlugins, profilePlugins, profilePluginError, setProfilePluginError,
    newProfilePluginUrl, setNewProfilePluginUrl, newProfilePluginName, setNewProfilePluginName,
    handleSetProfileOverride, handleClearProfileOverride,
    handleRenameProfilePlugin, handleEditProfilePluginUrl,

    // Link by code
    linkCode, setLinkCode, linkDeviceId, setLinkDeviceId, linkNewName, setLinkNewName,
    linkLoading, linkError, linkSuccess, handleLink,
    linkedToken, setLinkedToken, tokenCopied, copyLinkedToken,

    // MyShows sync
    syncDeviceId, setSyncDeviceId, syncNewDeviceName, setSyncNewDeviceName,
    syncProfileId, setSyncProfileId, syncNewProfileName, setSyncNewProfileName,
    syncDeviceProfiles, setSyncDeviceProfiles, handleSyncDeviceChange,
    syncLogin, setSyncLogin, syncPassword, setSyncPassword,
    syncLoading, syncDone, syncLog, syncLogRef, handleMyShowsSync,
    lastStage, lastStatus, errors, formatSyncEntry,

    // Telegram
    tgStatus, tgCode, tgLoading, tgCodeCopied, setTgCodeCopied,
    handleGenerateTgCode, handleTgUnlink,

    // LampaC import
    importDeviceId, setImportDeviceId, importNewDeviceName, setImportNewDeviceName,
    importProfileId, setImportProfileId, importNewProfileName, setImportNewProfileName,
    importDeviceProfiles, setImportDeviceProfiles, handleImportDeviceChange,
    importJson, setImportJson, importLoading, importMsg, importError, setImportError, handleLampacImport,

    // Lampa (file_view) import
    fileDeviceId, setFileDeviceId, fileNewDeviceName, setFileNewDeviceName,
    fileProfileId, setFileProfileId, fileNewProfileName, setFileNewProfileName,
    fileDeviceProfiles, setFileDeviceProfiles, fetchProfilesForDevice,
    fileJson, setFileJson, fileLoading, fileMsg, fileError, setFileError, handleFileImport,

    // Notifications
    notifSettings, setNotifSettings, notifSaving, notifMsg, handleSaveNotif,

    // Account settings
    disable2faPw, setDisable2faPw, disable2faCode, setDisable2faCode,
    disable2faLoading, disable2faMsg, handleDisable2FA,
    pwCurrent, setPwCurrent, pwNew, setPwNew, pwNew2, setPwNew2, pwTotp, setPwTotp,
    pwLoading, pwMsg, handleChangePassword,
    delPw, setDelPw, delTotp, setDelTotp, delLoading, handleDeleteAccount,

    // Backup
    backupMsg, backupError, handleExport, handleImportFile,
  }
}
