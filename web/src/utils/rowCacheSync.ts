import { invalidateUnwatchedRow, invalidateAllCatalogRows } from '@/pages/CatalogPage'
import { invalidateLibraryRows } from '@/pages/MediaLibraryPage'

// Инвалидация построчных кешей Каталога/Моё после изменения статуса/таймкода/
// избранного — общая точка для CardDetailPage (тот же браузер) и useLiveSync
// (WS-событие с другого устройства/вкладки). CatalogPage/MediaLibraryPage не
// ленивые (App.tsx импортирует их статически), поэтому обычный import не
// портит code-splitting — динамический import() здесь не нужен.
export function invalidateRowCachesAfterStatusChange(): void {
  invalidateUnwatchedRow()
  invalidateLibraryRows()
}

// То же самое, плюс полный сброс ВСЕХ построчных кешей Каталога — см.
// invalidateAllCatalogRows на CatalogPage.tsx за тем, почему это отдельная,
// более тяжёлая функция, а не просто расширение invalidateRowCachesAfterStatusChange
// (её же вызывает частый WS-путь, где полный сброс был бы избыточен). Только
// для локальных действий на CardDetailPage.
export function invalidateCatalogRowsForWatchedFilter(): void {
  invalidateRowCachesAfterStatusChange()
  invalidateAllCatalogRows()
}
