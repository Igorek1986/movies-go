import { useCallback, useMemo } from 'react'
import { usePluginSetting } from './usePluginSetting'

// np.js's own getAllCategories() names year categories 'year_YYYY'; our own
// /api/categories (and the actual fetch route, movies_id_{year} — see
// handleCategory) names the same conceptual category 'movies_id_YYYY'. Both
// clients write the same shared numparser_menu_sort/numparser_menu_hide, so
// without translating at this boundary, hiding "Фильмы 2020 года" on one
// client saved an id the other didn't recognize and never actually hid.
function toSharedId(id: string): string {
  return id.startsWith('movies_id_') ? 'year_' + id.slice('movies_id_'.length) : id
}
function fromSharedId(id: string): string {
  return id.startsWith('year_') ? 'movies_id_' + id.slice('year_'.length) : id
}

// Тот же numparser_menu_sort/numparser_menu_hide, что в np.js (Lampa) —
// порядок и видимость строк Каталога, общие на профиль, применяются
// одинаково в обоих режимах отображения (Hero и Classic), см.
// applyMenuOrder в utils/catalogCategories.ts. Callers work purely with
// наши собственные id категорий (movies_id_YYYY) — перевод в/из id Lampa
// (year_YYYY) происходит здесь же, прозрачно.
export function useMenuOrder(profileId: string) {
  const order = usePluginSetting<string[]>('np', 'numparser_menu_sort', profileId, [])
  const hidden = usePluginSetting<string[]>('np', 'numparser_menu_hide', profileId, [])

  // order.value/hidden.value are only a new array reference when the
  // underlying setting actually changes (plain useState in usePluginSetting)
  // — mapping them on every call (without memoizing on that reference) made
  // every one of THESE new arrays instead, which fed CatalogPage's
  // [menuOrder, menuHidden] effect dependency a "changed" value on every
  // single render, looping setCategories → re-render → "changed" again
  // forever and locking up the whole page (Enter/click stopped registering).
  const orderIds = useMemo(() => order.value.map(fromSharedId), [order.value])
  const hiddenIds = useMemo(() => hidden.value.map(fromSharedId), [hidden.value])
  const setOrder = useCallback((ids: string[]) => order.setValue(ids.map(toSharedId)), [order.setValue])
  const setHidden = useCallback((ids: string[]) => hidden.setValue(ids.map(toSharedId)), [hidden.setValue])

  return {
    order: orderIds, setOrder, orderLoaded: order.loaded,
    hidden: hiddenIds, setHidden, hiddenLoaded: hidden.loaded,
  }
}
