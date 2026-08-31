import { useEffect, useState } from 'react'
import { useAuth } from '@/hooks/useAuth'
import { BROWSE_LAYOUTS, resolveBrowseLayout, saveBrowseLayout, type BrowseLayout } from '@/utils/browseLayout'
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import rowStyles from './BottomNavSettings.module.scss'

interface Props {
  // See CardLayoutSettings — ProfilesRemoteView drills into its own screen
  // per section instead of an accordion; bare skips the <details>/<summary>
  // wrapper. Classic omits it, keeping its ordinary <details>.
  bare?: boolean
}

// Per-account setting (server, see users.browse_layout) — applies instantly,
// no save button: CatalogPage/MediaLibraryPage read it fresh (via their own
// useAuth() call) on every mount.
export function BrowseLayoutSettings({ bare }: Props = {}) {
  const { user } = useAuth()
  const [layout, setLayout] = useState<BrowseLayout>('hero')
  useEffect(() => {
    if (user) setLayout(resolveBrowseLayout(user.browse_layout))
  }, [user])

  function handleChange(next: BrowseLayout) {
    setLayout(next)
    saveBrowseLayout(next)
  }

  const body = (
    <>
      <p className={pageStyles.hint}>
        Как выглядят разделы «Каталог» и «Моё» — фон активной карточки с описанием сверху рядов, или обычный вид рядами карточек без фона.
      </p>
      <div className={rowStyles.positionRow} data-row-id="browseLayout-position">
        {BROWSE_LAYOUTS.map(opt => (
          <label key={opt.id} className={pageStyles.checkLabel}>
            <input type="radio" name="browseLayout" data-nav-item checked={layout === opt.id} onChange={() => handleChange(opt.id)} />
            {opt.label}
          </label>
        ))}
      </div>
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Вид Каталога и Моё</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
