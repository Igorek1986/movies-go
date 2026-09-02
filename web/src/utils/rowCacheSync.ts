import { invalidateUnwatchedRow } from '@/pages/CatalogPage'
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
