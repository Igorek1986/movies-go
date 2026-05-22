import { useCallback, useEffect, useRef, useState } from 'react'

export interface ParserStatus {
  running: boolean
  stopRequested: boolean
  nextRunAt: string | null
}

const POLL_RUNNING = 3000
const POLL_IDLE = 15000

export function useParserStatus(): [ParserStatus, () => Promise<void>] {
  const [status, setStatus] = useState<ParserStatus>({
    running: false,
    stopRequested: false,
    nextRunAt: null,
  })
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const runningRef = useRef(false)

  const refresh = useCallback(async () => {
    try {
      const res = await fetch('/api/admin/parsers')
      if (!res.ok) return
      const d = await res.json()
      const s: ParserStatus = {
        running: d.running ?? false,
        stopRequested: d.stop_requested ?? false,
        nextRunAt: d.next_run_at || null,
      }
      setStatus(s)
      runningRef.current = s.running
    } catch { /* network error — keep last state */ }
  }, [])

  useEffect(() => {
    let cancelled = false

    async function tick() {
      if (cancelled) return
      await refresh()
      if (cancelled) return
      const delay = runningRef.current ? POLL_RUNNING : POLL_IDLE
      timerRef.current = setTimeout(tick, delay)
    }

    tick()
    return () => {
      cancelled = true
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [refresh])

  return [status, refresh]
}

export function useCountdown(target: string | null): number {
  const [secs, setSecs] = useState(0)
  useEffect(() => {
    if (!target) { setSecs(0); return }
    const tick = () => setSecs(Math.max(0, Math.floor((new Date(target).getTime() - Date.now()) / 1000)))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [target])
  return secs
}

export function fmtCountdown(sec: number): string {
  if (sec <= 0) return 'вот-вот'
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (h > 0) return `${h}ч ${m}м`
  if (m > 0) return `${m}м ${s}с`
  return `${s}с`
}

export function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  const dd = String(d.getDate()).padStart(2, '0')
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const yyyy = d.getFullYear()
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${dd}.${mm}.${yyyy} ${hh}:${min}`
}
