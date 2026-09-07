import { useEffect, useState } from 'react'

// App-wide config pulled from GET /api/config — see App.tsx (which also
// applies image_proxy_url/watched_threshold as side effects via utils/config.ts)
// and LoginPage/RegisterPage (which need registration_disabled at render time).
// Cached at module scope so every caller shares one network request.
export interface AppConfig {
  image_proxy_url: string
  bot_name: string
  plugin_url: string
  watched_threshold: number
  registration_disabled: boolean
  password_blocklist: string[]
}

let cache: Promise<AppConfig | null> | null = null

function fetchConfig(): Promise<AppConfig | null> {
  if (!cache) {
    cache = fetch('/api/config')
      .then(r => r.ok ? r.json() : null)
      .catch(() => null)
  }
  return cache
}

export function useAppConfig(): { config: AppConfig | null; loading: boolean } {
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    fetchConfig().then(c => {
      if (!cancelled) {
        setConfig(c)
        setLoading(false)
      }
    })
    return () => { cancelled = true }
  }, [])

  return { config, loading }
}
