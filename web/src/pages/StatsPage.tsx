import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { useAuth } from '@/hooks/useAuth'
import styles from './StatsPage.module.scss'

interface NewUser {
  username: string
  created_at: string
}

interface StatRow {
  name: string
  requests: number
}

interface StatsData {
  users: number
  users_today: number
  devices: number
  media_cards: number
  timecodes: number
  new_users_today: NewUser[]
  api_ips_today: number
  api_reqs_today: number
  api_today: StatRow[]
  api_total: StatRow[]
  cats_today: StatRow[]
  cats_total: StatRow[]
  myshows_today: StatRow[]
  myshows_total: StatRow[]
}

interface UserRow {
  id: number
  username: string
  role: string
  is_admin: boolean
  created_at: string
  device_count: number
}

type Tab = 'today' | 'all'

function StatTable({ rows, cols }: { rows: StatRow[]; cols: [string, string] }) {
  const total = rows.reduce((s, r) => s + r.requests, 0)
  if (rows.length === 0) {
    return <p className={styles.emptyText}>Нет данных</p>
  }
  return (
    <table className={styles.table}>
      <thead>
        <tr>
          <th>{cols[0]}</th>
          <th>{cols[1]}</th>
          <th>Доля</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={i}>
            <td>{r.name}</td>
            <td className={styles.muted}>{r.requests.toLocaleString('ru')}</td>
            <td className={styles.muted}>
              {total > 0 ? ((r.requests / total) * 100).toFixed(1) + '%' : '—'}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function Section({
  title,
  tab,
  onTab,
  todayContent,
  allContent,
}: {
  title: string
  tab: Tab
  onTab: (t: Tab) => void
  todayContent: React.ReactNode
  allContent: React.ReactNode
}) {
  return (
    <div className={styles.section}>
      <div className={styles.sectionHeader}>
        <h2 className={styles.sectionTitle}>{title}</h2>
        <div className={styles.tabs}>
          <button
            className={`${styles.tab}${tab === 'today' ? ' ' + styles.tabActive : ''}`}
            onClick={() => onTab('today')}
          >
            Сегодня
          </button>
          <button
            className={`${styles.tab}${tab === 'all' ? ' ' + styles.tabActive : ''}`}
            onClick={() => onTab('all')}
          >
            Всё время
          </button>
        </div>
      </div>
      {tab === 'today' ? todayContent : allContent}
    </div>
  )
}

export default function StatsPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [stats, setStats] = useState<StatsData | null>(null)
  const [allUsers, setAllUsers] = useState<UserRow[]>([])
  const [usersTab, setUsersTab] = useState<Tab>('today')
  const [apiTab, setApiTab] = useState<Tab>('today')
  const [catsTab, setCatsTab] = useState<Tab>('today')
  const [myshowsTab, setMyshowsTab] = useState<Tab>('today')
  const [loading, setLoading] = useState(true)
  const [lastUpdate, setLastUpdate] = useState('')

  const fetchStats = useCallback(async () => {
    try {
      const [sRes, uRes] = await Promise.all([
        fetch('/api/admin/stats'),
        fetch('/api/admin/users'),
      ])
      if (sRes.ok) setStats(await sRes.json())
      if (uRes.ok) setAllUsers(await uRes.json())
      setLastUpdate(new Date().toLocaleTimeString('ru'))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (user && !user.is_admin) {
      navigate('/', { replace: true })
      return
    }
    fetchStats()
  }, [user, navigate, fetchStats])

  if (loading) {
    return <Layout><div className={styles.loading}>Загрузка…</div></Layout>
  }

  const counters = [
    { label: 'Новых сегодня', value: stats?.users_today ?? 0 },
    { label: 'Пользователей', value: stats?.users ?? 0 },
    { label: 'Устройств', value: stats?.devices ?? 0 },
    { label: 'IP сегодня', value: stats?.api_ips_today ?? 0 },
    { label: 'Запросов сегодня', value: stats?.api_reqs_today ?? 0 },
    { label: 'Карточек', value: stats?.media_cards ?? 0 },
    { label: 'Таймкодов', value: stats?.timecodes ?? 0 },
  ]

  return (
    <Layout>
      <div className={styles.page}>
        <div className={styles.pageHeader}>
          <h1 className={styles.pageTitle}>Статистика</h1>
          <div className={styles.headerRight}>
            {lastUpdate && <span className={styles.updateTime}>Обновлено: {lastUpdate}</span>}
            <button className={styles.refreshBtn} onClick={fetchStats}>Обновить</button>
          </div>
        </div>

        <div className={styles.countersGrid}>
          {counters.map(c => (
            <div key={c.label} className={styles.counterCard}>
              <div className={styles.counterValue}>{c.value.toLocaleString('ru')}</div>
              <div className={styles.counterLabel}>{c.label}</div>
            </div>
          ))}
        </div>

        {/* Пользователи */}
        <Section
          title="Пользователи"
          tab={usersTab}
          onTab={setUsersTab}
          todayContent={
            <table className={styles.table}>
              <thead><tr><th>Логин</th><th>Время</th></tr></thead>
              <tbody>
                {(stats?.new_users_today.length ?? 0) === 0
                  ? <tr><td colSpan={2} className={styles.emptyCell}>Новых нет</td></tr>
                  : stats!.new_users_today.map(u => (
                    <tr key={u.username + u.created_at}>
                      <td><strong>{u.username}</strong></td>
                      <td className={styles.muted}>{u.created_at}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          }
          allContent={
            <table className={styles.table}>
              <thead><tr><th>Логин</th><th>Роль</th><th>Устройств</th><th>Дата</th></tr></thead>
              <tbody>
                {allUsers.length === 0
                  ? <tr><td colSpan={4} className={styles.emptyCell}>Нет данных</td></tr>
                  : allUsers.map(u => (
                    <tr key={u.id}>
                      <td>
                        <strong>{u.username}</strong>
                        {u.is_admin && <span className={styles.adminBadge}>admin</span>}
                      </td>
                      <td className={styles.muted}>{u.role}</td>
                      <td className={styles.muted}>{u.device_count}</td>
                      <td className={styles.muted}>{new Date(u.created_at).toLocaleDateString('ru')}</td>
                    </tr>
                  ))}
              </tbody>
            </table>
          }
        />

        {/* API пользователи */}
        <Section
          title="API пользователи (IP)"
          tab={apiTab}
          onTab={setApiTab}
          todayContent={<StatTable rows={stats?.api_today ?? []} cols={['IP', 'Запросов']} />}
          allContent={<StatTable rows={stats?.api_total ?? []} cols={['IP', 'Запросов']} />}
        />

        {/* Категории */}
        <Section
          title="Категории"
          tab={catsTab}
          onTab={setCatsTab}
          todayContent={<StatTable rows={stats?.cats_today ?? []} cols={['Категория', 'Запросов']} />}
          allContent={<StatTable rows={stats?.cats_total ?? []} cols={['Категория', 'Запросов']} />}
        />

        {/* MyShows */}
        <Section
          title="MyShows"
          tab={myshowsTab}
          onTab={setMyshowsTab}
          todayContent={<StatTable rows={stats?.myshows_today ?? []} cols={['Логин', 'Синхронизаций']} />}
          allContent={<StatTable rows={stats?.myshows_total ?? []} cols={['Логин', 'Синхронизаций']} />}
        />
      </div>
    </Layout>
  )
}
