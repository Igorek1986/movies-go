import { useEffect, useRef, lazy, Suspense } from 'react'
import { Routes, Route, Navigate, Link, useLocation, useNavigationType } from 'react-router-dom'

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

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  if (loading) return null
  if (!user) return <Navigate to="/login" replace />
  return <>{children}</>
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
    <ActiveProfileProvider>
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

      {/* Приватные */}
      <Route path="/" element={<PrivateRoute><Navigate to="/catalog" replace /></PrivateRoute>} />
      <Route path="/profiles" element={<PrivateRoute><ProfilesPage /></PrivateRoute>} />
      <Route path="/catalog" element={<PrivateRoute><CatalogPage /></PrivateRoute>} />
      <Route path="/catalog/:category" element={<PrivateRoute><CatalogCategoryRedirect /></PrivateRoute>} />
      <Route path="/calendar" element={<PrivateRoute><CalendarPage /></PrivateRoute>} />
      <Route path="/media-library" element={<PrivateRoute><MediaLibraryPage /></PrivateRoute>} />
      <Route path="/history" element={<PrivateRoute><HistoryPage /></PrivateRoute>} />
      <Route path="/card/:cardId" element={<PrivateRoute><CardDetailPage /></PrivateRoute>} />
      <Route path="/admin" element={<PrivateRoute><AdminPage /></PrivateRoute>} />
      <Route path="/sessions" element={<PrivateRoute><SessionsPage /></PrivateRoute>} />
      <Route path="/stats" element={<PrivateRoute><StatsPage /></PrivateRoute>} />
      <Route path="/setup-2fa" element={<PrivateRoute><Setup2FAPage /></PrivateRoute>} />
      <Route path="/actor/:personId" element={<PrivateRoute><ActorPage /></PrivateRoute>} />
      <Route path="/admin/settings" element={<PrivateRoute><AdminSettingsPage /></PrivateRoute>} />
      <Route path="/admin/parsers" element={<PrivateRoute><ParsersPage /></PrivateRoute>} />
      <Route path="/admin/proxies" element={<PrivateRoute><ProxiesPage /></PrivateRoute>} />
      <Route path="/admin/logs" element={<PrivateRoute><LogsPage /></PrivateRoute>} />
      <Route path="/admin/bot" element={<PrivateRoute><BotPage /></PrivateRoute>} />
      <Route path="/admin/tmdb-missing" element={<PrivateRoute><TMDBMissingPage /></PrivateRoute>} />
      <Route path="/admin/cards-today" element={<PrivateRoute><NewCardsPage /></PrivateRoute>} />
      <Route path="/admin/all-cards" element={<PrivateRoute><AllCardsPage /></PrivateRoute>} />
      <Route path="/admin/popular" element={<PrivateRoute><PopularPage /></PrivateRoute>} />
      <Route path="/admin/popular-source" element={<PrivateRoute><PopularSourcePage /></PrivateRoute>} />
      <Route path="/admin/no-runtime-movies" element={<PrivateRoute><AllCardsPage noRuntime="movie" /></PrivateRoute>} />
      <Route path="/admin/no-runtime-tv" element={<PrivateRoute><AllCardsPage noRuntime="tv" /></PrivateRoute>} />
      <Route path="/admin/actors" element={<PrivateRoute><PersonsAdminPage /></PrivateRoute>} />
      <Route path="/admin/directors" element={<PrivateRoute><PersonsAdminPage /></PrivateRoute>} />
      <Route path="/admin/users-today" element={<PrivateRoute><UsersTodayPage /></PrivateRoute>} />
      <Route path="/admin/devices-today" element={<PrivateRoute><DevicesTodayPage /></PrivateRoute>} />
      <Route path="/admin/timecodes-today" element={<PrivateRoute><TimecodesTodayPage /></PrivateRoute>} />
      <Route path="/admin/tmdb-refreshed-today" element={<PrivateRoute><TMDBRefreshedTodayPage /></PrivateRoute>} />
      <Route path="/admin/users-list" element={<PrivateRoute><UsersListPage /></PrivateRoute>} />
      <Route path="/admin/devices-list" element={<PrivateRoute><DevicesListPage /></PrivateRoute>} />
      <Route path="/admin/timecodes-list" element={<PrivateRoute><TimecodesListPage /></PrivateRoute>} />

      <Route path="*" element={<NotFoundPage />} />
      </Routes>
      </Suspense>
      </div>
      <AppFooter />
    </ActiveProfileProvider>
  )
}
