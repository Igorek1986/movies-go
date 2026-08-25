// Shared cache key/TTL for the /api/admin/stats response — used by Layout
// (prefetches on any page once an admin is logged in) and by AdminPage /
// StatsPage (seed their initial render from it, then skip refetching while
// still fresh). One key so a prefetch from either surface warms the other.
export const ADMIN_STATS_CACHE_KEY = 'admin_stats_cache_v1'
export const ADMIN_STATS_TTL_MS = 5 * 60 * 1000
