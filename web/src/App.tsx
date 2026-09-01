import { useEffect, useRef, lazy, Suspense } from 'react'
import { Routes, Route, Navigate, Outlet, Link, useLocation, useNavigationType } from 'react-router-dom'

// Skipped on POP (browser back/forward) and on Layout's own handleBottomBack
// (Backspace / the nav panel's "Назад", marked via state.isBack — see its
// comment): both are "going back" to a page that may be restoring its own
// scroll/focus (e.g. CatalogPage's expanded category), even though
// handleBottomBack's nav(backUrl) is a PUSH as far as React Router's own
// navigationType is concerned, not a POP. This effect is a plain useEffect,
// which always runs after any useLayoutEffect in the same commit (React
// runs layout effects, children-before-parents, before any passive effects,
// regardless of tree depth), so unconditionally resetting to (0,0) here
// silently overwrote a same-commit restore no matter which page owned it.
function ScrollToTop() {
  const location = useLocation()
  const navType = useNavigationType()
  useEffect(() => {
    if (navType === 'POP') return
    if ((location.state as { isBack?: boolean } | null)?.isBack) return
    window.scrollTo(0, 0)
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps
  return null
}
import { useAuth } from '@/hooks/useAuth'
import { ActiveProfileProvider } from '@/contexts/ActiveProfileContext'
import { setImgProxy } from '@/utils/poster'
import { setWatchedThreshold } from '@/utils/config'
import footerStyles from '@/components/AppFooter.module.scss'

import LoginPage from '@/pages/LoginPage'
import CatalogPage from '@/pages/CatalogPage'
import CalendarPage from '@/pages/CalendarPage'
import MediaLibraryPage from '@/pages/MediaLibraryPage'
import HistoryPage from '@/pages/HistoryPage'
import CardDetailPage from '@/pages/CardDetailPage'
import AdminPage from '@/pages/AdminPage'
import NotFoundPage from '@/pages/NotFoundPage'

// One-off / rarely-revisited pages (auth flows, admin sub-pages, static
// pages): split into their own chunks (loaded on demand) instead of
// bloating the bundle every visitor downloads up front.
const RegisterPage = lazy(() => import('@/pages/RegisterPage'))
// Roughly doubles in source size across its two views (Classic + Remote) —
// see utils/settingsLayout.ts — no reason to ship it in the main bundle.
const ProfilesPage = lazy(() => import('@/pages/ProfilesPage'))
const SessionsPage = lazy(() => import('@/pages/SessionsPage'))
const StatsPage = lazy(() => import('@/pages/StatsPage'))
const Setup2FAPage = lazy(() => import('@/pages/Setup2FAPage'))
const Verify2FAPage = lazy(() => import('@/pages/Verify2FAPage'))
const ForgotPasswordPage = lazy(() => import('@/pages/ForgotPasswordPage'))
const ResetPasswordPage = lazy(() => import('@/pages/ResetPasswordPage'))
const RegisterSuccessPage = lazy(() => import('@/pages/RegisterSuccessPage'))
const ActorPage = lazy(() => import('@/pages/ActorPage'))
const StaticPage = lazy(() => import('@/pages/StaticPage'))
const TgMiniAppPage = lazy(() => import('@/pages/TgMiniAppPage'))
const AdminSettingsPage = lazy(() => import('@/pages/AdminSettingsPage'))
const ParsersPage = lazy(() => import('@/pages/ParsersPage'))
const ProxiesPage = lazy(() => import('@/pages/ProxiesPage'))
const LogsPage = lazy(() => import('@/pages/LogsPage'))
const BotPage = lazy(() => import('@/pages/BotPage'))
const TMDBMissingPage = lazy(() => import('@/pages/TMDBMissingPage'))
const NewCardsPage = lazy(() => import('@/pages/NewCardsPage'))
const AllCardsPage = lazy(() => import('@/pages/AllCardsPage'))
const PopularPage = lazy(() => import('@/pages/PopularPage'))
const PopularSourcePage = lazy(() => import('@/pages/PopularSourcePage'))
const PersonsAdminPage = lazy(() => import('@/pages/PersonsAdminPage'))
const UsersTodayPage = lazy(() => import('@/pages/UsersTodayPage'))
const DevicesTodayPage = lazy(() => import('@/pages/DevicesTodayPage'))
const TimecodesTodayPage = lazy(() => import('@/pages/TimecodesTodayPage'))
const TMDBRefreshedTodayPage = lazy(() => import('@/pages/TMDBRefreshedTodayPage'))
const UsersListPage = lazy(() => import('@/pages/UsersListPage'))
const DevicesListPage = lazy(() => import('@/pages/DevicesListPage'))
const TimecodesListPage = lazy(() => import('@/pages/TimecodesListPage'))

// One shared parent route for every authenticated page (react-router "layout
// route" — matched children render via <Outlet/>, and THIS component stays
// mounted across navigation between them, only unmounting when leaving the
// private route tree entirely). ActiveProfileProvider lives here, not
// wrapping the whole app (see App's own history) — mounted above the public/
// private split, its one-shot refresh() fired /api/devices before login even
// happened (an anonymous 401), and since it never remounts on the client-side
// navigate() a successful login does, that failed result stuck around for
// the rest of the session: devices/profiles empty, and Каталог's category
// rows stuck fetching with no token/profile_id forever — needing a real page
// reload to get a fresh mount with the cookie already present. Scoping it to
// only the routes that are gated on `user` already being resolved means its
// first-ever mount always has a valid session.
function PrivateShell() {
  const { user, loading } = useAuth()
  if (loading) return null
  if (!user) return <Navigate to="/login" replace />
  return (
    <ActiveProfileProvider>
      <Outlet />
    </ActiveProfileProvider>
  )
}

function CatalogCategoryRedirect() {
  return <Navigate to="/catalog" replace />
}

const FOOTER_HIDDEN = ['/consent', '/privacy']

function AppFooter() {
  const { pathname } = useLocation()
  const ref = useRef<HTMLElement>(null)
  const hidden = FOOTER_HIDDEN.includes(pathname)

  // Published as --app-footer-h so CatalogPage/MediaLibraryPage's hero
  // carousel (@mixin page-locked) can reserve exactly this much room instead
  // of either hiding the footer or leaving a residual scroll to reach it —
  // measured rather than hardcoded since it reflows with viewport width.
  useEffect(() => {
    if (hidden) {
      document.documentElement.style.setProperty('--app-footer-h', '0px')
      return
    }
    const el = ref.current
    if (!el) return
    const set = () => document.documentElement.style.setProperty('--app-footer-h', `${el.offsetHeight}px`)
    set()
    const ro = new ResizeObserver(set)
    ro.observe(el)
    return () => ro.disconnect()
  }, [hidden])

  if (hidden) return null
  return (
    <footer ref={ref} className={footerStyles.footer}>
      <a href="https://www.themoviedb.org/" target="_blank" rel="noopener noreferrer" className={footerStyles.tmdbLink}>
        <img src="/static/tmdb-logo.svg" alt="TMDB" className={footerStyles.tmdbLogo} />
      </a>
      <span className={footerStyles.sep}>·</span>
      <span className={footerStyles.attribution}>
        Сайт использует API TMDB, но не одобрен и не сертифицирован TMDB.
      </span>
      <span className={footerStyles.sep}>·</span>
      <Link to="/privacy" className={footerStyles.link}>Политика обработки персональных данных</Link>
      <span className={footerStyles.sep}>·</span>
      <Link to="/consent" className={footerStyles.link}>Согласие на обработку персональных данных</Link>
    </footer>
  )
}

export default function App() {
  useEffect(() => {
    fetch('/api/config')
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (d?.image_proxy_url) setImgProxy(d.image_proxy_url)
        if (d?.watched_threshold) setWatchedThreshold(d.watched_threshold)
      })
      .catch(() => {})
  }, [])

  return (
    <>
      <ScrollToTop />
      <div style={{ flex: 1 }}>
      <Suspense fallback={null}>
      <Routes>
      {/* Публичные */}
      <Route path="/consent" element={<StaticPage name="consent" />} />
      <Route path="/privacy"  element={<StaticPage name="privacy"  />} />
      <Route path="/tg-app"   element={<TgMiniAppPage />} />
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route path="/register-success" element={<RegisterSuccessPage />} />
      <Route path="/verify-2fa" element={<Verify2FAPage />} />
      <Route path="/forgot-password" element={<ForgotPasswordPage />} />
      <Route path="/reset-password" element={<ResetPasswordPage />} />

      {/* Приватные — общий родитель PrivateShell (см. её комментарий) держит
          ActiveProfileProvider смонтированным один раз на всё время работы с
          приватными страницами, не раньше и не позже. */}
      <Route element={<PrivateShell />}>
        <Route path="/" element={<Navigate to="/catalog" replace />} />
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="/catalog" element={<CatalogPage />} />
        <Route path="/catalog/:category" element={<CatalogCategoryRedirect />} />
        <Route path="/calendar" element={<CalendarPage />} />
        <Route path="/media-library" element={<MediaLibraryPage />} />
        <Route path="/history" element={<HistoryPage />} />
        <Route path="/card/:cardId" element={<CardDetailPage />} />
        <Route path="/admin" element={<AdminPage />} />
        <Route path="/sessions" element={<SessionsPage />} />
        <Route path="/stats" element={<StatsPage />} />
        <Route path="/setup-2fa" element={<Setup2FAPage />} />
        <Route path="/actor/:personId" element={<ActorPage />} />
        <Route path="/admin/settings" element={<AdminSettingsPage />} />
        <Route path="/admin/parsers" element={<ParsersPage />} />
        <Route path="/admin/proxies" element={<ProxiesPage />} />
        <Route path="/admin/logs" element={<LogsPage />} />
        <Route path="/admin/bot" element={<BotPage />} />
        <Route path="/admin/tmdb-missing" element={<TMDBMissingPage />} />
        <Route path="/admin/cards-today" element={<NewCardsPage />} />
        <Route path="/admin/all-cards" element={<AllCardsPage />} />
        <Route path="/admin/popular" element={<PopularPage />} />
        <Route path="/admin/popular-source" element={<PopularSourcePage />} />
        <Route path="/admin/no-runtime-movies" element={<AllCardsPage noRuntime="movie" />} />
        <Route path="/admin/no-runtime-tv" element={<AllCardsPage noRuntime="tv" />} />
        <Route path="/admin/actors" element={<PersonsAdminPage />} />
        <Route path="/admin/directors" element={<PersonsAdminPage />} />
        <Route path="/admin/users-today" element={<UsersTodayPage />} />
        <Route path="/admin/devices-today" element={<DevicesTodayPage />} />
        <Route path="/admin/timecodes-today" element={<TimecodesTodayPage />} />
        <Route path="/admin/tmdb-refreshed-today" element={<TMDBRefreshedTodayPage />} />
        <Route path="/admin/users-list" element={<UsersListPage />} />
        <Route path="/admin/devices-list" element={<DevicesListPage />} />
        <Route path="/admin/timecodes-list" element={<TimecodesListPage />} />
      </Route>

      <Route path="*" element={<NotFoundPage />} />
      </Routes>
      </Suspense>
      </div>
      <AppFooter />
    </>
  )
}
