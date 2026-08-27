import { useState } from 'react'
import { BROWSE_LAYOUTS, getStoredBrowseLayout, setStoredBrowseLayout, type BrowseLayout } from '@/utils/browseLayout'
import pageStyles from '@/pages/ProfilesPage.module.scss'
import rowStyles from './BottomNavSettings.module.scss'

// Per-device setting (localStorage, like cardLayout) — applies instantly, no
// save button: CatalogPage/MediaLibraryPage read it fresh on every mount.
export function BrowseLayoutSettings() {
  const [layout, setLayout] = useState<BrowseLayout>(() => getStoredBrowseLayout())

  function handleChange(next: BrowseLayout) {
    setLayout(next)
    setStoredBrowseLayout(next)
  }

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Вид Каталога и Моё</summary>
      <div className={pageStyles.detailsBody}>
        <p className={pageStyles.hint}>
          Как выглядят разделы «Каталог» и «Моё» — фон активной карточки с описанием сверху рядов, или обычный вид рядами карточек без фона.
        </p>
        <div className={rowStyles.positionRow}>
          {BROWSE_LAYOUTS.map(opt => (
            <label key={opt.id} className={pageStyles.checkLabel}>
              <input type="radio" name="browseLayout" checked={layout === opt.id} onChange={() => handleChange(opt.id)} />
              {opt.label}
            </label>
          ))}
        </div>
      </div>
    </details>
  )
}
