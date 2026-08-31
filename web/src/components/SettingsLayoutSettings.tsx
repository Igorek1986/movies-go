import { useEffect, useState } from 'react'
import { useAuth } from '@/hooks/useAuth'
import { SETTINGS_LAYOUTS, resolveSettingsLayout, saveSettingsLayout, type SettingsLayout } from '@/utils/settingsLayout'
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

// Per-account setting (server, see users.settings_layout) — applies on this
// page's next mount, via a full reload (so the fresh value round-trips
// through /api/me instead of needing a live-update event like BottomNav).
export function SettingsLayoutSettings({ bare }: Props = {}) {
  const { user } = useAuth()
  const [layout, setLayout] = useState<SettingsLayout>('remote')
  useEffect(() => {
    if (user) setLayout(resolveSettingsLayout(user.settings_layout))
  }, [user])

  async function handleChange(next: SettingsLayout) {
    setLayout(next)
    await saveSettingsLayout(next)
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
