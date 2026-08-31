import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { focusTopNavActive } from '@/utils/scrollNav'
import styles from './SessionsPage.module.scss'

interface Session {
  id: number
  browser: string
  ip: string
  created_at: string
  is_current: boolean
}

export default function SessionsPage() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const nav = useNavigate()

  async function load() {
    setLoading(true)
    const r = await fetch('/api/sessions')
    if (r.ok) {
      const d = await r.json()
      setSessions(d.sessions ?? [])
    }
    setLoading(false)
  }

  useEffect(() => { load() }, [])

  // Site-wide keyboard nav (see Layout/CardDetailPage): rows marked with
  // data-row-id, one focusable data-nav-item each — Up/Down moves between
  // rows, Up from the top row bridges to the top menu. Backspace and the
  // Left/Right side-panel summon are already handled by Layout itself for
  // every page but /catalog, so this only needs to own Up/Down.
  useEffect(() => {
    function isTypingTarget(el: Element | null) {
      const tag = el?.tagName
      return tag === 'INPUT' || tag === 'TEXTAREA' || (el as HTMLElement | null)?.isContentEditable
    }
    function getRows() {
      return Array.from(document.querySelectorAll<HTMLElement>('[data-row-id]'))
        .filter(row => row.querySelector('[data-nav-item]'))
    }
    function onKeyDown(e: KeyboardEvent) {
      if (isTypingTarget(document.activeElement)) return
      if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return

      const focused = document.activeElement as HTMLElement | null
      const currentRow = focused?.closest<HTMLElement>('[data-row-id]') ?? null
      const rows = getRows()

      if (!currentRow) {
        if (e.key === 'ArrowDown' && rows.length) {
          e.preventDefault()
          rows[0].querySelector<HTMLElement>('[data-nav-item]')?.focus()
        }
        return
      }

      e.preventDefault()
      const rowIdx = rows.indexOf(currentRow)
      if (e.key === 'ArrowUp' && rowIdx === 0) {
        focusTopNavActive()
        return
      }
      const targetIdx = rowIdx + (e.key === 'ArrowDown' ? 1 : -1)
      if (targetIdx < 0 || targetIdx >= rows.length) return
      rows[targetIdx].querySelector<HTMLElement>('[data-nav-item]')?.focus({ preventScroll: true })
      rows[targetIdx].scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  async function revoke(s: Session) {
    const msg = s.is_current
      ? 'Завершить текущую сессию? Вы будете выйдены.'
      : 'Завершить эту сессию?'
    if (!confirm(msg)) return
    const r = await fetch(`/api/sessions/${s.id}`, { method: 'DELETE' })
    const d = await r.json()
    if (d.logged_out) {
      nav('/login', { replace: true })
      window.location.reload()
    } else {
      load()
    }
  }

  async function revokeAll() {
    if (!confirm('Завершить все сессии? Вы будете выйдены отовсюду.')) return
    await fetch('/api/sessions', { method: 'DELETE' })
    nav('/login', { replace: true })
    window.location.reload()
  }

  return (
    <Layout>
      <div className={styles.page}>
        <div className={styles.header} data-row-id="revoke-all">
          <div>
            <h2 className={styles.title}>Активные сессии</h2>
            <p className={styles.hint}>Список устройств, с которых выполнен вход в аккаунт.</p>
          </div>
          <button className={`${styles.btnSm} ${styles.danger}`} data-nav-item onClick={revokeAll}>
            Завершить все
          </button>
        </div>

        {loading && <p className={styles.muted}>Загрузка…</p>}

        {!loading && sessions.length === 0 && (
          <p className={styles.muted}>Активных сессий нет.</p>
        )}

        <div className={styles.list}>
          {sessions.map(s => (
            <div key={s.id} className={`${styles.row} ${s.is_current ? styles.current : ''}`} data-row-id={`session-${s.id}`}>
              <div className={styles.info}>
                <span className={styles.browser}>{s.browser}</span>
                <span className={styles.meta}>
                  {s.ip} · {s.created_at}
                  {s.is_current && <em> · эта сессия</em>}
                </span>
              </div>
              <button
                className={`${styles.btnSm} ${s.is_current ? styles.danger : styles.secondary}`}
                data-nav-item
                onClick={() => revoke(s)}
              >
                {s.is_current ? 'Выйти' : 'Завершить'}
              </button>
            </div>
          ))}
        </div>
      </div>
    </Layout>
  )
}
