import { useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { NavLink, useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { BottomNav, BOTTOM_NAV_ICONS, type BottomNavItem } from '@/components/BottomNav'
import { BOTTOM_NAV_OPTIONS, resolveBottomNavKeys, resolveBottomNavPosition, type BottomNavConfig } from '@/utils/bottomNavConfig'
import { requestFocusCatalogSearch } from '@/utils/catalogSearchFocus'
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
  const searchWarmupRef = useRef<HTMLInputElement>(null)

  function handleThemeChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const next = e.target.value as ThemeId
    applyTheme(next)
    setTheme(next)
  }

  const [navKeys, setNavKeys] = useState<string[]>(() => resolveBottomNavKeys(undefined))
  const [navPosition, setNavPosition] = useState(() => resolveBottomNavPosition(undefined))
  useEffect(() => {
    setNavKeys(resolveBottomNavKeys(user?.bottom_nav_keys))
    setNavPosition(resolveBottomNavPosition(user?.bottom_nav_position))
  }, [user?.bottom_nav_keys, user?.bottom_nav_position])
  // Settings page dispatches this right after a successful save, so the
  // change is visible immediately without waiting for Layout's next mount
  // (per-page navigation) to re-fetch /api/me.
  useEffect(() => {
    const onUpdate = (e: Event) => {
      const detail = (e as CustomEvent<BottomNavConfig>).detail
      setNavKeys(detail.keys)
      setNavPosition(detail.position)
    }
    window.addEventListener('bottomnav:update', onUpdate)
    return () => window.removeEventListener('bottomnav:update', onUpdate)
  }, [])

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

  // Scroll-lock while the drawer is open. Deliberately NOT the classic
  // `body { position: fixed; top: -scrollY }` + restore-scroll trick — that
  // repositions body's own box, and iOS Safari/PWA (standalone especially)
  // has known bugs where other `position: fixed` descendants (BottomNav)
  // visually shift/jump along with it instead of staying viewport-anchored
  // as the spec says they should. Plain `overflow: hidden` + `overscroll-
  // behavior: contain` doesn't touch body's box at all, so nothing else on
  // the page can be dragged along with it — the modern recommended pattern
  // for this exact class of bug.
  useEffect(() => {
    if (menuOpen) {
      document.body.style.overflow = 'hidden'
      document.body.style.overscrollBehavior = 'contain'
    } else {
      document.body.style.overflow = ''
      document.body.style.overscrollBehavior = ''
    }
    return () => {
      document.body.style.overflow = ''
      document.body.style.overscrollBehavior = ''
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

  // User-configurable mobile bottom bar (see /profiles "Настройки" ->
  // «Нижняя панель») — navKeys is the account's saved key list (or the
  // built-in default), mapped to renderable items here. "back"/"home" get
  // special-cased onClick handlers; everything else is a plain route.
  // "Поиск" has no route of its own (BOTTOM_NAV_OPTIONS.search.to is null —
  // it'd otherwise highlight alongside "Главная" any time either lands on
  // /catalog). Already on /catalog: just collapse any expanded category and
  // focus in place.
  //
  // From elsewhere, a plain flushSync-then-focus isn't enough on iOS PWA:
  // even though the whole thing (route swap + CatalogPage's focus layout-
  // effect) runs synchronously inside this click handler, WebKit's "was this
  // focus() a direct result of a user gesture" heuristic doesn't survive a
  // full component-tree replacement (this Layout instance and its button get
  // torn down as part of the very navigation the click triggered). So we
  // "warm up" the keyboard on searchWarmupRef first — a plain input that's
  // already attached and isn't going anywhere until the navigation starts —
  // then hand focus to the real search input once it mounts. Once the
  // keyboard is up, moving focus between inputs doesn't need a fresh gesture
  // to keep it open, even across the heavier work in between.
  function handleBottomSearch() {
    if (location.pathname === '/catalog') {
      window.dispatchEvent(new CustomEvent('catalog:back'))
      window.dispatchEvent(new CustomEvent('catalog:focus-search'))
    } else {
      searchWarmupRef.current?.focus()
      requestFocusCatalogSearch()
      flushSync(() => nav('/catalog'))
    }
  }

  const bottomNavItems: BottomNavItem[] = navKeys.map(key => {
    const opt = BOTTOM_NAV_OPTIONS.find(o => o.key === key)!
    const onClick =
      key === 'back' ? handleBottomBack :
      key === 'home' ? () => window.dispatchEvent(new CustomEvent('catalog:back')) :
      key === 'search' ? handleBottomSearch :
      undefined
    return { key: opt.key, label: opt.label, icon: BOTTOM_NAV_ICONS[opt.key], to: opt.to ?? undefined, onClick }
  })

  // Desktop keyboard nav: Backspace = "Назад" everywhere (replaces the old
  // per-page floating back button), ArrowRight/ArrowLeft summon this same
  // panel as a floating side pill so desktop users get one without a mouse.
  // Skipped on /catalog: it already binds Backspace itself (collapses an
  // expanded category first, see its own onKeyDown) and owns all four
  // arrows for grid navigation — wiring both up would double-fire history
  // or fight over the arrow keys. Revisit together when catalog's own
  // keyboard nav gets reworked.
  const [desktopPanel, setDesktopPanel] = useState<'left' | 'right' | null>(null)
  const desktopPanelRef = useRef<HTMLElement>(null)

  useEffect(() => {
    if (!desktopPanel) return
    const first = desktopPanelRef.current?.querySelector('button, a') as HTMLElement | null
    first?.focus()
  }, [desktopPanel])

  useEffect(() => {
    function isTypingTarget(el: Element | null) {
      const tag = el?.tagName
      return tag === 'INPUT' || tag === 'TEXTAREA' || (el as HTMLElement | null)?.isContentEditable
    }

    function onKeyDown(e: KeyboardEvent) {
      // $bp-lg in variables.scss — desktop only, matches the width where
      // the panel isn't already shown via CSS for mobile/tablet.
      if (window.innerWidth <= 1024) return
      if (location.pathname === '/catalog') return
      if (isTypingTarget(document.activeElement)) return

      const panelHasFocus = !!desktopPanelRef.current?.contains(document.activeElement)

      if (e.key === 'Backspace') {
        e.preventDefault()
        setDesktopPanel(null)
        handleBottomBack()
        return
      }

      if (e.key === 'Escape' && desktopPanel) {
        e.preventDefault()
        setDesktopPanel(null)
        return
      }

      if (e.key === 'ArrowRight') {
        if (desktopPanel === 'left') { e.preventDefault(); setDesktopPanel(null); return }
        if (!desktopPanel) { e.preventDefault(); setDesktopPanel('right'); return }
      }
      if (e.key === 'ArrowLeft') {
        if (desktopPanel === 'right') { e.preventDefault(); setDesktopPanel(null); return }
        if (!desktopPanel) { e.preventDefault(); setDesktopPanel('left'); return }
      }

      if (panelHasFocus && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
        e.preventDefault()
        const focusable = Array.from(desktopPanelRef.current?.querySelectorAll('button, a') ?? []) as HTMLElement[]
        const i = focusable.indexOf(document.activeElement as HTMLElement)
        const next = Math.min(Math.max(i + (e.key === 'ArrowDown' ? 1 : -1), 0), focusable.length - 1)
        focusable[next]?.focus()
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [location.pathname, desktopPanel]) // eslint-disable-line react-hooks/exhaustive-deps

  function handleDesktopPanelBlur(e: React.FocusEvent<HTMLElement>) {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) {
      setDesktopPanel(null)
    }
  }

  const links = (
    <>
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
      <NavLink to="/profiles" className={linkClass} onClick={() => setMenuOpen(false)}>Настройки</NavLink>
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

      <main className={`${styles.main}${wide ? ' ' + styles.mainWide : ''}${navPosition === 'right' ? ' ' + styles.mainRightNav : ''}${navPosition === 'left' ? ' ' + styles.mainLeftNav : ''}`}>{children}</main>

      <input ref={searchWarmupRef} type="text" aria-hidden="true" tabIndex={-1} className={styles.searchWarmup} />

      <BottomNav items={bottomNavItems} position={navPosition} />

      {desktopPanel && (
        <BottomNav
          items={bottomNavItems}
          position={desktopPanel}
          forceShow
          navRef={desktopPanelRef}
          onBlur={handleDesktopPanelBlur}
        />
      )}
    </div>
  )
}
