import { useEffect, useRef, useState } from 'react'
import Layout from '@/components/Layout'
import PasswordInput from '@/components/PasswordInput'
import { BottomNavSettings } from '@/components/BottomNavSettings'
import { CardLayoutSettings } from '@/components/CardLayoutSettings'
import { BrowseLayoutSettings } from '@/components/BrowseLayoutSettings'
import { SettingsLayoutSettings } from '@/components/SettingsLayoutSettings'
import { RemoteDialog } from '@/components/remote/RemoteDialog'
import { RemoteSelect } from '@/components/remote/RemoteSelect'
import { RemoteIconPicker } from '@/components/remote/RemoteIconPicker'
import { useRemoteDialog } from '@/components/remote/useRemoteDialog'
import { focusTopNavActive } from '@/utils/scrollNav'
import { profileIconSrc } from '@/utils/profileIcon'
import { useProfilesPageState } from './useProfilesPageState'
// Shared look with Classic — inputs/buttons/text styles that don't change
// between views live here; ProfilesRemoteView.module.scss only adds the
// accordion/row-nav-specific rules.
import classicStyles from './ProfilesClassicView.module.scss'
import styles from './ProfilesRemoteView.module.scss'

// Only elements where Backspace actually edits text — a focused radio/
// checkbox/number/etc <input> doesn't consume Backspace at all, so it must
// NOT count here, or the capture-phase "Назад" listener below silently does
// nothing whenever focus happens to be on one (e.g. any settings radio row).
const TEXT_EDITING_INPUT_TYPES = new Set(['text', 'password', 'email', 'search', 'tel', 'url', 'number', ''])
function isTypingTarget(el: Element | null) {
  const tag = el?.tagName
  if (tag === 'TEXTAREA' || (el as HTMLElement | null)?.isContentEditable) return true
  if (tag === 'INPUT') return TEXT_EDITING_INPUT_TYPES.has((el as HTMLInputElement).type)
  return false
}

type SectionId =
  | 'devices' | 'link' | 'myshows' | 'telegram' | 'lampacImport' | 'fileImport'
  | 'notifications' | 'interface'
  | 'account' | 'backup'

// The four layout/appearance settings, grouped under one "Интерфейс" menu
// item with its own one-level submenu (same drill-down idea as devices, just
// one level instead of three).
type InterfaceSubId = 'bottomNav' | 'cardLayout' | 'browseLayout' | 'settingsLayout'
const INTERFACE_SUBMENU: { id: InterfaceSubId; title: string }[] = [
  { id: 'bottomNav', title: 'Панель навигации' },
  { id: 'cardLayout', title: 'Вид карточки фильма/сериала' },
  { id: 'browseLayout', title: 'Вид Каталога и Моё' },
  { id: 'settingsLayout', title: 'Дизайн страницы «Настройки»' },
]

export default function ProfilesRemoteView() {
  const { dialogEl, confirmDialog, promptDialog } = useRemoteDialog()
  const {
    user, isPremium, roleLabel, maxDevices,
    genericAlert, clearGenericAlert,
    devices, visibleTokens, toggleToken, copied, copyToken,
    newDeviceName, setNewDeviceName, createLoading, handleCreate,
    handleRename, handleRegenToken, handleClearTimecodes, handleDeleteDevice,
    openProfilesFor, openProfiles, profiles, profilesLimit, profileError,
    newProfileName, setNewProfileName, newProfileId, setNewProfileId, handleCreateProfile,
    iconPickerFor, setIconPickerFor, handleSetIcon,
    handleRenameProfile, handleToggleChild, handleSetBirthYear, handleEditParams,
    handleClearProfileTimecodes, handleDeleteProfile,
    yearPickerProfile, setYearPickerProfile, handleSaveBirthYear,
    openPluginsFor, openDevicePlugins, devicePlugins, pluginError,
    newPluginUrl, setNewPluginUrl, newPluginName, setNewPluginName, handleAddPlugin,
    handleToggleDevicePlugin, handleRenamePlugin, handleEditPluginUrl, handleDeletePlugin,
    profilePluginsFor, openProfilePlugins, profilePlugins, profilePluginError, setProfilePluginError,
    newProfilePluginUrl, setNewProfilePluginUrl, newProfilePluginName, setNewProfilePluginName,
    handleSetProfileOverride, handleClearProfileOverride,
    handleRenameProfilePlugin, handleEditProfilePluginUrl,
    linkCode, setLinkCode, linkDeviceId, setLinkDeviceId, linkNewName, setLinkNewName,
    linkLoading, linkError, linkSuccess, handleLink,
    linkedToken, setLinkedToken, tokenCopied, copyLinkedToken,
    syncDeviceId, setSyncDeviceId, syncNewDeviceName, setSyncNewDeviceName,
    syncProfileId, setSyncProfileId, syncNewProfileName, setSyncNewProfileName,
    syncDeviceProfiles, setSyncDeviceProfiles, handleSyncDeviceChange,
    syncLogin, setSyncLogin, syncPassword, setSyncPassword,
    syncLoading, syncDone, syncLog, syncLogRef, handleMyShowsSync,
    lastStage, lastStatus, errors, formatSyncEntry,
    tgStatus, tgCode, tgLoading, tgCodeCopied, setTgCodeCopied,
    handleGenerateTgCode, handleTgUnlink,
    importDeviceId, setImportDeviceId, importNewDeviceName, setImportNewDeviceName,
    importProfileId, setImportProfileId, importNewProfileName, setImportNewProfileName,
    importDeviceProfiles, setImportDeviceProfiles, handleImportDeviceChange,
    importJson, setImportJson, importLoading, importMsg, importError, setImportError, handleLampacImport,
    fileDeviceId, setFileDeviceId, fileNewDeviceName, setFileNewDeviceName,
    fileProfileId, setFileProfileId, fileNewProfileName, setFileNewProfileName,
    fileDeviceProfiles, setFileDeviceProfiles, fetchProfilesForDevice,
    fileJson, setFileJson, fileLoading, fileMsg, fileError, setFileError, handleFileImport,
    notifSettings, setNotifSettings, notifSaving, notifMsg, handleSaveNotif,
    disable2faPw, setDisable2faPw, disable2faCode, setDisable2faCode,
    disable2faLoading, disable2faMsg, handleDisable2FA,
    pwCurrent, setPwCurrent, pwNew, setPwNew, pwNew2, setPwNew2, pwTotp, setPwTotp,
    pwLoading, pwMsg, handleChangePassword,
    delPw, setDelPw, delTotp, setDelTotp, delLoading, handleDeleteAccount,
    backupMsg, backupError, handleExport, handleImportFile,
  } = useProfilesPageState({ confirmFn: confirmDialog, promptFn: promptDialog })

  // Lampa-style drill-down: a plain list of category rows; picking one
  // replaces the list with just that category's own screen (a back row up
  // top, its content below) — not an accordion. Starts at the list (unlike
  // the earlier accordion draft, which opened "Устройства" by default) since
  // that's the actual Lampa settings pattern this is modeled on.
  const [activeSection, setActiveSectionState] = useState<SectionId | null>(null)
  // "Мои устройства" has two more drill-down levels of its own (device ->
  // профили-или-плагины -> профиль-плагины) — no inline expand/collapse
  // anywhere, same one-screen-at-a-time model as the top-level menu, just
  // three levels deep instead of one.
  const [deviceSubview, setDeviceSubview] = useState<{ deviceId: number; view: 'profiles' | 'plugins' } | null>(null)
  const [profilePluginsView, setProfilePluginsView] = useState<{ deviceId: number; profileId: string } | null>(null)
  // "Интерфейс" has its own one-level submenu — same drill-down idea, just
  // one level instead of devices' three.
  const [interfaceSubview, setInterfaceSubview] = useState<InterfaceSubId | null>(null)
  const navRef = useRef({ activeSection, deviceSubview, profilePluginsView, interfaceSubview })
  navRef.current = { activeSection, deviceSubview, profilePluginsView, interfaceSubview }

  function setActiveSection(id: SectionId | null) {
    setActiveSectionState(id)
  }

  // Mobile/tablet only ($bp-lg, matches .section's own bottom-sheet media
  // query in the CSS): the drilled-into section is a `position: fixed`
  // sheet detached from the page, which otherwise leaves the near-empty
  // background page still scrollable underneath it — same scroll-lock idea
  // as Layout's own mobile drawer (see its menuOpen effect).
  useEffect(() => {
    if (activeSection && window.innerWidth <= 1024) {
      document.body.style.overflow = 'hidden'
      document.body.style.overscrollBehavior = 'contain'
    } else {
      document.body.style.overflow = ''
      document.body.style.overscrollBehavior = ''
    }
    return () => {
      document.body.style.overflow = ''
      document.body.style.overscrollBehavior = ''
    }
  }, [activeSection])

  // Remembers the last horizontal focus index (Left/Right) *globally*, not
  // per row — so Up/Down through a column of same-shaped rows (Панель
  // навигации's checkbox/↑/↓ list) stays in that column instead of resetting
  // to index 0 on every row, and landing on a differently-shaped row simply
  // clamps to whatever index it actually has (see focusRow below).
  const lastColumnIdx = useRef(0)
  // Backup's "Импортировать" is a real button (see Backup section) that
  // clicks this hidden input — Classic's <label>-wraps-hidden-input version
  // isn't reachable by keyboard at all (a bug that predates this split, left
  // alone there — see the plan's compromises).
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Up/Down/Left/Right between/within [data-row-id] rows — same model as
  // CardDetailPage/SessionsPage (see their comments for the full rationale):
  // getRows() reads the live DOM on every keypress, so collapsed sections
  // simply contribute no rows, no extra bookkeeping needed. Left/Right at a
  // row's outward edge is left unhandled (no preventDefault) so Layout's
  // desktop side-panel-summon can still claim it.
  useEffect(() => {
    // Always the *full*, unfiltered nav-item list, in DOM order — this is
    // what "column index" (lastColumnIdx) means: position 0 is always the
    // checkbox column, 1 is always ↑, 2 is always ↓, on every Панель
    // навигации row, whether or not that particular row happens to have
    // ↑ or ↓ disabled. Filtering disabled items out of this list (an
    // earlier version of this code did) shifts every later index down on
    // whichever row has the earlier one disabled, so "index 1" stops
    // meaning the same physical column from row to row — that's what made
    // Up/Down drift into the wrong column instead of staying put.
    function allNavItems(el: HTMLElement) {
      return Array.from(el.querySelectorAll<HTMLElement>('[data-nav-item]'))
    }
    function isDisabled(el: HTMLElement) {
      return el.hasAttribute('disabled')
    }
    function getRows() {
      return Array.from(document.querySelectorAll<HTMLElement>('[data-row-id]'))
        .filter(row => allNavItems(row).some(it => !isDisabled(it)))
    }
    // Lands on the closest enabled item to the requested column — searching
    // outward (both directions at once) rather than only forward, so e.g.
    // asking for column 1 (↑) on the very first row (↑ disabled there)
    // lands on column 0 (checkbox), not column 2 (↓): the nearer one wins.
    function focusRow(row: HTMLElement) {
      const items = allNavItems(row)
      if (!items.length) return
      const start = Math.min(lastColumnIdx.current, items.length - 1)
      let target: HTMLElement | undefined
      for (let d = 0; d < items.length; d++) {
        if (start - d >= 0 && !isDisabled(items[start - d])) { target = items[start - d]; break }
        if (start + d < items.length && !isDisabled(items[start + d])) { target = items[start + d]; break }
      }
      target?.focus({ preventScroll: true })
      target?.scrollIntoView({ block: 'center', behavior: 'smooth' })
    }
    function onKeyDown(e: KeyboardEvent) {
      if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return

      const focused = document.activeElement as HTMLElement | null
      const tag = focused?.tagName
      // A number input (Notifications' hour fields) keeps Up/Down native —
      // its own spinner. Everything else falls through to row-nav only at
      // the actual edge of whatever text is there (no selection): a
      // single-line <input> bridges out at the start/end of its text for
      // Left/Right (Up/Down have no native meaning in one, so those always
      // fall through — there's no Tab key on a remote to get out
      // otherwise); a <textarea> (multiline JSON import/prompt) bridges out
      // at the start/end of its text for Left/Right same as an input, and
      // at the first/last *line* for Up/Down (still needs those to move the
      // cursor between lines everywhere else, same "only at the edge" idea
      // as leaving a row via Left/Right below).
      const isNumberInput = tag === 'INPUT' && (focused as HTMLInputElement).type === 'number'
      if (isNumberInput) return
      // Only text-like input types support .selectionStart/.selectionEnd at
      // all (radio/checkbox/etc throw if you touch them) — those act like
      // any other nav-item instead (row-nav owns all four arrows, e.g. the
      // Снизу/Справа/Слева radio row and Панель навигации's checkboxes).
      const TEXT_LIKE_INPUT_TYPES = new Set(['text', 'password', 'email', 'search', 'tel', 'url', ''])
      const isTextLikeInput = tag === 'INPUT' && TEXT_LIKE_INPUT_TYPES.has((focused as HTMLInputElement).type)
      if (isTextLikeInput && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
        const input = focused as HTMLInputElement
        const pos = e.key === 'ArrowLeft' ? 0 : input.value.length
        if (input.selectionStart !== pos || input.selectionEnd !== pos) return
      }
      if (tag === 'TEXTAREA') {
        const area = focused as HTMLTextAreaElement
        const start = area.selectionStart ?? 0
        const end = area.selectionEnd ?? area.value.length
        if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
          const pos = e.key === 'ArrowLeft' ? 0 : area.value.length
          if (start !== pos || end !== pos) return
        } else {
          const atFirstLine = area.value.slice(0, start).indexOf('\n') === -1
          const atLastLine = area.value.slice(end).indexOf('\n') === -1
          if (e.key === 'ArrowUp' && !atFirstLine) return
          if (e.key === 'ArrowDown' && !atLastLine) return
        }
      }

      const currentRow = focused?.closest<HTMLElement>('[data-row-id]') ?? null

      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        if (!currentRow) return
        const items = allNavItems(currentRow)
        const idx = items.indexOf(focused as HTMLElement)
        if (idx === -1) return
        const step = e.key === 'ArrowRight' ? 1 : -1
        // Skip past any disabled item(s) in the direction of travel — e.g.
        // the checkbox -> ↓ skip over a disabled ↑ on the very first row.
        // Running off the end this way (never finding an enabled item)
        // means the row has nothing further that way — leave the event
        // unhandled so Layout's side-panel-summon can claim it, same as
        // reaching a real edge.
        let next = idx + step
        while (next >= 0 && next < items.length && isDisabled(items[next])) next += step
        if (next < 0 || next >= items.length) return
        e.preventDefault()
        lastColumnIdx.current = next
        items[next]?.focus({ preventScroll: true })
        items[next]?.scrollIntoView({ block: 'nearest', inline: 'center', behavior: 'smooth' })
        return
      }

      const rows = getRows()
      if (!currentRow) {
        if (e.key === 'ArrowDown' && rows.length) { e.preventDefault(); focusRow(rows[0]) }
        return
      }
      e.preventDefault()
      const rowIdx = rows.indexOf(currentRow)
      if (e.key === 'ArrowUp' && rowIdx === 0) { focusTopNavActive(); return }
      const targetIdx = rowIdx + (e.key === 'ArrowDown' ? 1 : -1)
      if (targetIdx < 0 || targetIdx >= rows.length) return
      focusRow(rows[targetIdx])
    }
    // Keeps lastColumnIdx in sync with a mouse click too (not just
    // keyboard Left/Right) — so clicking, say, a ↓ button and then
    // pressing ArrowDown lands on the next row's ↓, not back at its
    // checkbox.
    function onFocusIn(e: FocusEvent) {
      const target = e.target as HTMLElement | null
      const row = target?.closest<HTMLElement>('[data-row-id]')
      if (!row || !target?.hasAttribute('data-nav-item')) return
      const idx = allNavItems(row).indexOf(target)
      if (idx !== -1) lastColumnIdx.current = idx
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('focusin', onFocusIn)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('focusin', onFocusIn)
    }
  }, [])

  // Backspace: capture phase, ahead of Layout's own bubble-phase "Назад"
  // listener (Layout registers before this component in commit order — see
  // its own long comment on effect/listener ordering — so a plain bubble
  // listener here would fire AFTER Layout already navigated away). Pops
  // exactly one drill-down level (profilePluginsView -> deviceSubview ->
  // activeSection -> menu); does nothing at the menu, letting the event
  // fall through normally to Layout's own "Назад".
  //
  // Refocusing the row we drilled out of has to wait for the next render —
  // none of these screens stay mounted while a deeper one is showing (no
  // inline expand/collapse anywhere in this view), so a synchronous
  // querySelector right after clearing the state always ran against the
  // still-drilled-in DOM and found nothing (focus then fell back to
  // <body>).
  const pendingRefocusRowId = useRef<string | null>(null)
  function goBack() {
    const { activeSection: sec, deviceSubview: sub, profilePluginsView: ppv, interfaceSubview: isub } = navRef.current
    if (ppv) {
      // profile-{key}-bottom holds several buttons (Детский/Год рождения/
      // Параметры/Плагины ›) — data-focus-id picks out specifically the one
      // that opened this view, not just whichever comes first in the row.
      pendingRefocusRowId.current = `profile-${ppv.deviceId}-${ppv.profileId || '__default__'}-plugins`
      setProfilePluginsView(null)
    } else if (sub) {
      // Same idea — device-{id} holds both Профили › and Плагины ›.
      pendingRefocusRowId.current = `device-${sub.deviceId}-${sub.view}`
      setDeviceSubview(null)
    } else if (isub) {
      pendingRefocusRowId.current = `iface-menu-${isub}`
      setInterfaceSubview(null)
    } else if (sec) {
      pendingRefocusRowId.current = `menu-${sec}`
      setActiveSection(null)
    }
  }
  useEffect(() => {
    if (!pendingRefocusRowId.current) return
    const id = pendingRefocusRowId.current
    pendingRefocusRowId.current = null
    // data-focus-id targets one specific button directly (see goBack's
    // device/profile-plugins branches); everything else still has exactly
    // one [data-nav-item] per [data-row-id] row, so falling back to that
    // covers menu-*/iface-menu-* the same as before.
    const el = document.querySelector<HTMLElement>(`[data-focus-id="${CSS.escape(id)}"]`)
      ?? document.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(id)}"] [data-nav-item]`)
    el?.focus()
  }, [activeSection, deviceSubview, profilePluginsView, interfaceSubview])

  // Same idea as pendingRefocusRowId above, but for RemoteIconPicker/the
  // birth-year RemoteSelect (forceOpen mode has no trigger button of its
  // own to remember, unlike RemoteSelect's normal closed/open mode — see
  // its own triggerRef) — closing either one refocuses whichever profile
  // row button opened it, instead of stranding focus at <body>.
  const prevIconPickerFor = useRef<string | null>(null)
  useEffect(() => {
    if (prevIconPickerFor.current && !iconPickerFor) {
      document.querySelector<HTMLElement>(`[data-icon-trigger="${prevIconPickerFor.current}"]`)?.focus()
    }
    prevIconPickerFor.current = iconPickerFor
  }, [iconPickerFor])
  const prevYearPickerProfileId = useRef<string | null>(null)
  useEffect(() => {
    if (prevYearPickerProfileId.current !== null && !yearPickerProfile) {
      document.querySelector<HTMLElement>(`[data-year-trigger="${prevYearPickerProfileId.current}"]`)?.focus()
    }
    prevYearPickerProfileId.current = yearPickerProfile?.profile_id ?? null
  }, [yearPickerProfile])

  useEffect(() => {
    function onBackspaceCapture(e: KeyboardEvent) {
      if (e.key !== 'Backspace') return
      if (isTypingTarget(document.activeElement)) return
      // A RemoteDialog/RemoteSelect/RemoteIconPicker overlay owns Backspace
      // itself (closes just the overlay) — this listener runs in the
      // capture phase, ahead of that overlay's own bubble-phase onKeyDown,
      // so without this guard it always won the race and popped a whole
      // drill-down level instead of just closing the overlay on top of it.
      if ((document.activeElement as HTMLElement | null)?.closest('[data-remote-overlay]')) return
      const { activeSection: sec, deviceSubview: sub, profilePluginsView: ppv, interfaceSubview: isub } = navRef.current
      if (!sec && !sub && !ppv && !isub) return
      e.preventDefault()
      e.stopPropagation()
      goBack()
    }
    window.addEventListener('keydown', onBackspaceCapture, true)
    return () => window.removeEventListener('keydown', onBackspaceCapture, true)
  }, [])

  // Clicking outside the drilled-into panel, or the bottom-nav/desktop-panel
  // "Назад" button, pops one drill-down level too — same "go back" as
  // Backspace, instead of clicking through to whatever's underneath (the
  // page background) or leaving the page entirely (Назад's normal history
  // navigation). Capture phase so the "Назад" button's own onClick (which
  // would otherwise call Layout's handleBottomBack and navigate away) never
  // fires — same idea as the Backspace listener above.
  useEffect(() => {
    function onCaptureClick(e: MouseEvent) {
      const { activeSection: sec, deviceSubview: sub, profilePluginsView: ppv, interfaceSubview: isub } = navRef.current
      if (!sec && !sub && !ppv && !isub) return
      const target = e.target as HTMLElement | null
      if (!target) return
      if (target.closest('[data-key="back"]')) {
        e.preventDefault()
        e.stopPropagation()
        goBack()
        return
      }
      // Everything below only cares about the empty backdrop — leave normal
      // clicks (inside the panel itself, a RemoteDialog/RemoteSelect/
      // RemoteIconPicker overlay, the top nav, or any other bottom-nav
      // button) alone.
      if (target.closest(`.${styles.section}`)) return
      if (target.closest('[data-remote-overlay]')) return
      if (target.closest('[data-top-nav]') || target.closest('[data-bottom-nav]')) return
      goBack()
    }
    window.addEventListener('click', onCaptureClick, true)
    return () => window.removeEventListener('click', onCaptureClick, true)
  }, [])

  const menu: { id: SectionId; title: React.ReactNode }[] = [
    {
      id: 'devices', title: (
        <>
          Мои устройства
          <span className={classicStyles.roleBadge}>{roleLabel[user?.role ?? ''] ?? user?.role}</span>
          {maxDevices !== null && <span className={classicStyles.limitHint}> · {devices.length} / {maxDevices}</span>}
        </>
      ),
    },
    { id: 'link', title: 'Привязать устройство по коду' },
    { id: 'myshows', title: 'Синхронизация MyShows' },
    { id: 'telegram', title: 'Telegram' },
    { id: 'lampacImport', title: 'Импорт таймкодов из LampaC' },
    { id: 'fileImport', title: 'Импорт таймкодов из Lampa' },
    ...(tgStatus?.linked && notifSettings ? [{ id: 'notifications' as const, title: 'Уведомления' }] : []),
    { id: 'interface', title: 'Интерфейс' },
    { id: 'account', title: 'Настройки аккаунта' },
    { id: 'backup', title: 'Резервная копия' },
  ]
  const currentDevice = deviceSubview ? devices.find(d => d.id === deviceSubview.deviceId) : undefined
  const currentProfile = profilePluginsView
    ? profiles.find(p => p.profile_id === profilePluginsView.profileId)
    : undefined
  const currentTitle: React.ReactNode = profilePluginsView
    ? `Плагины — ${currentProfile ? (currentProfile.profile_id === '' ? 'Основной' : currentProfile.name) : ''}`
    : deviceSubview
      ? `${deviceSubview.view === 'profiles' ? 'Профили' : 'Плагины'} — ${currentDevice?.name ?? ''}`
      : interfaceSubview
        ? INTERFACE_SUBMENU.find(m => m.id === interfaceSubview)?.title
        : menu.find(m => m.id === activeSection)?.title

  return (
    <Layout>
      <div className={styles.page}>

        {activeSection === null ? (
          <div className={styles.menu}>
            {menu.map(item => (
              <div key={item.id} className={styles.menuRow} data-row-id={`menu-${item.id}`}>
                <button type="button" className={styles.menuRowBtn} data-nav-item onClick={() => setActiveSection(item.id)}>
                  <span className={styles.menuRowTitle}>{item.title}</span>
                  <span className={styles.menuRowChevron}>›</span>
                </button>
              </div>
            ))}
          </div>
        ) : (
        <div className={styles.section}>
          <div className={styles.backRow} data-row-id="back">
            <button type="button" className={styles.backBtn} data-nav-item onClick={goBack}>
              <span className={styles.backChevron}>‹</span>
              {currentTitle}
            </button>
          </div>

          {activeSection === 'devices' && !deviceSubview && (
            <div className={styles.sectionBody}>
              {devices.length === 0 ? (
                <p className={classicStyles.empty}>Устройств ещё нет</p>
              ) : devices.map(d => (
                <div key={d.id} className={styles.deviceRow}>
                  <div className={classicStyles.deviceInfo}>
                    <strong className={classicStyles.deviceName}>{d.name}</strong>
                    <div className={classicStyles.tokenRow} data-row-id={`device-${d.id}-token`}>
                      <button
                        type="button"
                        className={classicStyles.tokenCode}
                        data-nav-item
                        title="Нажмите, чтобы скопировать"
                        onClick={() => copyToken(d.id, d.token)}
                      >
                        {visibleTokens.has(d.id) ? d.token : d.token.slice(0, 4) + '-••••-••••-••••'}
                      </button>
                      <button className={classicStyles.btnIcon} data-nav-item onClick={() => toggleToken(d.id)} title="Показать/скрыть">
                        {visibleTokens.has(d.id) ? '🙈' : '👁'}
                      </button>
                      {copied === d.id && <span className={classicStyles.copiedHint}>Скопировано!</span>}
                    </div>
                    <span className={styles.deviceMeta}>
                      Таймкодов: {d.timecodes_count} · {new Date(d.created_at).toLocaleDateString('ru-RU')}
                    </span>
                  </div>
                  <div className={styles.deviceActions} data-row-id={`device-${d.id}`}>
                    <button className={classicStyles.btnSm} data-nav-item data-focus-id={`device-${d.id}-profiles`} onClick={() => { if (openProfilesFor !== d.id) openProfiles(d.id); setDeviceSubview({ deviceId: d.id, view: 'profiles' }) }}>
                      Профили ›
                    </button>
                    <button className={classicStyles.btnSm} data-nav-item data-focus-id={`device-${d.id}-plugins`} onClick={() => { if (openPluginsFor !== d.id) openDevicePlugins(d.id); setDeviceSubview({ deviceId: d.id, view: 'plugins' }) }}>
                      Плагины ›
                    </button>
                    <button className={classicStyles.btnSm} data-nav-item onClick={() => handleRename(d.id, d.name)}>Переименовать</button>
                    <button className={classicStyles.btnSm} data-nav-item onClick={() => handleRegenToken(d.id, d.name)}>Новый токен</button>
                    <button className={`${classicStyles.btnSm} ${classicStyles.warning}`} data-nav-item onClick={() => handleClearTimecodes(d.id, d.name)}>Очистить</button>
                    <button className={`${classicStyles.btnSm} ${classicStyles.danger}`} data-nav-item onClick={() => handleDeleteDevice(d.id, d.name)}>Удалить</button>
                  </div>
                </div>
              ))}

              {(maxDevices === null || devices.length < maxDevices) ? (
                <form className={styles.fieldRow} onSubmit={handleCreate}>
                  <div data-row-id="device-add-input">
                    <input
                      className={classicStyles.input}
                      data-nav-item
                      placeholder="Название нового устройства"
                      value={newDeviceName}
                      onChange={e => setNewDeviceName(e.target.value)}
                      required
                      maxLength={100}
                    />
                  </div>
                  <div data-row-id="device-add-submit">
                    <button type="submit" className={classicStyles.btnPrimary} data-nav-item disabled={createLoading}>
                      {createLoading ? 'Создание…' : 'Добавить устройство'}
                    </button>
                  </div>
                </form>
              ) : (
                <p className={classicStyles.limitReached}>Достигнут лимит устройств ({maxDevices})</p>
              )}
            </div>
          )}

          {/* ── Device → Профили (own screen, not an inline panel) ── */}
          {deviceSubview?.view === 'profiles' && !profilePluginsView && currentDevice && (
            <div className={styles.sectionBody}>
              {profiles.length === 0 && <p className={classicStyles.empty}>Нет профилей</p>}
              {profiles.map(p => {
                const key = `${currentDevice.id}-${p.profile_id || '__default__'}`
                return (
                  <div key={key} className={classicStyles.profileCard}>
                    <div className={classicStyles.profileCardTop} data-row-id={`profile-${key}-top`}>
                      <div className={classicStyles.profileCardLeft}>
                        <div className={classicStyles.profileNameRow}>
                          <div className={classicStyles.profileIconWrap}>
                            <button
                              className={classicStyles.profileIconBtn}
                              data-nav-item
                              data-icon-trigger={p.profile_id}
                              title="Изменить иконку"
                              onClick={() => setIconPickerFor(iconPickerFor === p.profile_id ? null : p.profile_id)}
                            >
                              <img src={profileIconSrc(p.icon || 'id1')} alt="" />
                            </button>
                            {iconPickerFor === p.profile_id && (
                              <RemoteIconPicker
                                current={p.icon || 'id1'}
                                onSelect={icon => handleSetIcon(p.profile_id, icon)}
                                onClose={() => setIconPickerFor(null)}
                              />
                            )}
                          </div>
                          <strong className={classicStyles.profileName}>{p.profile_id === '' ? 'Основной' : p.name}</strong>
                        </div>
                        <div className={classicStyles.profileMeta}>
                          {p.profile_id !== '' && <code className={classicStyles.profileId}>ID: {p.profile_id}</code>}
                          <span>{p.profile_id !== '' ? '· ' : ''}таймкодов: {p.timecodes_count}</span>
                        </div>
                      </div>
                      <div className={classicStyles.profileCardActions}>
                        <button className={classicStyles.btnIcon} data-nav-item title="Переименовать" onClick={() => handleRenameProfile(p.profile_id, p.name)}>✏️</button>
                        <button className={`${classicStyles.btnSm} ${classicStyles.warning}`} data-nav-item onClick={() => handleClearProfileTimecodes(p.profile_id, p.name)}>Очистить</button>
                        <button className={`${classicStyles.btnSm} ${classicStyles.danger}`} data-nav-item onClick={() => handleDeleteProfile(p.profile_id, p.name)}>Удалить</button>
                      </div>
                    </div>
                    <div className={classicStyles.profileCardBottom} data-row-id={`profile-${key}-bottom`}>
                      <button className={`${classicStyles.btnSm} ${p.child ? classicStyles.active : ''}`} data-nav-item onClick={() => handleToggleChild(p)}>
                        Детский {p.child ? '✓' : ''}
                      </button>
                      {p.child && (
                        <button className={classicStyles.btnSm} data-nav-item data-year-trigger={p.profile_id} onClick={() => handleSetBirthYear(p)} title="Год рождения ребёнка">
                          {p.child_birth_year ? `${p.child_birth_year} (${new Date().getFullYear() - p.child_birth_year} лет)` : 'Год рождения'}
                        </button>
                      )}
                      <button className={classicStyles.btnSm} data-nav-item onClick={() => handleEditParams(p)}>Параметры</button>
                      <button
                        className={classicStyles.btnSm}
                        data-nav-item
                        data-focus-id={`profile-${key}-plugins`}
                        onClick={() => { if (profilePluginsFor !== p.profile_id) openProfilePlugins(p.profile_id); setProfilePluginsView({ deviceId: currentDevice.id, profileId: p.profile_id }) }}
                      >
                        Плагины ›
                      </button>
                    </div>
                  </div>
                )
              })}
              {profileError && <p className={classicStyles.errorText}>{profileError}</p>}
              {(profilesLimit === 0 || profiles.length < profilesLimit) && (
                <form className={styles.fieldRow} onSubmit={handleCreateProfile}>
                  <div className={styles.fieldGrid} data-row-id={`profile-${currentDevice.id}-add`}>
                    <input
                      className={classicStyles.input}
                      data-nav-item
                      placeholder="Название профиля"
                      value={newProfileName}
                      onChange={e => setNewProfileName(e.target.value)}
                      required
                    />
                    <input
                      className={classicStyles.input}
                      data-nav-item
                      placeholder="ID профиля (авто если пусто)"
                      value={newProfileId}
                      onChange={e => setNewProfileId(e.target.value.replace(/[^a-zA-Z0-9_-]/g, '').slice(0, 32))}
                      maxLength={32}
                    />
                  </div>
                  <div data-row-id={`profile-${currentDevice.id}-add-submit`}>
                    <button type="submit" className={classicStyles.btnPrimary} data-nav-item>
                      Добавить профиль
                    </button>
                  </div>
                </form>
              )}
            </div>
          )}

          {/* ── Device → Плагины (own screen) ── */}
          {deviceSubview?.view === 'plugins' && currentDevice && (
            <div className={styles.sectionBody}>
              {devicePlugins.length === 0 && <p className={classicStyles.empty}>Плагинов ещё нет</p>}
              {devicePlugins.map(p => (
                <div key={p.id} className={classicStyles.profileCard} data-row-id={`devplugin-${currentDevice.id}-${p.id}`}>
                  <div className={classicStyles.profileCardLeft}>
                    <strong className={classicStyles.profileName}>{p.name || 'Без названия'}</strong>
                    <code className={classicStyles.profileId} style={{ wordBreak: 'break-all' }}>{p.url}</code>
                  </div>
                  {/* Always its own row below the title/URL (not the split-row
                      .profileCardTop layout) — that one puts actions inline to
                      the right only while there's room, so a short plugin name
                      looked different from a long URL that wraps the actions
                      underneath. Same actions, consistent position either way. */}
                  <div className={classicStyles.profileCardBottom}>
                    <button className={`${classicStyles.btnSm} ${p.enabled ? classicStyles.active : ''}`} data-nav-item onClick={() => handleToggleDevicePlugin(p)}>
                      {p.enabled ? 'Включён ✓' : 'Выключен'}
                    </button>
                    <button className={classicStyles.btnIcon} data-nav-item title="Переименовать" onClick={() => handleRenamePlugin(p)}>✏️</button>
                    <button className={classicStyles.btnIcon} data-nav-item title="Изменить URL" onClick={() => handleEditPluginUrl(p)}>🔗</button>
                    <button className={`${classicStyles.btnSm} ${classicStyles.danger}`} data-nav-item onClick={() => handleDeletePlugin(p)}>Удалить</button>
                  </div>
                </div>
              ))}
              {pluginError && <p className={classicStyles.errorText}>{pluginError}</p>}
              <form className={styles.fieldRow} onSubmit={handleAddPlugin}>
                <div className={styles.fieldGrid} data-row-id={`devplugin-${currentDevice.id}-add`}>
                  <input className={classicStyles.input} data-nav-item placeholder="URL плагина (https://...)" value={newPluginUrl} onChange={e => setNewPluginUrl(e.target.value)} required />
                  <input className={classicStyles.input} data-nav-item placeholder="Название (необязательно)" value={newPluginName} onChange={e => setNewPluginName(e.target.value)} maxLength={100} />
                </div>
                <div data-row-id={`devplugin-${currentDevice.id}-add-submit`}>
                  <button type="submit" className={classicStyles.btnPrimary} data-nav-item>
                    Добавить плагин
                  </button>
                </div>
              </form>
            </div>
          )}

          {/* ── Device → Профили → Плагины профиля (own screen) ── */}
          {profilePluginsView && (
            <div className={styles.sectionBody}>
              {profilePlugins.length === 0 && (
                <p className={classicStyles.empty}>Нет плагинов устройства — добавьте их в разделе «Плагины» устройства</p>
              )}
              {profilePlugins.map(pp => {
                const effective = pp.override ?? pp.device_enabled
                const key = `${profilePluginsView.deviceId}-${profilePluginsView.profileId || '__default__'}`
                return (
                  <div key={pp.url} className={classicStyles.profileCard} data-row-id={`profplugin-${key}-${pp.url}`}>
                    <div className={classicStyles.profileCardLeft}>
                      <span className={classicStyles.profileName}>
                        {pp.name || (pp.in_device_list ? pp.url : 'Без названия')}
                      </span>
                      {!pp.in_device_list && (
                        <>
                          <span className={classicStyles.profileMeta}>только для профиля</span>
                          <code className={classicStyles.profileId} style={{ wordBreak: 'break-all' }}>{pp.url}</code>
                        </>
                      )}
                    </div>
                    {/* Own row below the title, always — same fix as the
                        device plugin list above: a right-aligned action group
                        whose button count varies per row (2 here, 4 there)
                        looked staggered/inconsistent lined up next to titles
                        of different lengths. */}
                    <div className={classicStyles.profileCardBottom}>
                      <button className={`${classicStyles.btnSm} ${effective ? classicStyles.active : ''}`} data-nav-item onClick={() => handleSetProfileOverride(pp.url, !effective)}>
                        {effective ? 'Вкл ✓' : 'Выкл'}
                      </button>
                      {!pp.in_device_list && (
                        <>
                          <button className={classicStyles.btnIcon} data-nav-item title="Переименовать" onClick={() => handleRenameProfilePlugin(pp)}>✏️</button>
                          <button className={classicStyles.btnIcon} data-nav-item title="Изменить URL" onClick={() => handleEditProfilePluginUrl(pp)}>🔗</button>
                        </>
                      )}
                      {pp.override !== null && pp.in_device_list && (
                        <button className={classicStyles.btnSm} data-nav-item onClick={() => handleClearProfileOverride(pp.url)}>Сброс</button>
                      )}
                      {!pp.in_device_list && (
                        <button className={`${classicStyles.btnSm} ${classicStyles.danger}`} data-nav-item onClick={() => handleClearProfileOverride(pp.url)}>Удалить</button>
                      )}
                    </div>
                  </div>
                )
              })}
              <div className={styles.fieldGrid} data-row-id={`profplugin-${profilePluginsView.deviceId}-${profilePluginsView.profileId || '__default__'}-add`}>
                <input
                  className={classicStyles.input}
                  data-nav-item
                  placeholder="URL плагина только для профиля"
                  value={newProfilePluginUrl}
                  onChange={e => setNewProfilePluginUrl(e.target.value)}
                />
                <input
                  className={classicStyles.input}
                  data-nav-item
                  placeholder="Название (необязательно)"
                  value={newProfilePluginName}
                  onChange={e => setNewProfilePluginName(e.target.value)}
                />
              </div>
              <div data-row-id={`profplugin-${profilePluginsView.deviceId}-${profilePluginsView.profileId || '__default__'}-add-submit`}>
                <button
                  className={classicStyles.btnSm}
                  data-nav-item
                  onClick={async () => {
                    const url = newProfilePluginUrl.trim()
                    if (!url) return
                    setProfilePluginError('')
                    const err = await handleSetProfileOverride(url, true, newProfilePluginName.trim())
                    if (err) { setProfilePluginError(err); return }
                    setNewProfilePluginUrl('')
                    setNewProfilePluginName('')
                  }}
                >
                  Добавить
                </button>
              </div>
              {profilePluginError && <p className={classicStyles.errorText}>{profilePluginError}</p>}
            </div>
          )}

        {/* ── Link by code ── */}
          {activeSection === 'link' && (
            <div className={styles.sectionBody}>
              <p className={classicStyles.hint}>В настройках плагина нажмите «Привязать устройство» — на экране появится 6-значный код.</p>
              {linkError && <p className={classicStyles.errorText}>{linkError}</p>}
              {linkSuccess && <p className={classicStyles.successText}>{linkSuccess}</p>}
              <form className={styles.fieldRow} onSubmit={handleLink}>
                <div className={styles.fieldGrid} data-row-id="link-fields">
                  <input
                    className={classicStyles.input}
                    data-nav-item
                    placeholder="Код (6 цифр)"
                    value={linkCode}
                    onChange={e => setLinkCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    inputMode="numeric" maxLength={6} required
                  />
                  <RemoteSelect
                    value={String(linkDeviceId)}
                    onChange={v => setLinkDeviceId(v === 'new' ? 'new' : Number(v))}
                    options={[
                      ...devices.map(d => ({ value: String(d.id), label: d.name })),
                      ...((maxDevices === null || devices.length < (maxDevices ?? 99)) ? [{ value: 'new', label: '+ Новое устройство' }] : []),
                    ]}
                  />
                </div>
                {linkDeviceId === 'new' && (
                  <div data-row-id="link-new-name">
                    <input className={classicStyles.input} data-nav-item placeholder="Название нового устройства" value={linkNewName} onChange={e => setLinkNewName(e.target.value)} maxLength={100} />
                  </div>
                )}
                <div data-row-id="link-submit">
                  <button type="submit" className={classicStyles.btnPrimary} data-nav-item disabled={linkLoading || linkCode.length < 6}>
                    {linkLoading ? 'Привязка…' : 'Привязать'}
                  </button>
                </div>
              </form>
            </div>
          )}

        {/* ── MyShows sync ── */}
          {activeSection === 'myshows' && (
            <div className={styles.sectionBody}>
              {!isPremium ? (
                <div className={classicStyles.premiumGate}>
                  <p className={classicStyles.hint}>Синхронизация с MyShows доступна для подписчиков Premium.</p>
                  <span className={classicStyles.premiumBadge}>Premium</span>
                </div>
              ) : (
                <form className={styles.fieldRow} onSubmit={handleMyShowsSync}>
                  <div className={styles.fieldGrid} data-row-id="myshows-device-profile">
                    <RemoteSelect
                      value={syncDeviceId === '' ? '' : String(syncDeviceId)}
                      placeholder="Устройство"
                      onChange={v => { if (v === 'new') { setSyncDeviceId('new'); setSyncDeviceProfiles([]); setSyncProfileId('') } else handleSyncDeviceChange(Number(v)) }}
                      options={[
                        ...devices.map(d => ({ value: String(d.id), label: d.name })),
                        ...((maxDevices === null || devices.length < (maxDevices ?? 99)) ? [{ value: 'new', label: 'Новое устройство' }] : []),
                      ]}
                    />
                    <RemoteSelect
                      value={syncProfileId}
                      placeholder="Основной"
                      disabled={syncDeviceId === ''}
                      onChange={setSyncProfileId}
                      options={[
                        ...syncDeviceProfiles.map(p => ({ value: p.profile_id, label: p.name })),
                        ...(syncDeviceId !== '' ? [{ value: 'new', label: 'Новый профиль' }] : []),
                      ]}
                    />
                  </div>
                  {syncDeviceId === 'new' && (
                    <div data-row-id="myshows-new-device">
                      <input className={classicStyles.input} data-nav-item placeholder="Название устройства" value={syncNewDeviceName} onChange={e => setSyncNewDeviceName(e.target.value)} maxLength={100} required />
                    </div>
                  )}
                  {syncDeviceId !== '' && syncProfileId === 'new' && (
                    <div data-row-id="myshows-new-profile">
                      <input className={classicStyles.input} data-nav-item placeholder="Название профиля" value={syncNewProfileName} onChange={e => setSyncNewProfileName(e.target.value)} maxLength={100} />
                    </div>
                  )}
                  <div className={styles.fieldGrid} data-row-id="myshows-creds">
                    <input className={classicStyles.input} data-nav-item placeholder="Логин MyShows" value={syncLogin} onChange={e => setSyncLogin(e.target.value)} autoComplete="username" required />
                    <PasswordInput className={classicStyles.input} data-nav-item placeholder="Пароль MyShows" value={syncPassword} onChange={e => setSyncPassword(e.target.value)} autoComplete="current-password" required />
                  </div>
                  <div data-row-id="myshows-submit">
                    <button type="submit" className={classicStyles.btnPrimary} data-nav-item disabled={syncLoading || !syncDeviceId}>
                      {syncLoading ? 'Синхронизация…' : 'Синхронизировать'}
                    </button>
                  </div>
                  {(syncLog.length > 0 || syncDone) && (
                    <div className={classicStyles.syncLog} ref={syncLogRef}>
                      {lastStage && <div className={classicStyles.syncLogLine}>{formatSyncEntry(lastStage)}</div>}
                      {!lastStage && lastStatus && <div className={classicStyles.syncLogLine}>{formatSyncEntry(lastStatus)}</div>}
                      {errors.map((entry, i) => <div key={i} className={classicStyles.syncLogError}>{formatSyncEntry(entry)}</div>)}
                      {syncDone && errors.length === 0 && <div className={classicStyles.syncLogDone}>Синхронизация завершена</div>}
                    </div>
                  )}
                </form>
              )}
            </div>
          )}

        {/* ── Telegram ── */}
          {activeSection === 'telegram' && (
            <div className={styles.sectionBody}>
              {tgStatus?.linked ? (
                <div className={styles.fieldRow}>
                  <p className={classicStyles.hint}>Аккаунт привязан{tgStatus.username ? ` к @${tgStatus.username}` : ''}.</p>
                  <div data-row-id="tg-unlink">
                    <button className={`${classicStyles.btnSm} ${classicStyles.danger}`} data-nav-item onClick={handleTgUnlink}>Отвязать Telegram</button>
                  </div>
                </div>
              ) : (
                <div className={styles.fieldRow}>
                  <p className={classicStyles.hint}>Telegram не привязан. Привяжите, чтобы получать уведомления и сбрасывать пароль через бота.</p>
                  {tgCode ? (
                    <div>
                      <p className={classicStyles.hint}>Отправьте @{tgCode.link.split('t.me/')[1]?.split('?')[0]} команду:</p>
                      <code className={classicStyles.tgCode} onClick={() => { navigator.clipboard.writeText(`/start ${tgCode.code}`).catch(() => {}); setTgCodeCopied(true); setTimeout(() => setTgCodeCopied(false), 1500) }}>
                        /start {tgCode.code}
                        {tgCodeCopied && <span className={classicStyles.copiedHint}> Скопировано!</span>}
                      </code>
                      <p className={classicStyles.hint}>Или откройте ссылку:</p>
                      <div data-row-id="tg-open-link">
                        <a href={tgCode.link} target="_blank" rel="noreferrer" className={classicStyles.tgBtn} data-nav-item>Открыть в Telegram</a>
                      </div>
                      <p className={classicStyles.hint}>Код действителен {tgCode.ttl_min} минут</p>
                    </div>
                  ) : (
                    <div data-row-id="tg-generate">
                      <button className={classicStyles.btnPrimary} data-nav-item onClick={handleGenerateTgCode} disabled={tgLoading}>
                        {tgLoading ? 'Генерация…' : 'Привязать Telegram'}
                      </button>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

        {/* ── LampaC import ── */}
          {activeSection === 'lampacImport' && (
            <div className={styles.sectionBody}>
              <p className={classicStyles.hint}>Вставьте JSON-экспорт таймкодов из LampaC.</p>
              {importError && <p className={classicStyles.errorText}>{importError}</p>}
              {importMsg && <p className={classicStyles.successText}>{importMsg}</p>}
              <form className={styles.fieldRow} onSubmit={handleLampacImport}>
                <div className={styles.fieldGrid} data-row-id="lampac-device-profile">
                  <RemoteSelect
                    value={importDeviceId === '' ? '' : String(importDeviceId)}
                    placeholder="Устройство"
                    onChange={v => { if (v === 'new') { setImportDeviceId('new'); setImportDeviceProfiles([]); setImportProfileId('') } else handleImportDeviceChange(Number(v)) }}
                    options={[
                      ...devices.map(d => ({ value: String(d.id), label: d.name })),
                      ...((maxDevices === null || devices.length < (maxDevices ?? 99)) ? [{ value: 'new', label: 'Новое устройство' }] : []),
                    ]}
                  />
                  <RemoteSelect
                    value={importProfileId}
                    placeholder="Основной"
                    disabled={importDeviceId === ''}
                    onChange={setImportProfileId}
                    options={[
                      ...importDeviceProfiles.map(p => ({ value: p.profile_id, label: p.name })),
                      ...(importDeviceId !== '' ? [{ value: 'new', label: 'Новый профиль' }] : []),
                    ]}
                  />
                </div>
                {importDeviceId === 'new' && (
                  <div data-row-id="lampac-new-device">
                    <input className={classicStyles.input} data-nav-item placeholder="Название устройства" value={importNewDeviceName} onChange={e => setImportNewDeviceName(e.target.value)} maxLength={100} required />
                  </div>
                )}
                {importDeviceId !== '' && importProfileId === 'new' && (
                  <div data-row-id="lampac-new-profile">
                    <input className={classicStyles.input} data-nav-item placeholder="Название профиля" value={importNewProfileName} onChange={e => setImportNewProfileName(e.target.value)} maxLength={100} />
                  </div>
                )}
                <div data-row-id="lampac-json">
                  <textarea
                    className={classicStyles.jsonTextarea}
                    data-nav-item
                    placeholder={'{"card_id":{"item":"data"}}'}
                    value={importJson}
                    onChange={e => { setImportJson(e.target.value); setImportError('') }}
                    rows={5}
                    required
                  />
                </div>
                <div data-row-id="lampac-submit">
                  <button type="submit" className={classicStyles.btnPrimary} data-nav-item disabled={importLoading || !importDeviceId}>
                    {importLoading ? 'Импорт…' : 'Импортировать'}
                  </button>
                </div>
              </form>
            </div>
          )}

        {/* ── Lampa import ── */}
          {activeSection === 'fileImport' && (
            <div className={styles.sectionBody}>
              <p className={classicStyles.hint}>
                В консоли браузера на странице Lampa выполните:{' '}
                <code
                  className={classicStyles.codeSnippet}
                  onClick={() => navigator.clipboard.writeText("copy(localStorage.getItem('file_view'))").catch(() => {})}
                >
                  copy(localStorage.getItem('file_view'))
                </code>
                {' '}— затем вставьте JSON ниже.
              </p>
              {fileError && <p className={classicStyles.errorText}>{fileError}</p>}
              {fileMsg && <p className={classicStyles.successText}>{fileMsg}</p>}
              <form className={styles.fieldRow} onSubmit={handleFileImport}>
                <div className={styles.fieldGrid} data-row-id="file-device-profile">
                  <RemoteSelect
                    value={fileDeviceId === '' ? '' : String(fileDeviceId)}
                    placeholder="Устройство"
                    onChange={v => {
                      if (v === 'new') { setFileDeviceId('new'); setFileDeviceProfiles([]); setFileProfileId('') }
                      else { const id = Number(v); setFileDeviceId(id); fetchProfilesForDevice(id).then(p => { setFileDeviceProfiles(p); setFileProfileId(p.length > 0 ? p[0].profile_id : '') }) }
                    }}
                    options={[
                      ...devices.map(d => ({ value: String(d.id), label: d.name })),
                      ...((maxDevices === null || devices.length < (maxDevices ?? 99)) ? [{ value: 'new', label: 'Новое устройство' }] : []),
                    ]}
                  />
                  <RemoteSelect
                    value={fileProfileId}
                    placeholder="Основной"
                    disabled={fileDeviceId === ''}
                    onChange={setFileProfileId}
                    options={[
                      ...fileDeviceProfiles.map(p => ({ value: p.profile_id, label: p.name })),
                      ...(fileDeviceId !== '' ? [{ value: 'new', label: 'Новый профиль' }] : []),
                    ]}
                  />
                </div>
                {fileDeviceId === 'new' && (
                  <div data-row-id="file-new-device">
                    <input className={classicStyles.input} data-nav-item placeholder="Название устройства" value={fileNewDeviceName} onChange={e => setFileNewDeviceName(e.target.value)} maxLength={100} required />
                  </div>
                )}
                {fileDeviceId !== '' && fileProfileId === 'new' && (
                  <div data-row-id="file-new-profile">
                    <input className={classicStyles.input} data-nav-item placeholder="Название профиля" value={fileNewProfileName} onChange={e => setFileNewProfileName(e.target.value)} maxLength={100} />
                  </div>
                )}
                <div data-row-id="file-json">
                  <textarea
                    className={classicStyles.jsonTextarea}
                    data-nav-item
                    placeholder={'{"571234":{"percent":95,"time":3600}}'}
                    value={fileJson}
                    onChange={e => { setFileJson(e.target.value); setFileError('') }}
                    rows={5}
                    required
                  />
                </div>
                <div data-row-id="file-submit">
                  <button type="submit" className={classicStyles.btnPrimary} data-nav-item disabled={fileLoading || !fileDeviceId}>
                    {fileLoading ? 'Импорт…' : 'Импортировать'}
                  </button>
                </div>
              </form>
            </div>
          )}

        {/* ── Notifications (only reachable once Telegram is linked — see menu) ── */}
        {activeSection === 'notifications' && tgStatus?.linked && notifSettings && (
              <div className={styles.sectionBody}>
                <p className={classicStyles.hint}>Уведомления об истечении подписки и неактивности отправляются в Telegram.</p>
                {notifMsg && <p className={notifMsg === 'Сохранено' ? classicStyles.successText : classicStyles.errorText}>{notifMsg}</p>}
                <form className={styles.fieldRow} onSubmit={handleSaveNotif}>
                  <div data-row-id="notif-enabled">
                    <label className={classicStyles.checkLabel}>
                      <input
                        type="checkbox"
                        data-nav-item
                        checked={notifSettings.enabled}
                        onChange={e => setNotifSettings(cur => cur ? { ...cur, enabled: e.target.checked } : cur)}
                      />
                      Включить уведомления
                    </label>
                  </div>
                  <div data-row-id="notif-timezone">
                    <RemoteSelect
                      label="Часовой пояс"
                      value={notifSettings.timezone}
                      onChange={tz => setNotifSettings(cur => cur ? { ...cur, timezone: tz } : cur)}
                      options={['Europe/Moscow', 'Europe/Kaliningrad', 'Asia/Yekaterinburg', 'Asia/Omsk',
                        'Asia/Krasnoyarsk', 'Asia/Irkutsk', 'Asia/Yakutsk', 'Asia/Vladivostok',
                        'Asia/Magadan', 'Asia/Kamchatka', 'Europe/Kiev', 'Europe/Minsk',
                        'Asia/Almaty', 'Asia/Tashkent', 'Europe/London', 'Europe/Berlin'].map(tz => ({ value: tz, label: tz }))}
                    />
                  </div>
                  <div className={styles.fieldGrid} data-row-id="notif-hours">
                    <label className={styles.fieldRow}>
                      С (час)
                      <input
                        className={classicStyles.input}
                        data-nav-item
                        type="number" min={0} max={23}
                        value={notifSettings.notify_start}
                        onChange={e => setNotifSettings(cur => cur ? { ...cur, notify_start: Number(e.target.value) } : cur)}
                      />
                    </label>
                    <label className={styles.fieldRow}>
                      До (час)
                      <input
                        className={classicStyles.input}
                        data-nav-item
                        type="number" min={0} max={23}
                        value={notifSettings.notify_end}
                        onChange={e => setNotifSettings(cur => cur ? { ...cur, notify_end: Number(e.target.value) } : cur)}
                      />
                    </label>
                  </div>
                  <div data-row-id="notif-submit">
                    <button type="submit" className={classicStyles.btnPrimary} data-nav-item disabled={notifSaving}>
                      {notifSaving ? 'Сохранение…' : 'Сохранить'}
                    </button>
                  </div>
                </form>
              </div>
        )}

        {/* ── Интерфейс: submenu, then whichever sub-section is drilled into ── */}
        {activeSection === 'interface' && !interfaceSubview && (
          <div className={styles.menu}>
            {INTERFACE_SUBMENU.map(item => (
              <div key={item.id} className={styles.menuRow} data-row-id={`iface-menu-${item.id}`}>
                <button type="button" className={styles.menuRowBtn} data-nav-item onClick={() => setInterfaceSubview(item.id)}>
                  <span className={styles.menuRowTitle}>{item.title}</span>
                  <span className={styles.menuRowChevron}>›</span>
                </button>
              </div>
            ))}
          </div>
        )}
        {interfaceSubview === 'bottomNav' && <div className={styles.sectionBody}><BottomNavSettings bare /></div>}
        {interfaceSubview === 'cardLayout' && <div className={styles.sectionBody}><CardLayoutSettings bare /></div>}
        {interfaceSubview === 'browseLayout' && <div className={styles.sectionBody}><BrowseLayoutSettings bare /></div>}
        {interfaceSubview === 'settingsLayout' && <div className={styles.sectionBody}><SettingsLayoutSettings bare /></div>}

        {/* ── Account settings ── */}
          {activeSection === 'account' && (
            <div className={styles.sectionBody}>
              <h4 className={classicStyles.subTitle}>Двухфакторная аутентификация (2FA)</h4>
              {user?.totp_enabled ? (
                <>
                  <p className={classicStyles.hint}>
                    2FA включена. Резервных кодов осталось: <strong>{user.backup_codes_count}</strong>
                  </p>
                  {disable2faMsg && (
                    <p className={disable2faMsg === '2FA отключена' ? classicStyles.successText : classicStyles.errorText}>{disable2faMsg}</p>
                  )}
                  <form className={styles.fieldRow} onSubmit={handleDisable2FA}>
                    <div className={styles.fieldGrid} data-row-id="acc-2fa-disable">
                      <PasswordInput className={classicStyles.input} data-nav-item placeholder="Текущий пароль" value={disable2faPw} onChange={e => setDisable2faPw(e.target.value)} required />
                      <input
                        className={`${classicStyles.input} ${classicStyles.inputMono}`}
                        data-nav-item
                        type="text"
                        placeholder="Код из приложения"
                        value={disable2faCode}
                        onChange={e => setDisable2faCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                        inputMode="numeric" maxLength={6} required
                      />
                    </div>
                    <div data-row-id="acc-2fa-disable-submit">
                      <button className={`${classicStyles.btnSm} ${classicStyles.warning}`} data-nav-item type="submit" disabled={disable2faLoading}>
                        {disable2faLoading ? 'Отключение…' : 'Отключить 2FA'}
                      </button>
                    </div>
                  </form>
                </>
              ) : (
                <>
                  <p className={classicStyles.hint}>Защитите аккаунт кодом из приложения-аутентификатора (Google Authenticator, Aegis и др.).</p>
                  <div data-row-id="acc-2fa-enable">
                    <a href="/setup-2fa" className={classicStyles.btnPrimary} data-nav-item>Включить 2FA</a>
                  </div>
                </>
              )}

              <hr className={classicStyles.hr} />

              <h4 className={classicStyles.subTitle}>Сменить пароль</h4>
              {pwMsg && <p className={pwMsg === 'Пароль изменён' ? classicStyles.successText : classicStyles.errorText}>{pwMsg}</p>}
              <form className={styles.fieldRow} onSubmit={handleChangePassword}>
                <div data-row-id="acc-pw-current">
                  <PasswordInput className={classicStyles.input} data-nav-item placeholder="Текущий пароль" value={pwCurrent} onChange={e => setPwCurrent(e.target.value)} required />
                </div>
                <div data-row-id="acc-pw-new">
                  <PasswordInput className={classicStyles.input} data-nav-item placeholder="Новый пароль" value={pwNew} onChange={e => setPwNew(e.target.value)} minLength={6} required />
                  {pwNew.length > 0 && pwNew.length < 6 && <span className={classicStyles.errorText}>минимум 6 символов</span>}
                </div>
                <div data-row-id="acc-pw-confirm">
                  <PasswordInput className={classicStyles.input} data-nav-item placeholder="Повторите новый пароль" value={pwNew2} onChange={e => setPwNew2(e.target.value)} required />
                  {pwNew2.length > 0 && (
                    <span className={pwNew === pwNew2 ? classicStyles.successText : classicStyles.errorText}>
                      {pwNew === pwNew2 ? 'Пароли совпадают' : 'Пароли не совпадают'}
                    </span>
                  )}
                </div>
                {user?.totp_enabled && (
                  <div data-row-id="acc-pw-totp">
                    <input
                      className={`${classicStyles.input} ${classicStyles.inputMono}`}
                      data-nav-item
                      type="text"
                      placeholder="Код 2FA"
                      value={pwTotp}
                      onChange={e => setPwTotp(e.target.value.replace(/\D/g, '').slice(0, 6))}
                      inputMode="numeric" maxLength={6}
                    />
                  </div>
                )}
                <div data-row-id="acc-pw-submit">
                  <button className={classicStyles.btnPrimary} data-nav-item type="submit" disabled={pwLoading}>
                    {pwLoading ? 'Сохранение…' : 'Сменить пароль'}
                  </button>
                </div>
              </form>

              <hr className={classicStyles.hr} />

              <h4 className={classicStyles.subTitle} style={{ color: 'var(--danger, #e05252)' }}>Удалить аккаунт</h4>
              <p className={classicStyles.hint}>Все устройства и таймкоды будут удалены безвозвратно.</p>
              <form className={styles.fieldRow} onSubmit={handleDeleteAccount}>
                <div className={styles.fieldGrid} data-row-id="acc-delete">
                  <PasswordInput className={classicStyles.input} data-nav-item placeholder="Введите пароль для подтверждения" value={delPw} onChange={e => setDelPw(e.target.value)} required />
                  {user?.totp_enabled && (
                    <input
                      className={`${classicStyles.input} ${classicStyles.inputMono}`}
                      data-nav-item
                      type="text"
                      placeholder="Код 2FA"
                      value={delTotp}
                      onChange={e => setDelTotp(e.target.value.replace(/\D/g, '').slice(0, 6))}
                      inputMode="numeric" maxLength={6}
                    />
                  )}
                </div>
                <div data-row-id="acc-delete-submit">
                  <button className={`${classicStyles.btnPrimary} ${classicStyles.danger}`} data-nav-item type="submit" disabled={delLoading}>
                    {delLoading ? 'Удаление…' : 'Удалить аккаунт'}
                  </button>
                </div>
              </form>
            </div>
          )}

        {/* ── Backup ── */}
          {activeSection === 'backup' && (
            <div className={styles.sectionBody}>
              <p className={classicStyles.hint}>Экспортирует все устройства, профили, таймкоды и настройки плагинов. Импорт полностью заменяет текущие данные.</p>
              <div className={classicStyles.backupActions} data-row-id="backup-actions">
                <button className={classicStyles.btnPrimary} data-nav-item onClick={handleExport}>Экспортировать</button>
                <button
                  type="button"
                  className={classicStyles.btnSm}
                  data-nav-item
                  onClick={() => fileInputRef.current?.click()}
                >
                  Импортировать
                </button>
                <input ref={fileInputRef} type="file" accept=".json" style={{ display: 'none' }} onChange={handleImportFile} />
              </div>
              {backupMsg && <p className={classicStyles.successText}>{backupMsg}</p>}
              {backupError && <p className={classicStyles.errorText}>{backupError}</p>}
            </div>
          )}
        </div>
        )}

      </div>
      {yearPickerProfile && (
        <RemoteSelect
          forceOpen
          label="Год рождения"
          value={String(yearPickerProfile.child_birth_year ?? '')}
          options={[
            { value: '', label: 'Убрать' },
            ...Array.from({ length: 19 }, (_, i) => new Date().getFullYear() - i).map(y => ({
              value: String(y), label: `${y} (${new Date().getFullYear() - y} лет)`,
            })),
          ]}
          onChange={v => handleSaveBirthYear(v ? Number(v) : null)}
          onCloseForced={() => setYearPickerProfile(null)}
        />
      )}
      {linkedToken && (
        <RemoteDialog
          title="Устройство привязано"
          message={`Сохраните токен — он нужен для авторизации в плагине. В любой момент его можно посмотреть в разделе устройств.\n\n${linkedToken}`}
          mode="confirm"
          confirmLabel={tokenCopied ? '✓ Скопировано' : 'Копировать токен'}
          cancelLabel="Закрыть"
          onConfirm={copyLinkedToken}
          onCancel={() => setLinkedToken(null)}
        />
      )}
      {dialogEl}
      {genericAlert && (
        <RemoteDialog
          mode="confirm"
          message={genericAlert}
          hideCancel
          confirmLabel="Ок"
          onConfirm={clearGenericAlert}
          onCancel={clearGenericAlert}
        />
      )}
    </Layout>
  )
}
