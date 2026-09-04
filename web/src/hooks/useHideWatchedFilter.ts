import { usePluginSetting } from './usePluginSetting'

// Те же ключи/значение по умолчанию, что и в np.js (DEFAULT_MIN_PROGRESS=90,
// numparser_hide_watched выключен по умолчанию) — общая настройка на профиль,
// см. usePluginSetting.
const DEFAULT_MIN_PROGRESS = 90

export function useHideWatchedFilter(profileId: string) {
  const hideWatched = usePluginSetting<boolean>('np', 'numparser_hide_watched', profileId, false)
  const minProgress = usePluginSetting<number>('np', 'numparser_min_progress', profileId, DEFAULT_MIN_PROGRESS)
  return {
    hideWatched: hideWatched.value,
    setHideWatched: hideWatched.setValue,
    hideWatchedLoaded: hideWatched.loaded,
    minProgress: minProgress.value,
  }
}

// Добавляет hide_watched/percent в запрос категории — те же query-параметры,
// что шлёт np.js (Api.list, см. plugins/np.js), и та же обработка на бэкенде
// (applyHideWatched в internal/api/content.go): фильтрует по проценту
// просмотра уже отрисованные карточки, ничего специфичного для веба здесь нет.
export function applyHideWatchedParams(params: URLSearchParams, hideWatched: boolean, minProgress: number): void {
  if (!hideWatched) return
  params.set('hide_watched', '1')
  params.set('percent', String(minProgress))
}
