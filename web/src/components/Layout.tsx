import { useEffect, useState } from 'react'
import { NavLink, useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { applyTheme, getStoredTheme, THEMES, type ThemeId } from '@/utils/theme'
import { loadTTLCache, saveTTLCache } from '@/utils/ttlCache'
import { ADMIN_STATS_CACHE_KEY, ADMIN_STATS_TTL_MS } from '@/utils/adminStatsCache'
import styles from './Layout.module.scss'

export default function Layout({ children, wide }: { children: React.ReactNode; wide?: boolean }) {
  const { user } = useAuth()
  const nav = useNavigate()
  const location = useLocation()
  const [menuOpen, setMenuOpen] = useState(false)
  const [theme, setTheme] = useState<ThemeId>(() => getStoredTheme())

  function handleThemeChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const next = e.target.value as ThemeId
    applyTheme(next)
    setTheme(next)
  }

  useEffect(() => {
    setMenuOpen(false)
  }, [location.pathname])

  // Warm the admin-stats cache from whatever page the admin happens to be
  // on, so that by the time they actually open /admin or /stats it's
  // usually already fresh — skipped entirely if a still-valid cache exists
  // (no point re-fetching on every page navigation).
  useEffect(() => {
    if (!user?.is_admin) return
    const cached = loadTTLCache(ADMIN_STATS_CACHE_KEY, ADMIN_STATS_TTL_MS)
    if (cached && !cached.stale) return
    fetch('/api/admin/stats')
      .then(r => (r.ok ? r.json() : null))
      .then(data => { if (data) saveTTLCache(ADMIN_STATS_CACHE_KEY, data) })
      .catch(() => { /* best-effort prefetch */ })
  }, [user?.is_admin])

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
      <NavLink to="/catalog"  className={linkClass} onClick={() => { setMenuOpen(false); window.dispatchEvent(new CustomEvent('catalog:back')) }}>Каталог</NavLink>
      <NavLink to="/calendar" className={linkClass} onClick={() => setMenuOpen(false)}>Календарь</NavLink>
      <NavLink to="/media-library" className={linkClass} onClick={() => setMenuOpen(false)}>Моё</NavLink>
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
        {/* Mobile burger — leftmost */}
        <button
          className={`${styles.burger}${menuOpen ? ' ' + styles.burgerOpen : ''}`}
          onClick={() => setMenuOpen(o => !o)}
          aria-label="Меню"
        >
          <span /><span /><span />
        </button>

        <a className={styles.brand} href="/">Movies API</a>

        {/* Desktop */}
        <div className={styles.navLinks}>
          {links}
          <span className={styles.navUser}>{user?.username}</span>
          <ProfileSwitcher />
          <select className={styles.themeSelect} value={theme} onChange={handleThemeChange} aria-label="Тема">
            {THEMES.map(t => (
              <option key={t.id} value={t.id}>{t.label}</option>
            ))}
          </select>
          <button className={styles.btnLogout} onClick={handleLogout}>Выйти</button>
        </div>

        {/* Mobile: profile switcher — rightmost */}
        <div className={styles.navMobileRight}>
          <ProfileSwitcher />
        </div>
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
        <select className={styles.drawerThemeSelect} value={theme} onChange={handleThemeChange} aria-label="Тема">
          {THEMES.map(t => (
            <option key={t.id} value={t.id}>{t.label}</option>
          ))}
        </select>
        <button className={styles.drawerLogout} onClick={handleLogout}>Выйти</button>
      </div>

      <main className={`${styles.main}${wide ? ' ' + styles.mainWide : ''}`}>{children}</main>
    </div>
  )
}
