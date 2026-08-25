import { useState } from 'react'
import { CARD_LAYOUTS, getStoredCardLayout, setStoredCardLayout, type CardLayout } from '@/utils/cardLayout'
// Reuses ProfilesPage's own <details>/<summary>/checkbox styles (consistent
// look with BottomNavSettings) and its .positionRow layout for the radio
// row — same pattern as BottomNavSettings' Снизу/Справа/Слева picker.
import pageStyles from '@/pages/ProfilesPage.module.scss'
import rowStyles from './BottomNavSettings.module.scss'

// Per-device setting (localStorage, like theme) — applies instantly, no
// save button: CardDetailPage reads it fresh on every mount.
export function CardLayoutSettings() {
  const [layout, setLayout] = useState<CardLayout>(() => getStoredCardLayout())

  function handleChange(next: CardLayout) {
    setLayout(next)
    setStoredCardLayout(next)
  }

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Вид карточки фильма/сериала</summary>
      <div className={pageStyles.detailsBody}>
        <p className={pageStyles.hint}>
          Как выглядит страница фильма/сериала — фон на весь экран и блок с описанием внизу (как в мобильном приложении), или классический вид с баннером сверху.
        </p>
        <div className={rowStyles.positionRow}>
          {CARD_LAYOUTS.map(opt => (
            <label key={opt.id} className={pageStyles.checkLabel}>
              <input type="radio" name="cardLayout" checked={layout === opt.id} onChange={() => handleChange(opt.id)} />
              {opt.label}
            </label>
          ))}
        </div>
      </div>
    </details>
  )
}
