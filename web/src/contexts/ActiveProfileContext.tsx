import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'

export interface Device {
  id: number
  name: string
  token: string
}

export interface Profile {
  device_id: number
  profile_id: string
  name: string
  icon: string
}

interface StoredProfileKey {
  device_id: number
  profile_id: string
}

export const ACTIVE_PROFILE_STORAGE_KEY = 'active_profile'
const PROFILE_KEY = ACTIVE_PROFILE_STORAGE_KEY

// chi's router never matches an empty path segment, so the implicit ""
// default profile can't be addressed as .../profiles/ in a URL — use this
// sentinel instead (the backend translates it back to "").
export const DEFAULT_PROFILE_URL_PARAM = '_default'

export function profileUrlParam(profileId: string): string {
  return profileId === '' ? DEFAULT_PROFILE_URL_PARAM : encodeURIComponent(profileId)
}

function loadStoredKey(): StoredProfileKey | null {
  try {
    const raw = localStorage.getItem(PROFILE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    if (typeof parsed?.device_id === 'number' && typeof parsed?.profile_id === 'string') return parsed
    return null
  } catch {
    return null
  }
}

interface ActiveProfileState {
  devices: Device[]
  profiles: Profile[]
  activeDevice: Device | null
  activeProfile: Profile | null
  loaded: boolean
  selectProfile: (p: Profile) => void
  refresh: () => Promise<void>
}

const ActiveProfileContext = createContext<ActiveProfileState | null>(null)

export function ActiveProfileProvider({ children }: { children: ReactNode }) {
  const [devices, setDevices] = useState<Device[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [activeKey, setActiveKey] = useState<StoredProfileKey | null>(() => loadStoredKey())
  const [loaded, setLoaded] = useState(false)

  const refresh = useCallback(async () => {
    const res = await fetch('/api/devices')
    if (!res.ok) { setLoaded(true); return }
    const devList: Device[] = await res.json()

    const allProfiles: Profile[] = []
    await Promise.all(devList.map(async d => {
      const pRes = await fetch(`/api/devices/${d.id}/profiles`)
      if (!pRes.ok) return
      const pd: { profiles: { profile_id: string; name: string; icon: string }[] } = await pRes.json()
      for (const p of pd.profiles) {
        allProfiles.push({ device_id: d.id, profile_id: p.profile_id, name: p.name, icon: p.icon })
      }
      // Devices with no timecodes yet have no profile rows at all — add a
      // synthetic default entry so the device stays selectable in the switcher.
      if (!pd.profiles.length) {
        allProfiles.push({ device_id: d.id, profile_id: '', name: '', icon: '' })
      }
    }))

    setDevices(devList)
    setProfiles(allProfiles)
    setActiveKey(prev => {
      const stillValid = prev && allProfiles.some(p => p.device_id === prev.device_id && p.profile_id === prev.profile_id)
      if (stillValid) return prev
      const fallback = allProfiles[0]
      const next = fallback ? { device_id: fallback.device_id, profile_id: fallback.profile_id } : null
      if (next) localStorage.setItem(PROFILE_KEY, JSON.stringify(next))
      else localStorage.removeItem(PROFILE_KEY)
      return next
    })
    setLoaded(true)
  }, [])

  useEffect(() => { refresh() }, [refresh])

  const selectProfile = useCallback((p: Profile) => {
    const key = { device_id: p.device_id, profile_id: p.profile_id }
    setActiveKey(key)
    localStorage.setItem(PROFILE_KEY, JSON.stringify(key))
  }, [])

  const activeProfile = profiles.find(p => p.device_id === activeKey?.device_id && p.profile_id === activeKey?.profile_id) ?? null
  const activeDevice = devices.find(d => d.id === (activeProfile?.device_id ?? activeKey?.device_id)) ?? null

  return (
    <ActiveProfileContext.Provider value={{ devices, profiles, activeDevice, activeProfile, loaded, selectProfile, refresh }}>
      {children}
    </ActiveProfileContext.Provider>
  )
}

export function useActiveProfile(): ActiveProfileState {
  const ctx = useContext(ActiveProfileContext)
  if (!ctx) throw new Error('useActiveProfile must be used within ActiveProfileProvider')
  return ctx
}
