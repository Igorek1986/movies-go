import { useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { NavLink, useNavigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { ProfileSwitcher } from '@/components/ProfileSwitcher'
import { BottomNav, BOTTOM_NAV_ICONS, SearchIcon, type BottomNavItem } from '@/components/BottomNav'
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

  // Desktop keyboard nav (site-wide rollout, in progress): Backspace =
  // "Назад" everywhere (replaces the old per-page floating back button),
  // ArrowRight/ArrowLeft summon this same panel as a floating side pill so
  // desktop users get one without a mouse. Skipped on /catalog: it already
  // binds Backspace itself (collapses an expanded category first, see its
  // own onKeyDown) and owns all four arrows for grid navigation — wiring
  // both up would double-fire history or fight over the arrow keys.
  // (Top-menu Left/Right/Down navigation is handled below and, unlike this
  // panel, is NOT skipped on /catalog — see the onTopNav branch.)
  const [desktopPanel, setDesktopPanel] = useState<'left' | 'right' | null>(null)
  const desktopPanelRef = useRef<HTMLElement>(null)
  // Where keyboard focus was right before the panel opened — restored when
  // it closes via the opposite arrow or Escape, so the row-edge bridge
  // (CardDetailPage) feels like a detour rather than a one-way trip.
  const preOpenFocusRef = useRef<HTMLElement | null>(null)
  // Mirrors desktopPanel/location.pathname for the keydown effect below,
  // which must never re-subscribe (see the long comment there) — updated by
  // plain assignment during render instead of via that effect's own deps.
  const desktopPanelStateRef = useRef(desktopPanel)
  desktopPanelStateRef.current = desktopPanel
  const pathnameRef = useRef(location.pathname)
  pathnameRef.current = location.pathname

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
      if (isTypingTarget(document.activeElement)) return

      const activeEl = document.activeElement as HTMLElement | null
      const onTopNav = !!activeEl?.closest('[data-top-nav]')

      // Top menu (Каталог/Календарь/...): Left/Right moves between links,
      // works on every page — part of the site-wide keyboard nav being
      // rolled out incrementally, starting here + the catalog grid/search
      // entry point (see CatalogPage). Down is deliberately left alone (no
      // preventDefault) so a page-specific handler can decide what "back
      // into the page" means — on /catalog that's its own onKeyDown's
      // "focus isn't on a card → jump to the first one" fallback.
      if (onTopNav && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
        e.preventDefault()
        e.stopImmediatePropagation()
        const topLinks = Array.from(document.querySelectorAll<HTMLElement>('[data-top-nav] a, [data-top-nav] [data-top-nav-search]'))
        const i = topLinks.indexOf(activeEl!)
        const next = Math.min(Math.max(i + (e.key === 'ArrowRight' ? 1 : -1), 0), topLinks.length - 1)
        topLinks[next]?.focus()
        return
      }
      if (onTopNav && e.key === 'ArrowDown') return

      // Everything below is skipped on /catalog: it already binds Backspace
      // itself (collapses an expanded category first) and owns all four
      // arrows for its own grid navigation — wiring both up would
      // double-fire history or fight over the arrow keys.
      if (pathnameRef.current === '/catalog') return

      const panelHasFocus = !!desktopPanelRef.current?.contains(document.activeElement)
      // A page can opt into owning Left/Right itself by marking its rows
      // with data-row-id + data-nav-item (see CardDetailPage) — skip
      // summoning the panel while focus is inside one, EXCEPT exactly at
      // its outward edge (leftmost item + Left, rightmost + Right), which
      // bridges to the panel instead — same idea as ArrowUp from the first
      // row bridging to the top menu. This runs before the page's own
      // handler (Layout's effect commits first), so it has to work out the
      // edge case itself from the same data-row-id/data-nav-item markup
      // rather than relying on the page to have already decided.
      const row = activeEl?.closest<HTMLElement>('[data-row-id]') ?? null
      let onPageRowNav = false
      if (row) {
        // The progress bar's own scrub (Left/Right adjusts the value, see
        // CardDetailPage) always owns these keys — as a single-item row
        // it'd otherwise look like it's permanently "at both edges" and
        // bridge to the panel on every press instead of scrubbing.
        if (activeEl?.hasAttribute('data-progress-slider')) {
          onPageRowNav = true
        } else {
          const items = Array.from(row.querySelectorAll<HTMLElement>('[data-nav-item]'))
          const idx = items.indexOf(activeEl!)
          const atOuterEdge = idx !== -1 &&
            ((e.key === 'ArrowLeft' && idx === 0) || (e.key === 'ArrowRight' && idx === items.length - 1))
          onPageRowNav = !atOuterEdge
        }
      }

      // Every branch below that takes an action also calls
      // stopImmediatePropagation — preventDefault alone only suppresses the
      // browser's own default behavior, it does NOT stop a page's own
      // window keydown listener (e.g. CardDetailPage's row nav) from also
      // firing for the same event. Without it, closing the panel here and
      // restoring focus to a row item was immediately followed, same
      // keypress, by that page's handler ALSO moving focus one more step —
      // each open/close silently ate an extra Left/Right press.
      if (e.key === 'Backspace') {
        e.preventDefault()
        e.stopImmediatePropagation()
        setDesktopPanel(null)
        handleBottomBack()
        return
      }

      if (e.key === 'Escape' && desktopPanelStateRef.current) {
        e.preventDefault()
        e.stopImmediatePropagation()
        setDesktopPanel(null)
        preOpenFocusRef.current?.focus()
        return
      }

      if (e.key === 'ArrowRight') {
        if (desktopPanelStateRef.current === 'left') { e.preventDefault(); e.stopImmediatePropagation(); setDesktopPanel(null); preOpenFocusRef.current?.focus(); return }
        if (!desktopPanelStateRef.current && !onPageRowNav) { e.preventDefault(); e.stopImmediatePropagation(); preOpenFocusRef.current = activeEl; setDesktopPanel('right'); return }
      }
      if (e.key === 'ArrowLeft') {
        if (desktopPanelStateRef.current === 'right') { e.preventDefault(); e.stopImmediatePropagation(); setDesktopPanel(null); preOpenFocusRef.current?.focus(); return }
        if (!desktopPanelStateRef.current && !onPageRowNav) { e.preventDefault(); e.stopImmediatePropagation(); preOpenFocusRef.current = activeEl; setDesktopPanel('left'); return }
      }

      if (panelHasFocus && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
        e.preventDefault()
        e.stopImmediatePropagation()
        const focusable = Array.from(desktopPanelRef.current?.querySelectorAll('button, a') ?? []) as HTMLElement[]
        const i = focusable.indexOf(document.activeElement as HTMLElement)
        const next = Math.min(Math.max(i + (e.key === 'ArrowDown' ? 1 : -1), 0), focusable.length - 1)
        focusable[next]?.focus()
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
    // Deliberately []: this must subscribe exactly once and never
    // re-subscribe. addEventListener preserves registration order, and
    // CardDetailPage (also a window keydown listener, added once on ITS
    // mount) never moves — but this effect used to list desktopPanel as a
    // dep, so opening/closing the panel re-ran it, which REMOVES the old
    // listener and ADDS a new one at the END of the list. After that first
    // toggle, CardDetailPage's listener — added before this one existed in
    // its new slot — started firing FIRST instead of second. That meant
    // this handler's "was that row edge?" check ran on the DOM state AFTER
    // CardDetailPage had already moved focus for the same keypress, not
    // before — so arriving at index 0 via a normal in-row move (already
    // correctly handled) looked identical to "user is AT the edge, open the
    // panel" and fired a second, unwanted open. current desktopPanel/
    // pathname come from the refs above instead, kept in sync every render.
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

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

        {/* Desktop — data-top-nav wraps both the search button and the link
            pill row so Layout's own Left/Right cycling (below) and the
            ArrowUp bridge from CatalogPage/MediaLibraryPage can both reach
            the button by keyboard, even though it's visually its own item
            (not part of .navLinks' pill group) and not one of the `a` page
            links themselves — see the querySelectorAll below, which matches
            it via data-nav-item instead of tag name. */}
        <div className={styles.navGroup} data-top-nav>
          {/* Catalog search's own field is fixed-position and hidden until
              opened (see CatalogPage's floating bar) — the hero carousel's
              full-viewport backdrop covers normal page content, so this is
              the one entry point that's always reachable regardless of
              layout or which page you're on. data-top-nav-search, not
              data-nav-item — that name is already a site-wide marker (see
              CardDetailPage.module.scss's bare, unscoped
              [data-nav-item]:focus-visible rule, which isn't CSS-Modules-
              scoped and so applies to ANY element with the attribute once
              that stylesheet has loaded) for the generic row-nav rectangle
              outline, which isn't the look wanted here. */}
          <button className={styles.navSearchBtn} data-top-nav-search onClick={handleBottomSearch} aria-label="Поиск" title="Поиск">
            <SearchIcon />
          </button>
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
