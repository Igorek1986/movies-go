// Bottom-nav "Поиск" needs to focus the catalog's search input, which may
// require navigating to /catalog first (from another page) — CatalogPage
// won't be mounted yet when the click handler runs, so a plain custom event
// can be lost. A flag checked on mount covers that case; the event covers
// the same-page case (already on /catalog, no remount happens).
let pending = false

export function requestFocusCatalogSearch() {
  pending = true
  window.dispatchEvent(new CustomEvent('catalog:focus-search'))
}

export function takePendingFocusCatalogSearch(): boolean {
  if (!pending) return false
  pending = false
  return true
}
