// Shared by CatalogPage.tsx (actual row order/visibility) and
// MenuOrderSettings.tsx (the management screen) — same shuffle/grouping
// rules np.js applies for Lampa, so "как в Lampa" holds for both clients.

import { applyHideWatchedParams } from '@/hooks/useHideWatchedFilter'

export interface CatalogCategory {
  id: string
  name: string
}

export interface CategoryPageResult<T> {
  items: T[]
  totalPages: number
}

// Fetches page 1 of a category row — the exact same request CategoryRow's
// own loadItems makes, factored out so CatalogPage's row-ahead prefetch (see
// its own comment) can populate _cache.rows for a category BEFORE the user
// ever navigates to it, without duplicating this URL-building logic.
export async function fetchCategoryPage<T>(
  categoryId: string,
  opts: { token: string; profileId: string; hideWatched: boolean; hidePercent: number; unwatchedSort?: string },
): Promise<CategoryPageResult<T>> {
  const params = new URLSearchParams({ per_page: '20', page: '1' })
  if (opts.token && opts.profileId != null) {
    params.set('token', opts.token)
    params.set('profile_id', opts.profileId)
    applyHideWatchedParams(params, opts.hideWatched, opts.hidePercent)
  }
  if (categoryId === 'unwatched' && opts.unwatchedSort) params.set('sort', opts.unwatchedSort)
  const res = await fetch(`/${encodeURIComponent(categoryId)}?${params}`)
  if (!res.ok) throw new Error('HTTP ' + res.status)
  const data = await res.json()
  return { items: data.results || [], totalPages: data.total_pages || 1 }
}

export function shuffleArray<T>(arr: T[]): T[] {
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

// genre_*/actor_*/director_* are a randomly-shuffled pool whose actor/director
// ids change on every /api/categories request (see handleAPICategories) —
// they can't be individually targeted by a saved order/hide list. np.js's own
// menu editor already treats the whole pool as one entry ('collections_block'
// in getAllCategories) for exactly this reason; mirrored here so the same
// saved numparser_menu_sort/numparser_menu_hide values mean the same thing
// on both clients.
export const COLLECTIONS_BLOCK_ID = 'collections_block'
export const COLLECTIONS_BLOCK_NAME = 'Подборки'

export function isCollectionsBlockMember(id: string): boolean {
  return id.startsWith('genre_') || id.startsWith('actor_') || id.startsWith('director_')
}

// Fetches the raw category list (same endpoint np.js reads for Lampa) and
// prepends the synthetic "Непросмотренные" entry — a local-only category
// (see handleUnwatched) that never goes through /api/categories.
export async function fetchCatalogCategories(): Promise<CatalogCategory[]> {
  const res = await fetch('/api/categories')
  if (!res.ok) return []
  const cats: CatalogCategory[] = await res.json()
  return [{ id: 'unwatched', name: 'Непросмотренные' }, ...cats]
}

// Collapses the genre_*/actor_*/director_* run into one "Подборки" entry —
// for UIs that manage order/visibility (MenuOrderSettings), not for actual
// row rendering (that needs every real category — see applyMenuOrder).
export function collapseCollectionsBlock(categories: CatalogCategory[]): CatalogCategory[] {
  const result: CatalogCategory[] = []
  let inserted = false
  for (const c of categories) {
    if (isCollectionsBlockMember(c.id)) {
      if (!inserted) { result.push({ id: COLLECTIONS_BLOCK_ID, name: COLLECTIONS_BLOCK_NAME }); inserted = true }
      continue
    }
    result.push(c)
  }
  return result
}

// Applies the saved numparser_menu_sort/numparser_menu_hide (same keys/
// values np.js syncs for Lampa) to the real, uncollapsed category list.
// shuffledBlock is the ALREADY-shuffled genre/actor/director pool for this
// page load (see CatalogPage's _cache.shuffledBlock) — passed in rather than
// shuffled here so re-applying this (e.g. when order/hidden change) doesn't
// re-shuffle rows the user never asked to reshuffle.
export function applyMenuOrder(
  categories: CatalogCategory[],
  shuffledBlock: CatalogCategory[],
  order: string[],
  hidden: string[],
): CatalogCategory[] {
  const hiddenSet = new Set(hidden)
  const blockHidden = hiddenSet.has(COLLECTIONS_BLOCK_ID)
  const block = blockHidden ? [] : shuffledBlock

  // No custom order saved yet — today's default: block members shuffled
  // among themselves but left in their own original slots (not grouped into
  // one contiguous run), same as the old randomizeGenres behavior.
  if (!order.length) {
    if (blockHidden) return categories.filter(c => !isCollectionsBlockMember(c.id))
    let gi = 0
    return categories.map(c => (isCollectionsBlockMember(c.id) ? block[gi++] : c))
  }

  const rest = categories.filter(c => !isCollectionsBlockMember(c.id) && !hiddenSet.has(c.id))
  const restMap = new Map(rest.map(c => [c.id, c]))
  const result: CatalogCategory[] = []
  for (const id of order) {
    if (id === COLLECTIONS_BLOCK_ID) { result.push(...block); continue }
    const c = restMap.get(id)
    if (c) { result.push(c); restMap.delete(id) }
  }
  result.push(...restMap.values())
  if (!order.includes(COLLECTIONS_BLOCK_ID)) result.push(...block)
  return result
}
