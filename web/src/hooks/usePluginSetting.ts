import { useCallback, useEffect, useState } from 'react'
import { getWebClientId, subscribeLiveSync } from '@/hooks/useLiveSync'

// Зеркалит pluginSettingKey на бэкенде (internal/api/web_plugin_settings.go) —
// нужно, чтобы сверять msg.key из живого SettingsHub-события (там всегда
// полный ключ) с этим (plugin, key, profileId).
function fullSettingKey(profileId: string, key: string): string {
  return profileId ? `${key}_profile_${profileId}` : key
}

// Lampa (np.js) хранит булевы значения строками 'true'/'false' — обходит
// баг Lampa.Storage.get, затирающий закешированный boolean false обратно
// дефолтом (см. storableValue в np.js) — и шлёт их серверу в таком же виде,
// так что plugin_settings может содержать как настоящий JSON-boolean (если
// последним писал веб), так и строку (если Lampa). Непустая строка "false"
// truthy в JS — без этой нормализации чекбокс всегда рисовался отмеченным.
function normalizeSettingValue<T>(raw: unknown, defaultValue: T): T {
  if (raw === undefined || raw === null) return defaultValue
  if (raw === 'true') return true as unknown as T
  if (raw === 'false') return false as unknown as T
  return raw as T
}

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
        setValue(normalizeSettingValue(data?.value, defaultValue))
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
    // defaultValue намеренно не в зависимостях — обычно это инлайн-литерал,
    // на каждый рендер новый объект/примитив по ссылке не важен, но если это
    // объект — не хотим перезапрашивать из-за него.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plugin, key, profileId])

  // Живое обновление, если этот же ключ поменяли на другом устройстве/вкладке
  // (Lampa через __NMSync или другая открытая вкладка веба) — без этого
  // галочка отражала бы чужое изменение только после перезагрузки страницы.
  useEffect(() => {
    const fullKey = fullSettingKey(profileId, key)
    return subscribeLiveSync((msg) => {
      if (msg.plugin !== plugin || msg.key !== fullKey) return
      setValue(normalizeSettingValue(msg.value, defaultValue))
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plugin, key, profileId])

  const save = useCallback((next: T): Promise<boolean> => {
    setValue(next)
    const params = new URLSearchParams({ plugin, key, profile_id: profileId, client_id: getWebClientId() })
    return fetch(`/api/web/plugin-setting?${params}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ value: next }),
    }).then(r => r.ok).catch(() => false)
  }, [plugin, key, profileId])

  return { value, setValue: save, loaded }
}
