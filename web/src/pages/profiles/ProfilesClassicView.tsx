import { useEffect, useState, useRef, useLayoutEffect, useMemo } from 'react'
import Layout from '@/components/Layout'
import PasswordInput from '@/components/PasswordInput'
import { BottomNavSettings } from '@/components/BottomNavSettings'
import { CardLayoutSettings } from '@/components/CardLayoutSettings'
import { BrowseLayoutSettings } from '@/components/BrowseLayoutSettings'
import { SettingsLayoutSettings } from '@/components/SettingsLayoutSettings'
import { PROFILE_ICON_IDS, profileIconSrc } from '@/utils/profileIcon'
import { useProfilesPageState } from './useProfilesPageState'
import styles from './ProfilesClassicView.module.scss'

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

function BirthYearPicker({ current, onSave, onClose }: {
  current: number | null
  onSave: (year: number | null) => void
  onClose: () => void
}) {
  const currentYear = new Date().getFullYear()
  const years = useMemo(() => {
    const arr: number[] = []
    for (let y = currentYear; y >= currentYear - 18; y--) arr.push(y)
    return arr
  }, [currentYear])

  const [selected, setSelected] = useState(current ?? currentYear - 8)
  const scrollRef = useRef<HTMLDivElement>(null)
  const ITEM_H = 40
  const PAD = 2 // padding items top/bottom

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const idx = years.indexOf(selected)
    if (idx === -1) return
    el.scrollTop = (idx + PAD) * ITEM_H - (el.clientHeight / 2 - ITEM_H / 2)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function handleScroll() {
    const el = scrollRef.current
    if (!el) return
    const centerOffset = el.scrollTop + el.clientHeight / 2
    const idx = Math.round((centerOffset / ITEM_H) - PAD - 0.5)
    const clamped = Math.max(0, Math.min(years.length - 1, idx))
    setSelected(years[clamped])
  }

  const padItems = Array(PAD).fill(null)

  return (
    <div className={styles.modalOverlay} onClick={onClose}>
      <div className={styles.yearPickerModal} onClick={e => e.stopPropagation()}>
        <p className={styles.yearPickerTitle}>Год рождения</p>
        <div className={styles.yearDrum}>
          <div className={styles.yearDrumScroll} ref={scrollRef} onScroll={handleScroll}>
            {padItems.map((_, i) => <div key={`t${i}`} style={{ height: ITEM_H }} />)}
            {years.map(y => (
              <div
                key={y}
                className={`${styles.yearDrumItem} ${y === selected ? styles.yearDrumItemSelected : ''}`}
                onClick={() => {
                  setSelected(y)
                  const el = scrollRef.current
                  if (!el) return
                  const idx = years.indexOf(y)
                  el.scrollTo({ top: (idx + PAD) * ITEM_H - (el.clientHeight / 2 - ITEM_H / 2), behavior: 'smooth' })
                }}
              >
                {y} ({currentYear - y} лет)
              </div>
            ))}
            {padItems.map((_, i) => <div key={`b${i}`} style={{ height: ITEM_H }} />)}
          </div>
          <div className={styles.yearDrumHighlight} />
          <div className={styles.yearDrumShadowTop} />
          <div className={styles.yearDrumShadowBottom} />
        </div>
        <div className={styles.yearPickerActions}>
          <button className={styles.btnPrimary} style={{ flex: 1 }} onClick={() => onSave(selected)}>
            Сохранить
          </button>
          {current && (
            <button className={`${styles.btnSm} ${styles.danger}`} onClick={() => onSave(null)}>
              Убрать
            </button>
          )}
          <button className={styles.btnSm} onClick={onClose}>Отмена</button>
        </div>
      </div>
    </div>
  )
}

export default function ProfilesClassicView() {
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
  } = useProfilesPageState()

  // genericAlert replaces the old inline alert(...) calls (see the hook) so
  // Remote can show them as text instead — Classic just turns it straight
  // back into a real window.alert(), same UX as before this split.
  useEffect(() => {
    if (genericAlert) { window.alert(genericAlert); clearGenericAlert() }
  }, [genericAlert]) // eslint-disable-line react-hooks/exhaustive-deps

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
                    <button className={styles.btnSm} onClick={() => openDevicePlugins(d.id)}>
                      Плагины{openPluginsFor === d.id ? ' ▲' : ' ▼'}
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
                            {p.child && (
                              <button
                                className={styles.btnSm}
                                onClick={() => handleSetBirthYear(p)}
                                title="Год рождения ребёнка для ограничения контента по возрасту"
                              >
                                {p.child_birth_year ? `${p.child_birth_year} (${new Date().getFullYear() - p.child_birth_year} лет)` : 'Год рождения'}
                              </button>
                            )}
                            <button className={styles.btnSm} onClick={() => handleEditParams(p)}>
                              Параметры
                            </button>
                            <button className={styles.btnSm} onClick={() => openProfilePlugins(p.profile_id)}>
                              Плагины{profilePluginsFor === p.profile_id ? ' ▲' : ' ▼'}
                            </button>
                          </div>
                          {profilePluginsFor === p.profile_id && (
                            <div className={styles.profilesPanel}>
                              {profilePlugins.length === 0 && (
                                <p className={styles.empty}>Нет плагинов устройства — добавьте их в разделе «Плагины» устройства</p>
                              )}
                              {profilePlugins.map(pp => {
                                const effective = pp.override ?? pp.device_enabled
                                return (
                                  <div key={pp.url} style={{
                                    display: 'flex', flexDirection: 'column', gap: '4px',
                                    border: '1px solid var(--color-border)', borderRadius: '6px', padding: '6px 10px',
                                  }}>
                                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', flexWrap: 'wrap' }}>
                                      <span style={{ fontSize: '0.82rem', fontWeight: 600 }}>
                                        {pp.name || (pp.in_device_list ? pp.url : 'Без названия')}
                                        {!pp.in_device_list && (
                                          <span style={{ fontWeight: 400, color: 'var(--color-text-muted)' }}> (только для профиля)</span>
                                        )}
                                      </span>
                                      <div style={{ display: 'flex', gap: '6px', flexShrink: 0 }}>
                                        <button
                                          className={`${styles.btnSm} ${effective ? styles.active : ''}`}
                                          onClick={() => handleSetProfileOverride(pp.url, !effective)}
                                        >
                                          {effective ? 'Вкл ✓' : 'Выкл'}
                                        </button>
                                        {!pp.in_device_list && (
                                          <>
                                            <button className={styles.btnIcon} title="Переименовать" onClick={() => handleRenameProfilePlugin(pp)}>✏️</button>
                                            <button className={styles.btnIcon} title="Изменить URL" onClick={() => handleEditProfilePluginUrl(pp)}>🔗</button>
                                          </>
                                        )}
                                        {pp.override !== null && pp.in_device_list && (
                                          <button className={styles.btnSm} onClick={() => handleClearProfileOverride(pp.url)}>
                                            Сброс
                                          </button>
                                        )}
                                        {!pp.in_device_list && (
                                          <button className={`${styles.btnSm} ${styles.danger}`} onClick={() => handleClearProfileOverride(pp.url)}>
                                            Удалить
                                          </button>
                                        )}
                                      </div>
                                    </div>
                                    {!pp.in_device_list && (
                                      <code
                                        style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', wordBreak: 'break-all', cursor: 'pointer' }}
                                        title="Нажмите, чтобы изменить URL"
                                        onClick={() => handleEditProfilePluginUrl(pp)}
                                      >
                                        {pp.url}
                                      </code>
                                    )}
                                  </div>
                                )
                              })}
                              <form
                                className={styles.formRow}
                                onSubmit={async e => {
                                  e.preventDefault()
                                  const url = newProfilePluginUrl.trim()
                                  if (!url) return
                                  setProfilePluginError('')
                                  const err = await handleSetProfileOverride(url, true, newProfilePluginName.trim())
                                  if (err) { setProfilePluginError(err); return }
                                  setNewProfilePluginUrl('')
                                  setNewProfilePluginName('')
                                }}
                              >
                                <input
                                  className={styles.input}
                                  placeholder="Доп. URL плагина только для этого профиля"
                                  value={newProfilePluginUrl}
                                  onChange={e => setNewProfilePluginUrl(e.target.value)}
                                />
                                <input
                                  className={styles.input}
                                  placeholder="Название (необязательно)"
                                  value={newProfilePluginName}
                                  onChange={e => setNewProfilePluginName(e.target.value)}
                                  style={{ maxWidth: '180px' }}
                                />
                                <button className={styles.btnSm} type="submit">Добавить</button>
                              </form>
                              {profilePluginError && <p className={styles.errorText}>{profilePluginError}</p>}
                            </div>
                          )}
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

                  {openPluginsFor === d.id && (
                    <div className={styles.profilesPanel}>
                      <h4 className={styles.profilesTitle}>Плагины Lampa</h4>
                      <p className={styles.hint}>
                        Загружаются в Lampa через bootstrap-модуль (lampainit-invc.js) на этом устройстве. Изменения применяются без перезапуска Lampac.
                      </p>
                      {devicePlugins.length === 0 && <p className={styles.empty}>Плагинов ещё нет</p>}
                      {devicePlugins.map(p => (
                        <div key={p.id} className={styles.profileCard}>
                          <div className={styles.profileCardTop}>
                            <div className={styles.profileCardLeft}>
                              <strong className={styles.profileName}>{p.name || 'Без названия'}</strong>
                              <div className={styles.profileMeta}>
                                <code
                                  className={styles.profileId}
                                  style={{ wordBreak: 'break-all', cursor: 'pointer' }}
                                  title="Нажмите, чтобы изменить URL"
                                  onClick={() => handleEditPluginUrl(p)}
                                >
                                  {p.url}
                                </code>
                              </div>
                            </div>
                            <div className={styles.profileCardActions}>
                              <button
                                className={`${styles.btnSm} ${p.enabled ? styles.active : ''}`}
                                onClick={() => handleToggleDevicePlugin(p)}
                              >
                                {p.enabled ? 'Включён ✓' : 'Выключен'}
                              </button>
                              <button className={styles.btnIcon} title="Переименовать" onClick={() => handleRenamePlugin(p)}>✏️</button>
                              <button className={styles.btnIcon} title="Изменить URL" onClick={() => handleEditPluginUrl(p)}>🔗</button>
                              <button className={`${styles.btnSm} ${styles.danger}`} onClick={() => handleDeletePlugin(p)}>Удалить</button>
                            </div>
                          </div>
                        </div>
                      ))}
                      {pluginError && <p className={styles.errorText}>{pluginError}</p>}
                      <form className={`${styles.formCol} ${styles.newProfileForm}`} onSubmit={handleAddPlugin}>
                        <input
                          className={styles.input}
                          placeholder="URL плагина (https://...)"
                          value={newPluginUrl}
                          onChange={e => setNewPluginUrl(e.target.value)}
                          required
                        />
                        <input
                          className={styles.input}
                          placeholder="Название (необязательно)"
                          value={newPluginName}
                          onChange={e => setNewPluginName(e.target.value)}
                          maxLength={100}
                        />
                        <button className={styles.btnPrimary} type="submit">Добавить плагин</button>
                      </form>
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
                      <p className={styles.hint}>Отправьте @{tgCode.link.split('t.me/')[1]?.split('?')[0]} команду:</p>
                      <code
                        className={styles.tgCode}
                        title="Нажмите чтобы скопировать"
                        onClick={() => {
                          navigator.clipboard.writeText(`/start ${tgCode.code}`).catch(() => {})
                          setTgCodeCopied(true)
                          setTimeout(() => setTgCodeCopied(false), 1500)
                        }}
                      >
                        /start {tgCode.code}
                        {tgCodeCopied && <span className={styles.copiedHint}> Скопировано!</span>}
                      </code>
                      <p className={styles.hint} style={{ marginTop: 8 }}>Или откройте ссылку:</p>
                      <a href={tgCode.link} target="_blank" rel="noreferrer" className={styles.tgBtn}>
                        Открыть в Telegram
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

          <BottomNavSettings />
          <CardLayoutSettings />
          <BrowseLayoutSettings />
          <SettingsLayoutSettings />

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

      {yearPickerProfile && (
        <BirthYearPicker
          current={yearPickerProfile.child_birth_year ?? null}
          onSave={handleSaveBirthYear}
          onClose={() => setYearPickerProfile(null)}
        />
      )}
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
