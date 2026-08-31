import { useEffect, useRef, useState } from 'react'
import { useActiveProfile, profileUrlParam, type Profile } from '@/contexts/ActiveProfileContext'
import { PROFILE_ICON_IDS, profileIconSrc, profileInitials } from '@/utils/profileIcon'
import { getGridCols } from '@/utils/scrollNav'
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

function QuickEdit({ profile, rowKey, onDone }: { profile: Profile; rowKey: string; onDone: () => void }) {
  const { refresh } = useActiveProfile()
  const [name, setName] = useState(profile.name)
  const [savingIcon, setSavingIcon] = useState<string | null>(null)
  const gridRef = useRef<HTMLDivElement>(null)

  // Autofocus the current icon so keyboard/remote users land somewhere
  // useful the moment this opens, same as RemoteIconPicker.
  useEffect(() => {
    const current = gridRef.current?.querySelector<HTMLElement>(`[data-icon="${profile.icon || 'id1'}"]`)
    ;(current ?? gridRef.current?.querySelector<HTMLElement>('[data-nav-item]'))?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
      {/* Its own row in the same Up/Down sequence as the profile items —
          data-icon-grid marks it for 2D Left/Right/Up/Down instead of the
          normal "Up/Down = go to a different row" handling; running off the
          grid's own top/bottom edge is what falls through to the next/prev
          row (the rename row below, or the item/edit row above). */}
      <div className={styles.iconGrid} data-row-id={`${rowKey}-icons`} data-icon-grid ref={gridRef}>
        {PROFILE_ICON_IDS.map(id => (
          <button
            key={id}
            type="button"
            data-nav-item
            data-icon={id}
            className={`${styles.iconOption}${profile.icon === id ? ' ' + styles.iconOptionActive : ''}`}
            disabled={savingIcon !== null}
            onClick={() => handlePickIcon(id)}
          >
            <img src={profileIconSrc(id)} alt="" />
          </button>
        ))}
      </div>
      <form className={styles.renameForm} data-row-id={`${rowKey}-rename`} onSubmit={handleRename}>
        <input
          className={styles.renameInput}
          data-nav-item
          value={name}
          placeholder={profile.profile_id === '' ? 'Основной' : 'Имя профиля'}
          onChange={e => setName(e.target.value)}
          maxLength={100}
        />
        <button type="submit" className={styles.renameSave} data-nav-item>Сохранить</button>
      </form>
    </div>
  )
}

export function ProfileSwitcher() {
  const { devices, profiles, activeProfile, loaded, selectProfile } = useActiveProfile()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const avatarBtnRef = useRef<HTMLButtonElement>(null)
  // Left/Right-column memory, scoped to this one dropdown — same idea as
  // ProfilesRemoteView's lastColumnIdx (moving Up/Down keeps whatever
  // column you were last on, clamped to whatever the target row actually
  // has), just local to this component instead of page-wide.
  const lastColumnIdx = useRef(0)

  useEffect(() => {
    if (!open) return
    function onPointer(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) { setOpen(false); setEditing(false) }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') { setOpen(false); setEditing(false); return }
      if (e.key === 'Backspace') {
        // Backspace inside the rename field edits it, doesn't close the
        // menu — same guard as RemoteDialog's own prompt input.
        const tag = (e.target as HTMLElement | null)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA') return
        // Closes the menu instead of bubbling to Layout's own Backspace
        // ("Назад", navigates the page back) — document fires before
        // window in the bubble chain, so stopping it here is enough, no
        // capture-phase trick needed like ProfilesRemoteView's back button.
        e.preventDefault()
        e.stopPropagation()
        setOpen(false)
        setEditing(false)
      }
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  // Autofocus the first menu item on open, and hand focus back to the
  // avatar button whenever the dropdown closes (Escape, outside click, or
  // picking a profile) — same "return focus to the trigger" convention as
  // RemoteSelect/RemoteDialog.
  const wasOpenRef = useRef(false)
  useEffect(() => {
    if (open) { lastColumnIdx.current = 0; dropdownRef.current?.querySelector<HTMLElement>('[data-nav-item]')?.focus() }
    else if (wasOpenRef.current) avatarBtnRef.current?.focus()
    wasOpenRef.current = open
  }, [open])

  // Same [data-row-id]/[data-nav-item] model as everywhere else in the
  // site (Left/Right within a row, Up/Down between rows) — just scoped to
  // this dropdown's own subtree instead of the whole page, since it's a
  // self-contained popup. The one addition is data-icon-grid rows (the
  // icon picker): Up/Down there move by a grid row (±cols) instead of
  // jumping straight to the next/prev outer row, only falling through to
  // that once the 2D move runs off the grid's own top/bottom edge — that's
  // what lets Down carry you out of the icons into the rename field below.
  // Always stopPropagation once we're inside a row: this dropdown lives
  // inside Layout's [data-top-nav] scope, and without it Layout's own
  // top-nav Left/Right cycling would also fire for the same keypress and
  // yank focus to a completely different nav link while the popup is open.
  function handleDropdownKeyDown(e: React.KeyboardEvent) {
    if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return
    const container = dropdownRef.current
    const active = document.activeElement as HTMLElement | null
    const currentRow = active?.closest<HTMLElement>('[data-row-id]')
    if (!container || !currentRow || !container.contains(currentRow)) return
    e.stopPropagation()

    const items = Array.from(currentRow.querySelectorAll<HTMLElement>('[data-nav-item]'))
    const idx = items.indexOf(active as HTMLElement)
    if (idx === -1) return
    const isGridRow = currentRow.hasAttribute('data-icon-grid')

    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault()
      const next = idx + (e.key === 'ArrowRight' ? 1 : -1)
      if (next < 0 || next >= items.length) return
      lastColumnIdx.current = next
      items[next]?.focus()
      return
    }

    if (isGridRow) {
      const cols = getGridCols(items)
      const next = idx + (e.key === 'ArrowDown' ? cols : -cols)
      if (next >= 0 && next < items.length) {
        e.preventDefault()
        lastColumnIdx.current = next
        items[next]?.focus()
        return
      }
      // Ran off the grid's own top/bottom edge — fall through below to
      // move to the adjacent outer row instead.
    }

    e.preventDefault()
    const rows = Array.from(container.querySelectorAll<HTMLElement>('[data-row-id]'))
      .filter(r => r.querySelector('[data-nav-item]'))
    const rowIdx = rows.indexOf(currentRow)
    const targetIdx = rowIdx + (e.key === 'ArrowDown' ? 1 : -1)
    if (targetIdx < 0 || targetIdx >= rows.length) return
    const targetItems = Array.from(rows[targetIdx].querySelectorAll<HTMLElement>('[data-nav-item]'))
    if (!targetItems.length) return
    // A grid column index doesn't mean anything on a plain row (mostly ≠0,
    // e.g. the icon grid is 6 wide but the rename row below it is only 2) —
    // leaving a grid always lands on that row's first item instead of
    // carrying the column over, so Down out of the icons reliably reaches
    // the rename field rather than skipping straight to Save.
    const targetIdx2 = isGridRow ? 0 : Math.min(lastColumnIdx.current, targetItems.length - 1)
    lastColumnIdx.current = targetIdx2
    targetItems[targetIdx2]?.focus()
  }

  if (!loaded || devices.length === 0) return null

  const showDeviceName = devices.length > 1

  return (
    <div className={styles.switcher} ref={ref}>
      <button
        ref={avatarBtnRef}
        className={styles.avatarBtn}
        data-top-nav-profile
        onClick={() => setOpen(o => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        title={activeProfile ? profileLabel(activeProfile) : 'Профиль'}
      >
        <Avatar profile={activeProfile} className={styles.avatarImg} />
      </button>

      {open && (
        <div className={styles.dropdown} role="menu" ref={dropdownRef} onKeyDown={handleDropdownKeyDown}>
          {profiles.length === 0 && <div className={styles.empty}>Нет профилей</div>}
          {profiles.map(p => {
            const isActive = p.device_id === activeProfile?.device_id && p.profile_id === activeProfile?.profile_id
            const deviceName = devices.find(d => d.id === p.device_id)?.name
            const rowKey = `${p.device_id}-${p.profile_id || '__default__'}`
            return (
              <div key={rowKey}>
                <div className={styles.itemRow} data-row-id={rowKey}>
                  <button
                    role="menuitem"
                    data-nav-item
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
                      data-nav-item
                      className={styles.editBtn}
                      title="Изменить профиль"
                      onClick={e => { e.stopPropagation(); setEditing(v => !v) }}
                    >
                      ✏️
                    </button>
                  )}
                </div>
                {isActive && editing && (
                  <QuickEdit profile={p} rowKey={rowKey} onDone={() => setEditing(false)} />
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
