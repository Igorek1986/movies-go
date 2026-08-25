import { useEffect, useState } from 'react'
import { NavLink, useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { applyTheme, getStoredTheme, THEMES, type ThemeId } from '@/utils/theme'
import { loadTTLCache, saveTTLCache } from '@/utils/ttlCache'
import { ADMIN_STATS_CACHE_KEY, ADMIN_STATS_TTL_MS } from '@/utils/adminStatsCache'
import styles from './Layout.module.scss'

// Minimal stroke icons for the mobile bottom bar — currentColor so they pick
// up the active/inactive text color for free, no icon library needed for 4 glyphs.
const iconProps = { width: 22, height: 22, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }

function BackIcon() {
  return <svg {...iconProps}><path d="M15 18l-6-6 6-6" /></svg>
}
function HomeIcon() {
  return <svg {...iconProps}><path d="M3 11l9-8 9 8" /><path d="M5 10v10h14V10" /></svg>
}
function StarIcon() {
  return <svg {...iconProps}><path d="M12 2l3.09 6.26L22 9.27l-5 4.87L18.18 21 12 17.77 5.82 21 7 14.14l-5-4.87 6.91-1.01L12 2z" /></svg>
}
function HistoryIcon() {
  return <svg {...iconProps}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3.5 2" /></svg>
}

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

  // Mobile bottom bar — Назад/Главная/Поиск/Настройки, same 4 slots on every
  // page (like Lampa's own remote-control bar) instead of a page-specific
  // floating back button. Назад prefers the caller-supplied backUrl (exact
  // scroll/filter state on the catalog) so the "don't lose context" behavior
  // CardDetailPage/ActorPage relied on keeps working; falls back to normal
  // browser history otherwise.
  function handleBottomBack() {
    const backUrl = (location.state as { backUrl?: string } | null)?.backUrl
    if (backUrl) nav(backUrl)
    else nav(-1)
  }

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `${styles.navLink}${isActive ? ' ' + styles.active : ''}`

  const bottomNavClass = ({ isActive }: { isActive: boolean }) =>
    `${styles.bottomNavItem}${isActive ? ' ' + styles.bottomNavItemActive : ''}`

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

      {/* Mobile bottom bar — hidden on desktop via CSS, see .bottomNav */}
      <nav className={styles.bottomNav} aria-label="Быстрая навигация">
        <button type="button" className={styles.bottomNavItem} onClick={handleBottomBack}>
          <BackIcon />
          <span>Назад</span>
        </button>
        <NavLink to="/catalog" className={bottomNavClass} onClick={() => window.dispatchEvent(new CustomEvent('catalog:back'))}>
          <HomeIcon />
          <span>Главная</span>
        </NavLink>
        <NavLink to="/media-library" className={bottomNavClass}>
          <StarIcon />
          <span>Моё</span>
        </NavLink>
        <NavLink to="/history" className={bottomNavClass}>
          <HistoryIcon />
          <span>История</span>
        </NavLink>
      </nav>
    </div>
  )
}
