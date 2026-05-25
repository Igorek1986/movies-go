import { useEffect } from 'react'
import { Routes, Route, Navigate, Link, useLocation } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { setImgProxy } from '@/utils/poster'
import footerStyles from '@/components/AppFooter.module.scss'

import LoginPage from '@/pages/LoginPage'
import RegisterPage from '@/pages/RegisterPage'
import ProfilesPage from '@/pages/ProfilesPage'
import CatalogPage from '@/pages/CatalogPage'
import HistoryPage from '@/pages/HistoryPage'
import CardDetailPage from '@/pages/CardDetailPage'
import AdminPage from '@/pages/AdminPage'
import SessionsPage from '@/pages/SessionsPage'
import StatsPage from '@/pages/StatsPage'
import Setup2FAPage from '@/pages/Setup2FAPage'
import Verify2FAPage from '@/pages/Verify2FAPage'
import ForgotPasswordPage from '@/pages/ForgotPasswordPage'
import ResetPasswordPage from '@/pages/ResetPasswordPage'
import RegisterSuccessPage from '@/pages/RegisterSuccessPage'
import NotFoundPage from '@/pages/NotFoundPage'
import ActorPage from '@/pages/ActorPage'
import AdminSettingsPage from '@/pages/AdminSettingsPage'
import ParsersPage from '@/pages/ParsersPage'
import ProxiesPage from '@/pages/ProxiesPage'
import LogsPage from '@/pages/LogsPage'
import StaticPage from '@/pages/StaticPage'
import TgMiniAppPage from '@/pages/TgMiniAppPage'

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
  if (FOOTER_HIDDEN.includes(pathname)) return null
  return (
    <footer className={footerStyles.footer}>
      <a href="https://www.themoviedb.org/" target="_blank" rel="noopener noreferrer">
        <img src="/static/tmdb-logo.svg" alt="TMDB" className={footerStyles.tmdbLogo} />
      </a>
      <p className={footerStyles.attribution}>
        Сайт использует API TMDB, но не одобрен и не сертифицирован TMDB.
      </p>
      <p className={footerStyles.links}>
        <Link to="/privacy" className={footerStyles.link}>Политика обработки персональных данных</Link>
        <span className={footerStyles.sep}>·</span>
        <Link to="/consent" className={footerStyles.link}>Согласие на обработку персональных данных</Link>
      </p>
    </footer>
  )
}

export default function App() {
  useEffect(() => {
    fetch('/api/config')
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d?.image_proxy_url) setImgProxy(d.image_proxy_url) })
      .catch(() => {})
  }, [])

  return (
    <>
      <div style={{ flex: 1 }}>
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

      <Route path="*" element={<NotFoundPage />} />
      </Routes>
      </div>
      <AppFooter />
    </>
  )
}
