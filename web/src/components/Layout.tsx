import { NavLink, useNavigate } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import styles from './Layout.module.scss'

export default function Layout({ children }: { children: React.ReactNode }) {
  const { user } = useAuth()
  const nav = useNavigate()

  async function handleLogout() {
    await fetch('/api/logout', { method: 'POST' })
    nav('/login', { replace: true })
    window.location.reload()
  }

  return (
    <div className={styles.layout}>
      <nav className={styles.nav}>
        <a className={styles.brand} href="/">Lampa</a>
        <div className={styles.navLinks}>
          <NavLink to="/profiles" className={({ isActive }) => `${styles.navLink}${isActive ? ' ' + styles.active : ''}`}>
            Устройства
          </NavLink>
          <NavLink to="/catalog" className={({ isActive }) => `${styles.navLink}${isActive ? ' ' + styles.active : ''}`}>
            Каталог
          </NavLink>
          <NavLink to="/history" className={({ isActive }) => `${styles.navLink}${isActive ? ' ' + styles.active : ''}`}>
            История
          </NavLink>
          {user?.is_admin && (
            <NavLink to="/admin" className={({ isActive }) => `${styles.navLink}${isActive ? ' ' + styles.active : ''}`}>
              Админ
            </NavLink>
          )}
          <span className={styles.navUser}>{user?.username}</span>
          <button className={styles.btnLogout} onClick={handleLogout}>Выйти</button>
        </div>
      </nav>
      <main className={styles.main}>{children}</main>
    </div>
  )
}
