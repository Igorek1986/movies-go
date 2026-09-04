import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { usePluginSetting } from '@/hooks/usePluginSetting'
// Reuses ProfilesPage's own <details>/<summary>/checkbox styles — see BottomNavSettings.
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'

interface Props {
  // See CardLayoutSettings — bare skips the <details>/<summary> wrapper for
  // ProfilesRemoteView's drill-down screens. Classic omits it.
  bare?: boolean
}

// "Скрывать просмотренное в ленте" — та же per-профильная настройка, что
// numparser_hide_watched в np.js (Lampa), общая на профиль (plugin_settings:
// user_id+profile_id+plugin, не привязана к устройству) — переключил здесь,
// сразу поменялось и в Lampa, и наоборот. Действует на строки Каталога
// (см. useHideWatchedFilter в CatalogPage.tsx), не на «Моё» — там строки уже
// отобраны по статусу, а не по проценту просмотра.
export function HideWatchedSettings({ bare }: Props = {}) {
  const { activeProfile } = useActiveProfile()
  const profileId = activeProfile?.profile_id ?? ''
  const { value: hideWatched, setValue: setHideWatched, loaded } = usePluginSetting<boolean>(
    'np', 'numparser_hide_watched', profileId, false,
  )

  const profileName = activeProfile && activeProfile.profile_id !== '' ? activeProfile.name : 'Основной'

  const body = (
    <>
      <p className={pageStyles.hint}>
        Не показывать в Каталоге фильмы и сериалы, которые уже почти или полностью просмотрены — та же настройка, что в Lampa (общая для профиля «{profileName}» на всех устройствах).
      </p>
      <label className={pageStyles.checkLabel}>
        <input
          type="checkbox"
          data-nav-item
          checked={hideWatched}
          disabled={!loaded}
          onChange={e => setHideWatched(e.target.checked)}
        />
        Скрывать просмотренное в Каталоге
      </label>
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Просмотренное в ленте</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
