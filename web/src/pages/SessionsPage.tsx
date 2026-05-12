import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
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
        <div className={styles.header}>
          <div>
            <h2 className={styles.title}>Активные сессии</h2>
            <p className={styles.hint}>Список устройств, с которых выполнен вход в аккаунт.</p>
          </div>
          <button className={`${styles.btnSm} ${styles.danger}`} onClick={revokeAll}>
            Завершить все
          </button>
        </div>

        {loading && <p className={styles.muted}>Загрузка…</p>}

        {!loading && sessions.length === 0 && (
          <p className={styles.muted}>Активных сессий нет.</p>
        )}

        <div className={styles.list}>
          {sessions.map(s => (
            <div key={s.id} className={`${styles.row} ${s.is_current ? styles.current : ''}`}>
              <div className={styles.info}>
                <span className={styles.browser}>{s.browser}</span>
                <span className={styles.meta}>
                  {s.ip} · {s.created_at}
                  {s.is_current && <em> · эта сессия</em>}
                </span>
              </div>
              <button
                className={`${styles.btnSm} ${s.is_current ? styles.danger : styles.secondary}`}
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
