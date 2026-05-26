import { useEffect, useState } from 'react'
import { NavLink, useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import styles from './Layout.module.scss'

export default function Layout({ children, wide }: { children: React.ReactNode; wide?: boolean }) {
  const { user } = useAuth()
  const nav = useNavigate()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)

  useEffect(() => {
    setMenuOpen(false)
  }, [location.pathname])

  useEffect(() => {
    if (menuOpen) {
      const scrollY = window.scrollY
      document.body.style.position = 'fixed'
      document.body.style.top = `-${scrollY}px`
      document.body.style.width = '100%'
      document.body.style.overflowY = 'scroll'
    } else {
      const top = parseFloat(document.body.style.top || '0')
      document.body.style.position = ''
      document.body.style.top = ''
      document.body.style.width = ''
      document.body.style.overflowY = ''
      if (top !== 0) window.scrollTo(0, Math.abs(top))
    }
    return () => {
      document.body.style.position = ''
      document.body.style.top = ''
      document.body.style.width = ''
      document.body.style.overflowY = ''
    }
  }, [menuOpen])

  async function handleLogout() {
    await fetch('/api/logout', { method: 'POST' })
    nav('/login', { replace: true })
    window.location.reload()
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `${styles.navLink}${isActive ? ' ' + styles.active : ''}`

  const links = (
    <>
      <NavLink to="/profiles" className={linkClass} onClick={() => setMenuOpen(false)}>Устройства</NavLink>
      <NavLink to="/catalog"  className={linkClass} onClick={() => setMenuOpen(false)}>Каталог</NavLink>
      <NavLink to="/history"  className={linkClass} onClick={() => setMenuOpen(false)}>История</NavLink>
      <NavLink to="/sessions" className={linkClass} onClick={() => setMenuOpen(false)}>Сессии</NavLink>
      {user?.is_admin && (
        <NavLink to="/admin" className={linkClass} onClick={() => setMenuOpen(false)}>Админ</NavLink>
      )}
      {user?.is_admin && (
        <NavLink to="/stats" className={linkClass} onClick={() => setMenuOpen(false)}>Статистика</NavLink>
      )}
    </>
  )

  return (
    <div className={styles.layout}>
      <nav className={styles.nav}>
        <a className={styles.brand} href="/">Movies API</a>

        {/* Desktop */}
        <div className={styles.navLinks}>
          {links}
          <span className={styles.navUser}>{user?.username}</span>
          <button className={styles.btnLogout} onClick={handleLogout}>Выйти</button>
        </div>

        {/* Mobile burger */}
        <button
          className={`${styles.burger}${menuOpen ? ' ' + styles.burgerOpen : ''}`}
          onClick={() => setMenuOpen(o => !o)}
          aria-label="Меню"
        >
          <span /><span /><span />
        </button>
      </nav>

      {/* Mobile overlay */}
      {menuOpen && (
        <div className={styles.overlay} onClick={() => setMenuOpen(false)} />
      )}

      {/* Mobile drawer */}
      <div className={`${styles.drawer}${menuOpen ? ' ' + styles.drawerOpen : ''}`}>
        <div className={styles.drawerUser}>{user?.username}</div>
        <div className={styles.drawerLinks}>
          {links}
        </div>
        <button className={styles.drawerLogout} onClick={handleLogout}>Выйти</button>
      </div>

      <main className={`${styles.main}${wide ? ' ' + styles.mainWide : ''}`}>{children}</main>
    </div>
  )
}
