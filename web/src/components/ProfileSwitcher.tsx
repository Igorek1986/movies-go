import { useEffect, useRef, useState } from 'react'
import { useActiveProfile, profileUrlParam, type Profile } from '@/contexts/ActiveProfileContext'
import { PROFILE_ICON_IDS, profileIconSrc, profileInitials } from '@/utils/profileIcon'
import styles from './ProfileSwitcher.module.scss'

function profileLabel(p: Profile): string {
  return p.name || (p.profile_id === '' ? 'Основной' : p.profile_id)
}

function Avatar({ profile, className }: { profile: Profile | null; className?: string }) {
  if (profile?.icon) {
    return <img className={className} src={profileIconSrc(profile.icon)} alt="" />
  }
  return <span className={className}>{profileInitials(profile ? profileLabel(profile) : '?')}</span>
}

function QuickEdit({ profile, onDone }: { profile: Profile; onDone: () => void }) {
  const { refresh } = useActiveProfile()
  const [name, setName] = useState(profile.name)
  const [savingIcon, setSavingIcon] = useState<string | null>(null)

  async function patch(body: Record<string, string>) {
    await fetch(`/api/devices/${profile.device_id}/profiles/${profileUrlParam(profile.profile_id)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    await refresh()
  }

  async function handlePickIcon(iconId: string) {
    setSavingIcon(iconId)
    await patch({ icon: iconId })
    setSavingIcon(null)
  }

  async function handleRename(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed || trimmed === profile.name) return
    await patch({ name: trimmed })
    onDone()
  }

  return (
    <div className={styles.quickEdit} onClick={e => e.stopPropagation()}>
      <div className={styles.iconGrid}>
        {PROFILE_ICON_IDS.map(id => (
          <button
            key={id}
            type="button"
            className={`${styles.iconOption}${profile.icon === id ? ' ' + styles.iconOptionActive : ''}`}
            disabled={savingIcon !== null}
            onClick={() => handlePickIcon(id)}
          >
            <img src={profileIconSrc(id)} alt="" />
          </button>
        ))}
      </div>
      <form className={styles.renameForm} onSubmit={handleRename}>
        <input
          className={styles.renameInput}
          value={name}
          placeholder={profile.profile_id === '' ? 'Основной' : 'Имя профиля'}
          onChange={e => setName(e.target.value)}
          maxLength={100}
        />
        <button type="submit" className={styles.renameSave}>Сохранить</button>
      </form>
    </div>
  )
}

export function ProfileSwitcher() {
  const { devices, profiles, activeProfile, loaded, selectProfile } = useActiveProfile()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onPointer(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) { setOpen(false); setEditing(false) }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') { setOpen(false); setEditing(false) }
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!loaded || devices.length === 0) return null

  const showDeviceName = devices.length > 1

  return (
    <div className={styles.switcher} ref={ref}>
      <button
        className={styles.avatarBtn}
        onClick={() => setOpen(o => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        title={activeProfile ? profileLabel(activeProfile) : 'Профиль'}
      >
        <Avatar profile={activeProfile} className={styles.avatarImg} />
      </button>

      {open && (
        <div className={styles.dropdown} role="menu">
          {profiles.length === 0 && <div className={styles.empty}>Нет профилей</div>}
          {profiles.map(p => {
            const isActive = p.device_id === activeProfile?.device_id && p.profile_id === activeProfile?.profile_id
            const deviceName = devices.find(d => d.id === p.device_id)?.name
            return (
              <div key={`${p.device_id}:${p.profile_id}`}>
                <div className={styles.itemRow}>
                  <button
                    role="menuitem"
                    className={`${styles.item}${isActive ? ' ' + styles.itemActive : ''}`}
                    onClick={() => { selectProfile(p); setOpen(false); setEditing(false) }}
                  >
                    <Avatar profile={p} className={styles.itemAvatar} />
                    <span className={styles.itemText}>
                      <span className={styles.itemName}>{profileLabel(p)}</span>
                      {showDeviceName && deviceName && <span className={styles.itemDevice}>{deviceName}</span>}
                    </span>
                  </button>
                  {isActive && (
                    <button
                      type="button"
                      className={styles.editBtn}
                      title="Изменить профиль"
                      onClick={e => { e.stopPropagation(); setEditing(v => !v) }}
                    >
                      ✏️
                    </button>
                  )}
                </div>
                {isActive && editing && (
                  <QuickEdit profile={p} onDone={() => setEditing(false)} />
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
