import { useEffect, useState } from 'react'
import Layout from '@/components/Layout'
import styles from './AdminPage.module.scss'

interface AdminUser {
  id: number
  username: string
  role: string
  is_admin: boolean
  created_at: string
  device_count: number
}

interface Stats {
  users: number
  devices: number
  media_cards: number
  timecodes: number
}

export default function AdminPage() {
  const [users, setUsers] = useState<AdminUser[]>([])
  const [stats, setStats] = useState<Stats | null>(null)
  const [loading, setLoading] = useState(true)

  async function fetchAll() {
    setLoading(true)
    const [uRes, sRes] = await Promise.all([
      fetch('/api/admin/users'),
      fetch('/api/admin/stats'),
    ])
    if (uRes.ok) setUsers(await uRes.json())
    if (sRes.ok) setStats(await sRes.json())
    setLoading(false)
  }

  useEffect(() => { fetchAll() }, [])

  async function setRole(id: number, role: string) {
    await fetch(`/api/admin/users/${id}/role`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ role }),
    })
    fetchAll()
  }

  async function deleteUser(id: number, username: string) {
    if (!confirm(`Удалить пользователя ${username}?`)) return
    await fetch(`/api/admin/users/${id}`, { method: 'DELETE' })
    fetchAll()
  }

  const roleOrder = ['simple', 'premium', 'super']
  function nextRole(current: string) {
    const idx = roleOrder.indexOf(current)
    return roleOrder[(idx + 1) % roleOrder.length]
  }

  return (
    <Layout>
      <div className={styles.page}>
        <h1 className={styles.title}>Администрирование</h1>

        {stats && (
          <div className={styles.stats}>
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.users}</p>
              <p className={styles.statLabel}>Пользователей</p>
            </div>
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.devices}</p>
              <p className={styles.statLabel}>Устройств</p>
            </div>
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.media_cards.toLocaleString()}</p>
              <p className={styles.statLabel}>Медиакарточек</p>
            </div>
            <div className={styles.statCard}>
              <p className={styles.statValue}>{stats.timecodes.toLocaleString()}</p>
              <p className={styles.statLabel}>Таймкодов</p>
            </div>
          </div>
        )}

        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>Пользователи</h2>
          {loading && <p className={styles.empty}>Загрузка…</p>}
          {!loading && users.length === 0 && <p className={styles.empty}>Нет пользователей</p>}
          {!loading && users.length > 0 && (
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Имя</th>
                  <th>Роль</th>
                  <th>Устройств</th>
                  <th>Создан</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {users.map(u => (
                  <tr key={u.id}>
                    <td>{u.id}</td>
                    <td>{u.username}{u.is_admin && ' 👑'}</td>
                    <td>
                      <span className={`${styles.roleBadge} ${styles[u.role] ?? styles.simple}`}>
                        {u.role}
                      </span>
                    </td>
                    <td>{u.device_count}</td>
                    <td>{new Date(u.created_at).toLocaleDateString('ru-RU')}</td>
                    <td>
                      <div className={styles.actions}>
                        <button
                          className={styles.btnSm}
                          onClick={() => setRole(u.id, nextRole(u.role))}
                          title={`Сменить на ${nextRole(u.role)}`}
                        >
                          → {nextRole(u.role)}
                        </button>
                        {!u.is_admin && (
                          <button
                            className={`${styles.btnSm} ${styles.danger}`}
                            onClick={() => deleteUser(u.id, u.username)}
                          >
                            Удалить
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </Layout>
  )
}
