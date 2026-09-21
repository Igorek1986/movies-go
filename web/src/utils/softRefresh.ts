import { useSyncExternalStore } from 'react'
import { flushSync } from 'react-dom'
import { invalidateAllCatalogRows, invalidateUnwatchedRow } from '@/pages/CatalogPage'
import { invalidateLibraryRows } from '@/pages/MediaLibraryPage'
import { invalidateHistoryCache } from '@/pages/HistoryPage'
import { ADMIN_STATS_CACHE_KEY } from '@/utils/adminStatsCache'
import { findInPlaceRefresh } from '@/utils/inPlaceRefresh'
import '@/utils/networkActivity' // начинает считать запросы уже с загрузки приложения

// «Мягкое» обновление для pull-to-refresh: вместо window.location.reload()
// (пустой экран, повторный разбор JS, страница собирается с нуля — отсюда
// мигание) сбрасываем кеши и ремаунтим текущую страницу. Ряды Каталога
// помечаются stale — CategoryRow показывает прежние данные сразу и тихо
// подменяет их свежими, без «Загрузка…» (см. RowCache.stale в CatalogPage).

let version = 0
const listeners = new Set<() => void>()

function subscribe(cb: () => void) {
  listeners.add(cb)
  return () => { listeners.delete(cb) }
}

// Ключ для <Outlet key=…> приватных страниц (App.tsx): смена ключа = ремаунт.
export function useSoftRefreshKey(): number {
  return useSyncExternalStore(subscribe, () => version)
}

// Страница может обновиться «на месте» (см. inPlaceRefresh.ts) — тогда ремаунта
// нет вовсе, и PullToRefresh не притушает контент.
export function canRefreshInPlace(): boolean {
  return findInPlaceRefresh() !== null
}

// Возвращает promise, который завершается, когда свежие данные уже на экране
// (для ремаунта — сразу: страница сама подтянет данные под спиннером).
//
// flushSync: ремаунт должен закоммититься в этом же тике — PullToRefresh сразу
// после него ищет новый <main> и возвращает ему текущий сдвиг, иначе между
// коммитом и следующим кадром страница дёрнулась бы вверх.
export function softRefresh(): Promise<void> {
  try { localStorage.removeItem(ADMIN_STATS_CACHE_KEY) } catch { /* кеш — best-effort */ }
  const inPlace = findInPlaceRefresh()
  if (inPlace) return inPlace.refresh()
  invalidateAllCatalogRows()
  invalidateUnwatchedRow()
  invalidateLibraryRows()
  invalidateHistoryCache()
  version++
  flushSync(() => { listeners.forEach(l => l()) })
  return Promise.resolve()
}
