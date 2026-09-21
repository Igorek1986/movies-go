// Счётчик запросов в полёте — нужен pull-to-refresh, чтобы понять, что
// ремонтированная страница уже дозагрузила данные и можно убирать снимок
// старой (см. PullToRefresh.tsx). Оборачивает window.fetch один раз при
// первом импорте; на поведение самих запросов не влияет.
let pending = 0
let lastChange = Date.now()

if (typeof window !== 'undefined' && !(window.fetch as { __tracked?: boolean }).__tracked) {
  const orig = window.fetch.bind(window)
  const tracked: typeof window.fetch = (...args) => {
    pending++
    lastChange = Date.now()
    return orig(...args).finally(() => { pending--; lastChange = Date.now() })
  }
  ;(tracked as { __tracked?: boolean }).__tracked = true
  window.fetch = tracked
}

// Нет запросов в полёте и новых не было quietMs — данные страницы осели
export function isNetworkQuiet(quietMs: number): boolean {
  return pending === 0 && Date.now() - lastChange >= quietMs
}
