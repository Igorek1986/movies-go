import { useCallback, useEffect, useState } from 'react'
import { subscribeLiveSync, getWebClientId } from './useLiveSync'

export interface WebExtension {
  id: number
  url: string
  name: string
  enabled: boolean
  status: 'ok' | 'unreachable' | 'invalid' | 'unknown'
  status_code: number | null
  status_checked_at: string | null
  created_at: string
}

export interface UseExtensionsResult {
  extensions: WebExtension[]
  loading: boolean
  add: (url: string, name?: string) => Promise<WebExtension>
  update: (id: number, patch: { name?: string; enabled?: boolean }) => Promise<void>
  remove: (id: number) => Promise<void>
  check: (id: number) => Promise<void>
}

// CRUD + live-sync для /profiles → «Расширения». Список per-user (не привязан
// к активному Lampa-профилю), обновляется у всех открытых вкладок/устройств
// того же аккаунта через WS-событие extensions_changed (см. useLiveSync.ts).
// Вызывать один раз на страницу (ProfilesClassicView/ProfilesRemoteView) и
// прокидывать результат вниз пропом — два независимых вызова в одной вкладке
// не видят изменений друг друга сразу же (см. комментарий в
// ExtensionsSection.tsx о exceptClientID).
export function useExtensions(): UseExtensionsResult {
  const [extensions, setExtensions] = useState<WebExtension[]>([])
  const [loading, setLoading] = useState(true)

  const reload = useCallback(async () => {
    const res = await fetch('/api/extensions')
    if (!res.ok) return
    const d = await res.json().catch(() => ({}))
    setExtensions(d.extensions ?? [])
  }, [])

  useEffect(() => {
    reload().finally(() => setLoading(false))
    return subscribeLiveSync(msg => {
      if (msg.type === 'extensions_changed') reload()
    })
  }, [reload])

  const add = useCallback(async (url: string, name?: string) => {
    const res = await fetch(`/api/extensions?client_id=${encodeURIComponent(getWebClientId())}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, name: name ?? '' }),
    })
    const d = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(d.error || 'Ошибка добавления')
    await reload()
    return d as WebExtension
  }, [reload])

  const update = useCallback(async (id: number, patch: { name?: string; enabled?: boolean }) => {
    const res = await fetch(`/api/extensions/${id}?client_id=${encodeURIComponent(getWebClientId())}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    if (!res.ok) throw new Error('Ошибка сохранения')
    await reload()
  }, [reload])

  const remove = useCallback(async (id: number) => {
    const res = await fetch(`/api/extensions/${id}?client_id=${encodeURIComponent(getWebClientId())}`, { method: 'DELETE' })
    if (!res.ok) throw new Error('Ошибка удаления')
    await reload()
  }, [reload])

  const check = useCallback(async (id: number) => {
    const res = await fetch(`/api/extensions/${id}/check?client_id=${encodeURIComponent(getWebClientId())}`, { method: 'POST' })
    const d = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(d.error || 'Ошибка проверки')
    await reload()
  }, [reload])

  return { extensions, loading, add, update, remove, check }
}
