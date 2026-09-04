import { usePluginSetting } from './usePluginSetting'

// Те же 7 вариантов, тот же ключ и тот же plugin ('np_unwatched', не 'np'!),
// что и SORT_KEY/DEFAULT_SORT в np_unwatched.js — общая per-профильная
// настройка, синхронизируется через тот же plugin_settings, что и Lampa.
export const UNWATCHED_SORT_DEFAULT = 'progress'

export const UNWATCHED_SORT_OPTIONS: { value: string; label: string }[] = [
  { value: 'progress', label: 'По прогрессу' },
  { value: 'unwatched_count', label: 'По количеству непросмотренных' },
  { value: 'air_date', label: 'По дате последнего эпизода ↓' },
  { value: 'air_date_asc', label: 'По дате последнего эпизода ↑' },
  { value: 'first_unwatched_date', label: 'По дате первого непросмотренного ↓' },
  { value: 'first_unwatched_date_asc', label: 'По дате первого непросмотренного ↑' },
  { value: 'alphabet', label: 'По алфавиту' },
]

export function useUnwatchedSort(profileId: string) {
  const { value, setValue, loaded } = usePluginSetting<string>(
    'np_unwatched', 'np_unwatched_sort_order', profileId, UNWATCHED_SORT_DEFAULT,
  )
  return { unwatchedSort: value, setUnwatchedSort: setValue, unwatchedSortLoaded: loaded }
}
