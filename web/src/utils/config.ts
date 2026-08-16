// App-wide config pulled from GET /api/config once at startup (see App.tsx) — module-level
// mutable state, same pattern as setImgProxy in poster.ts.

let _watchedThreshold = 90 // matches the server default (app_settings "watched_threshold")

export function setWatchedThreshold(pct: number) {
  if (pct > 0) _watchedThreshold = pct
}

export function watchedThreshold(): number {
  return _watchedThreshold
}
