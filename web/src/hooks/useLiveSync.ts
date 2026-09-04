import { useEffect, useRef } from 'react'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { invalidateRowCachesAfterStatusChange } from '@/utils/rowCacheSync'

// Стабильный на всю вкладку id — тот же смысл, что у window.__npClientId в
// Lampa-плагинах (np_unwatched.js/np_profiles.js): сервер исключает по нему
// ИМЕННО эту вкладку из широковещательной рассылки её же собственного
// изменения (см. exceptClientID в internal/ws.Hub.Broadcast), не более того —
// не персистентный между перезагрузками, не идентифицирует пользователя.
let webClientId = ''
export function getWebClientId(): string {
  if (!webClientId) {
    webClientId = typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : Math.random().toString(36).slice(2)
  }
  return webClientId
}

const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 10000, 30000]

export interface LiveSyncMessage {
  type?: string
  profile_id?: string
  card_id?: string
  status?: string
  item?: string
  data?: { time?: number; duration?: number; percent?: number; special?: boolean }
  // Событие SettingsHub (см. handleWebPatchPluginSetting/handlePatchPluginSettings) —
  // единственное без своего type: та же структура, что уже шлют друг другу
  // Lampa-устройства через __NMSync. key — уже полный ключ с суффиксом профиля
  // (см. pluginSettingKey на бэкенде), не голый key из usePluginSetting.
  plugin?: string
  key?: string
  value?: unknown
}

// Публикация каждого входящего сообщения — единственное WS-соединение живёт в
// PrivateShell (useLiveSync ниже), но конкретной странице (например
// CardDetailPage) нужно реагировать на события именно её card_id живьём, не
// дожидаясь инвалидации построчных кешей Каталога/Моё. Простой pub/sub вместо
// второго WS-соединения на страницу.
type LiveSyncListener = (msg: LiveSyncMessage) => void
const listeners = new Set<LiveSyncListener>()

export function subscribeLiveSync(listener: LiveSyncListener): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

// Живая синхронизация статусов/таймкодов/избранного/профиля с Lampa-
// устройствами того же аккаунта — тот же WS-канал (TimecodeHub на /timecode/ws),
// что уже используют np_unwatched.js/np_profiles.js, но подключаемся на
// /api/web/ws с авторизацией по сессионной cookie вместо device-токена (у
// браузера токена нет и не должно быть — см. handleWebWS на бэкенде).
//
// Смонтирован один раз в PrivateShell (см. App.tsx) — соединение живёт всё
// время работы с приватными страницами, реконнектится с нарастающей паузой
// при обрыве.
export function useLiveSync(): void {
  const { activeProfile, refresh } = useActiveProfile()

  // Рефы вместо прямых зависимостей эффекта — переоткрывать WS-соединение при
  // каждой смене активного профиля незачем, сообщения фильтруются по свежему
  // значению без пересоздания сокета.
  const activeProfileRef = useRef(activeProfile)
  activeProfileRef.current = activeProfile
  const refreshRef = useRef(refresh)
  refreshRef.current = refresh

  useEffect(() => {
    let ws: WebSocket | null = null
    let stopped = false
    let attempt = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined

    function connect() {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      ws = new WebSocket(`${proto}//${location.host}/api/web/ws?client_id=${encodeURIComponent(getWebClientId())}`)

      ws.onopen = () => { attempt = 0 }

      ws.onmessage = (event) => {
        let msg: LiveSyncMessage
        try { msg = JSON.parse(event.data) } catch { return }

        // SettingsHub-события (см. LiveSyncMessage.plugin) не несут type —
        // только plugin/key/value/profile_id. usePluginSetting сам решает,
        // относится ли конкретное сообщение к нему (сверяя plugin+key), а
        // profile_id здесь не тот, что у sameProfile ниже (это профиль, для
        // которого сохранена настройка, а не профиль устройства-отправителя,
        // хотя обычно совпадают) — не фильтруем, просто раздаём подписчикам.
        if (!msg.type) {
          if (msg.plugin && msg.key) listeners.forEach(l => l(msg))
          return
        }

        if (msg.type === 'profile_updated') {
          refreshRef.current()
          return
        }

        // Пересечение aired_cutoff (см. StartUnwatchedCutoffInvalidation на
        // бэкенде) — рассылается TimecodeHub.BroadcastAll всем подряд, без
        // profile_id (это не действие конкретного профиля/устройства), поэтому
        // не проходит через фильтр sameProfile ниже. Строка «Непросмотренные»
        // сама разберётся (см. подписку в CatalogPage) — здесь только чистим
        // кеш, чтобы немонтированная строка тоже перезапросила данные при
        // следующем открытии.
        if (msg.type === 'unwatched_stale') {
          invalidateRowCachesAfterStatusChange()
          listeners.forEach(l => l(msg))
          return
        }

        // profile_id в сообщении — профиль устройства-отправителя; сверяем с
        // активным на вебе, чтобы не дёргать кеш зря при изменениях в другом
        // профиле того же аккаунта (тот же принцип, что и в np_unwatched.js —
        // там тоже фильтрация по profile_id идёт на клиенте, не на сервере).
        const active = activeProfileRef.current
        const sameProfile = !!active && msg.profile_id === active.profile_id

        if ((msg.type === 'status' || msg.type === 'timecode' || msg.type === 'favorite') && sameProfile) {
          invalidateRowCachesAfterStatusChange()
        }

        // Подписчикам (CardDetailPage) решать самим, относится ли card_id к
        // ним — здесь фильтруем только по профилю, чтобы не разносить чужие
        // события другого профиля того же аккаунта.
        if (sameProfile) {
          listeners.forEach(l => l(msg))
        }
      }

      ws.onclose = () => {
        if (stopped) return
        const delay = RECONNECT_DELAYS_MS[Math.min(attempt, RECONNECT_DELAYS_MS.length - 1)]
        attempt++
        reconnectTimer = setTimeout(connect, delay)
      }

      ws.onerror = () => { ws?.close() }
    }

    connect()

    return () => {
      stopped = true
      clearTimeout(reconnectTimer)
      ws?.close()
    }
  }, [])
}
