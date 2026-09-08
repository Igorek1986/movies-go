import { useState } from 'react'
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import styles from './ExtensionsSection.module.scss'
import type { UseExtensionsResult, WebExtension } from '@/hooks/useExtensions'
import { ExtensionHost } from './ExtensionHost'

interface Props {
  // Один useExtensions() на страницу (ProfilesClassicView/ProfilesRemoteView),
  // прокидывается сюда пропом — а не вызывается заново внутри компонента.
  // ProfilesRemoteView строит из этого же списка ещё и пункты корневого
  // меню (см. ExtensionHost-экраны `ext:${id}`); два независимых вызова
  // useExtensions() в одной вкладке не синхронизируются между собой сразу
  // после add/update/remove — WS-рассылка extensions_changed нарочно не
  // долетает до вкладки-источника изменения (exceptClientID в ws.Hub), а
  // обновляется только тот вызов хука, который сам сделал fetch.
  state: UseExtensionsResult
  // ProfilesRemoteView (TV drill-down) skips the <details>/<summary>
  // wrapper — same convention as CardLayoutSettings/HideWatchedSettings etc.
  bare?: boolean
  // ProfilesRemoteView renders each enabled extension as its own top-level
  // menu entry/screen (see ProfilesRemoteView.tsx `ext:${id}` sections)
  // instead of nesting them inside this management screen — pass false
  // there to skip rendering them here and avoid double-mounting the same
  // iframe. ProfilesClassicView keeps the default (true): its flat
  // <details> list already renders each extension as its own sibling block,
  // right where a user expects a new "Настройки" item to show up.
  renderHosts?: boolean
}

function StatusBadge({ ext }: { ext: WebExtension }) {
  if (ext.status === 'ok') {
    return <span className={`${styles.badge} ${styles.badgeOk}`}>{ext.status_code ?? 'ok'}</span>
  }
  if (ext.status === 'unreachable') {
    return <span className={`${styles.badge} ${styles.badgeBad}`}>{ext.status_code ? `HTTP ${ext.status_code}` : 'недоступно'}</span>
  }
  if (ext.status === 'invalid') {
    return <span className={`${styles.badge} ${styles.badgeWarn}`}>некорректный файл</span>
  }
  return <span className={`${styles.badge} ${styles.badgeUnknown}`}>не проверено</span>
}

// «Расширения» — управление списком (добавление по ссылке, вкл/выкл,
// проверка статуса, удаление) + сразу под ним рендер включённых расширений
// (см. ExtensionHost) — единая точка, куда вставляются пункты меню
// Настроек, приходящие из расширений.
export function ExtensionsSection({ state, bare, renderHosts = true }: Props) {
  const { extensions, add, update, remove, check } = state
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [checkingId, setCheckingId] = useState<number | null>(null)

  async function handleAdd(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await add(url.trim(), name.trim())
      setUrl('')
      setName('')
    } catch (err) {
      setError(String((err as Error).message || err))
    }
    setBusy(false)
  }

  async function handleCheck(ext: WebExtension) {
    setCheckingId(ext.id)
    try { await check(ext.id) } catch { /* статус останется прежним, ошибку видно по бейджу */ }
    setCheckingId(null)
  }

  const body = (
    <>
      <p className={pageStyles.hint}>
        Подключаемые по ссылке скрипты — включены/выключены сразу на всех устройствах и вкладках под этим логином.
      </p>

      {extensions.length === 0 && <p className={pageStyles.empty}>Расширений ещё нет</p>}
      {extensions.map(ext => (
        <div key={ext.id} className={pageStyles.profileCard} data-row-id={`ext-${ext.id}`}>
          <div className={pageStyles.profileCardTop}>
            <div className={pageStyles.profileCardLeft}>
              <strong className={pageStyles.profileName}>{ext.name || 'Без названия'}</strong>
              <div className={pageStyles.profileMeta}>
                <code className={pageStyles.profileId} style={{ wordBreak: 'break-all' }}>{ext.url}</code>
                <StatusBadge ext={ext} />
              </div>
            </div>
            <div className={pageStyles.profileCardActions}>
              <button
                className={`${pageStyles.btnSm} ${ext.enabled ? pageStyles.active : ''}`}
                data-nav-item
                onClick={() => update(ext.id, { enabled: !ext.enabled })}
              >
                {ext.enabled ? 'Включено ✓' : 'Выключено'}
              </button>
              <button className={pageStyles.btnIcon} data-nav-item title="Проверить" disabled={checkingId === ext.id} onClick={() => handleCheck(ext)}>
                {checkingId === ext.id ? '…' : '↻'}
              </button>
              <button className={`${pageStyles.btnSm} ${pageStyles.danger}`} data-nav-item onClick={() => remove(ext.id)}>Удалить</button>
            </div>
          </div>
        </div>
      ))}

      {error && <p className={pageStyles.errorText}>{error}</p>}
      <form className={`${pageStyles.formCol} ${pageStyles.newProfileForm}`} onSubmit={handleAdd}>
        <input
          className={pageStyles.input}
          data-nav-item
          placeholder="Ссылка на расширение (https://... или /file.js)"
          value={url}
          onChange={e => setUrl(e.target.value)}
          required
        />
        <input
          className={pageStyles.input}
          data-nav-item
          placeholder="Название (необязательно)"
          value={name}
          onChange={e => setName(e.target.value)}
          maxLength={200}
        />
        <button className={pageStyles.btnPrimary} data-nav-item type="submit" disabled={busy}>
          {busy ? 'Добавление…' : 'Добавить расширение'}
        </button>
      </form>
    </>
  )

  const hosts = renderHosts
    ? extensions.filter(e => e.enabled).map(ext => <ExtensionHost key={ext.id} extension={ext} bare={bare} />)
    : null

  if (bare) {
    return (
      <>
        {body}
        {hosts}
      </>
    )
  }

  return (
    <>
      <details className={pageStyles.details}>
        <summary className={pageStyles.summary}>Расширения</summary>
        <div className={pageStyles.detailsBody}>{body}</div>
      </details>
      {hosts}
    </>
  )
}
