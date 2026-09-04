import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { useUnwatchedSort, UNWATCHED_SORT_OPTIONS } from '@/hooks/useUnwatchedSort'
// Reuses ProfilesPage's own <details>/<summary>/radio styles — see HideWatchedSettings.
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'

interface Props {
  // See CardLayoutSettings — bare skips the <details>/<summary> wrapper for
  // ProfilesRemoteView's drill-down screens. Classic omits it.
  bare?: boolean
}

// "Сортировка «Непросмотренные»" — та же per-профильная настройка, что
// np_unwatched_sort_order в np_unwatched.js (Lampa; plugin 'np_unwatched',
// не 'np' — отдельная строка plugin_settings), общая на профиль. Действует
// только на категорию "unwatched" (см. useUnwatchedSort в CatalogPage.tsx).
export function UnwatchedSortSettings({ bare }: Props = {}) {
  const { activeProfile } = useActiveProfile()
  const profileId = activeProfile?.profile_id ?? ''
  const { unwatchedSort, setUnwatchedSort, unwatchedSortLoaded: loaded } = useUnwatchedSort(profileId)

  const profileName = activeProfile && activeProfile.profile_id !== '' ? activeProfile.name : 'Основной'

  const body = (
    <>
      <p className={pageStyles.hint}>
        Порядок показа сериалов в «Непросмотренных» (общая для профиля «{profileName}» на всех устройствах).
      </p>
      {UNWATCHED_SORT_OPTIONS.map(opt => (
        <label key={opt.value} className={pageStyles.checkLabel}>
          <input
            type="radio"
            name="unwatchedSort"
            data-nav-item
            checked={unwatchedSort === opt.value}
            disabled={!loaded}
            onChange={() => setUnwatchedSort(opt.value)}
          />
          {opt.label}
        </label>
      ))}
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Сортировка «Непросмотренные»</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
