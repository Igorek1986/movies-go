import { useState } from 'react'
import { SETTINGS_LAYOUTS, getStoredSettingsLayout, setStoredSettingsLayout, type SettingsLayout } from '@/utils/settingsLayout'
// Reuses ProfilesPage's own <details>/<summary>/checkbox styles, like
// CardLayoutSettings/BrowseLayoutSettings — rendered in BOTH views (Classic
// and Remote) so switching to 'remote' always leaves a way back.
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import rowStyles from './BottomNavSettings.module.scss'

interface Props {
  // See CardLayoutSettings — bare skips the <details>/<summary> wrapper for
  // ProfilesRemoteView's drill-down screens. Classic omits it.
  bare?: boolean
}

// Per-device setting (localStorage, like cardLayout/browseLayout) — applies
// instantly, no save button, takes effect on this page's next mount.
export function SettingsLayoutSettings({ bare }: Props = {}) {
  const [layout, setLayout] = useState<SettingsLayout>(() => getStoredSettingsLayout())

  function handleChange(next: SettingsLayout) {
    setLayout(next)
    setStoredSettingsLayout(next)
    window.location.reload()
  }

  const body = (
    <>
      <p className={pageStyles.hint}>
        Классический вид (как сейчас) или вид, заточенный под управление с клавиатуры/пульта — список разделов, выбор проваливается в отдельный экран.
      </p>
      <div className={rowStyles.positionRow} data-row-id="settingsLayout-position">
        {SETTINGS_LAYOUTS.map(opt => (
          <label key={opt.id} className={pageStyles.checkLabel}>
            <input type="radio" name="settingsLayout" data-nav-item checked={layout === opt.id} onChange={() => handleChange(opt.id)} />
            {opt.label}
          </label>
        ))}
      </div>
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Дизайн страницы «Настройки»</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
