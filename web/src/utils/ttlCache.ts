// Tiny localStorage-backed cache with a TTL. Used to seed a page's initial
// render from the last known value instantly — including across full page
// reloads and from a prefetch triggered on a different page (unlike a plain
// module-level variable, which only survives client-side navigation within
// the same tab load) — while still telling the caller whether it's fresh
// enough to skip a refetch.

interface Entry<T> {
  data: T
  savedAt: number
}

export function loadTTLCache<T>(key: string, ttlMs: number): { data: T; stale: boolean } | null {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Entry<T>
    return { data: parsed.data, stale: Date.now() - parsed.savedAt > ttlMs }
  } catch {
    return null
  }
}

export function saveTTLCache<T>(key: string, data: T): void {
  try {
    localStorage.setItem(key, JSON.stringify({ data, savedAt: Date.now() } satisfies Entry<T>))
  } catch {
    // private-mode / quota-exceeded — cache is a nice-to-have, not essential
  }
}
