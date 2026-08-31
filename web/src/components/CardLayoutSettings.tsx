import { useState } from 'react'
import { CARD_LAYOUTS, getStoredCardLayout, setStoredCardLayout, type CardLayout } from '@/utils/cardLayout'
// Reuses ProfilesPage's own <details>/<summary>/checkbox styles (consistent
// look with BottomNavSettings) and its .positionRow layout for the radio
// row — same pattern as BottomNavSettings' Снизу/Справа/Слева picker.
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import rowStyles from './BottomNavSettings.module.scss'

interface Props {
  // ProfilesRemoteView drills into a dedicated screen per section (its own
  // header/back row) instead of an accordion — bare skips the <details>/
  // <summary> wrapper and renders just the body. Classic omits it entirely,
  // keeping its ordinary <details>.
  bare?: boolean
}

// Per-device setting (localStorage, like theme) — applies instantly, no
// save button: CardDetailPage reads it fresh on every mount.
export function CardLayoutSettings({ bare }: Props = {}) {
  const [layout, setLayout] = useState<CardLayout>(() => getStoredCardLayout())

  function handleChange(next: CardLayout) {
    setLayout(next)
    setStoredCardLayout(next)
  }

  const body = (
    <>
      <p className={pageStyles.hint}>
        Как выглядит страница фильма/сериала — фон на весь экран и блок с описанием внизу (как в мобильном приложении), или классический вид с баннером сверху.
      </p>
      <div className={rowStyles.positionRow} data-row-id="cardLayout-position">
        {CARD_LAYOUTS.map(opt => (
          <label key={opt.id} className={pageStyles.checkLabel}>
            <input type="radio" name="cardLayout" data-nav-item checked={layout === opt.id} onChange={() => handleChange(opt.id)} />
            {opt.label}
          </label>
        ))}
      </div>
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Вид карточки фильма/сериала</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
