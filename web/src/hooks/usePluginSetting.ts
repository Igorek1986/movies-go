import { useCallback, useEffect, useState } from 'react'

// Читает/пишет один ключ из plugin_settings (та же таблица и те же ключи,
// которыми Lampa-плагины (np.js, np_unwatched.js) обмениваются между
// устройствами через __NMSync/handlePatchPluginSettings) — см.
// handleWebGetPluginSetting/handleWebPatchPluginSetting на бэкенде. Строка
// в БД общая на весь профиль (user_id, profile_id, plugin), не привязана к
// конкретному устройству — правка отсюда сразу видна и в Lampa, и наоборот.
export function usePluginSetting<T>(plugin: string, key: string, profileId: string, defaultValue: T) {
  const [value, setValue] = useState<T>(defaultValue)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    setLoaded(false)
    const params = new URLSearchParams({ plugin, key, profile_id: profileId })
    fetch(`/api/web/plugin-setting?${params}`)
      .then(r => r.ok ? r.json() : null)
      .then((data: { value?: T } | null) => {
        setValue(data && data.value !== undefined && data.value !== null ? data.value : defaultValue)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
    // defaultValue намеренно не в зависимостях — обычно это инлайн-литерал,
    // на каждый рендер новый объект/примитив по ссылке не важен, но если это
    // объект — не хотим перезапрашивать из-за него.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plugin, key, profileId])

  const save = useCallback((next: T): Promise<boolean> => {
    setValue(next)
    const params = new URLSearchParams({ plugin, key, profile_id: profileId })
    return fetch(`/api/web/plugin-setting?${params}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value: next }),
    }).then(r => r.ok).catch(() => false)
  }, [plugin, key, profileId])

  return { value, setValue: save, loaded }
}
