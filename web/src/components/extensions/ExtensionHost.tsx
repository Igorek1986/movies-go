import { useEffect, useRef, useState } from 'react'
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import styles from './ExtensionsSection.module.scss'
import { buildExtensionShell } from './extensionShell'
import type { WebExtension } from '@/hooks/useExtensions'

// Именованные RPC-действия, которые расширению разрешено дёргать через мост
// (см. internal/api/web_extensions_rpc.go) — здесь только для быстрой
// диагностики в консоли, реальная проверка allowlist — на бэкенде.
const KNOWN_ACTIONS = new Set([
  'devices.list', 'devices.create', 'profiles.list', 'profiles.create', 'timecodes.importLampac',
])

const READY_TIMEOUT_MS = 5000

// Рендерит одно включённое расширение в изолированном iframe (sandbox
// "allow-scripts" без allow-same-origin — непрозрачный origin, ни cookie, ни
// DOM хоста расширению не видны) и обслуживает его RPC-мост через
// postMessage. Каждое расширение — свой <details>, заголовок берётся из
// Ext.ready({title}) с фолбэком на сохранённое имя.
interface Props {
  extension: WebExtension
  // ProfilesRemoteView (TV drill-down) skips the <details>/<summary> wrapper
  // — same convention as ExtensionsSection/CardLayoutSettings etc.
  bare?: boolean
}

export function ExtensionHost({ extension, bare }: Props) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const [title, setTitle] = useState(extension.name || extension.url)
  const [ready, setReady] = useState(false)
  const readyRef = useRef(false)
  const [timedOut, setTimedOut] = useState(false)
  const [height, setHeight] = useState(80)

  useEffect(() => {
    const timer = setTimeout(() => { if (!readyRef.current) setTimedOut(true) }, READY_TIMEOUT_MS)
    return () => clearTimeout(timer)
  }, [extension.url])

  useEffect(() => {
    function onMessage(e: MessageEvent) {
      if (!iframeRef.current || e.source !== iframeRef.current.contentWindow) return
      const m = e.data
      if (!m || typeof m !== 'object') return

      if (m.type === 'ext:ready') {
        readyRef.current = true
        setReady(true)
        setTimedOut(false)
        if (m.meta?.title) setTitle(m.meta.title)
        return
      }

      if (m.type === 'ext:resize') {
        if (typeof m.height === 'number' && m.height > 0) setHeight(m.height)
        return
      }

      if (m.type === 'ext:api') {
        const { reqId, action, params } = m
        if (!KNOWN_ACTIONS.has(action)) {
          iframeRef.current.contentWindow?.postMessage({ type: 'ext:api:reply', reqId, error: 'unknown action' }, '*')
          return
        }
        fetch('/api/extensions/rpc', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action, params }),
        })
          .then(async res => {
            const body = await res.json().catch(() => ({}))
            if (!res.ok) throw new Error(body.error || 'Ошибка запроса')
            return body
          })
          .then(result => {
            iframeRef.current?.contentWindow?.postMessage({ type: 'ext:api:reply', reqId, result }, '*')
          })
          .catch(err => {
            iframeRef.current?.contentWindow?.postMessage({ type: 'ext:api:reply', reqId, error: String(err.message || err) }, '*')
          })
      }
    }

    window.addEventListener('message', onMessage)
    return () => window.removeEventListener('message', onMessage)
  }, [])

  const frame = (
    <>
      {!ready && <p className={styles.hostLoading}>Загрузка…</p>}
      <iframe
        ref={iframeRef}
        className={styles.hostFrame}
        style={{ height }}
        data-nav-item={bare ? true : undefined}
        // allow-forms — иначе браузер тихо блокирует submit любой <form>
        // внутри расширения (консоль: "Blocked form submission... sandboxed
        // and the 'allow-forms' permission is not set") ДО того, как JS
        // вообще получает событие — e.preventDefault() в обработчике не
        // успевает сработать, потому что обработчик не вызывается вовсе.
        // Не расширяет доступ за пределы самого iframe: без allow-same-origin
        // он всё так же не может обратиться никуда, кроме postMessage-моста.
        sandbox="allow-scripts allow-forms"
        allow="clipboard-write"
        srcDoc={buildExtensionShell(extension.url)}
        title={title}
      />
    </>
  )

  if (bare) {
    return (
      <div data-row-id={`ext-host-${extension.id}`}>
        <p className={pageStyles.hint}>
          {title}
          {timedOut && !ready && <span className={`${styles.badge} ${styles.badgeWarn}`} style={{ marginLeft: 8 }}>не отвечает</span>}
        </p>
        {frame}
      </div>
    )
  }

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>
        {title}
        {timedOut && !ready && <span className={`${styles.badge} ${styles.badgeWarn}`} style={{ marginLeft: 8 }}>не отвечает</span>}
      </summary>
      <div className={pageStyles.detailsBody}>{frame}</div>
    </details>
  )
}
