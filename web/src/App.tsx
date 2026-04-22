import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'

import LoginPage from '@/pages/LoginPage'
import RegisterPage from '@/pages/RegisterPage'
import ProfilesPage from '@/pages/ProfilesPage'
import CatalogPage from '@/pages/CatalogPage'
import HistoryPage from '@/pages/HistoryPage'
import AdminPage from '@/pages/AdminPage'
import NotFoundPage from '@/pages/NotFoundPage'

function PrivateRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  if (loading) return null
  if (!user) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      {/* Публичные */}
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />

      {/* Приватные */}
      <Route path="/" element={<PrivateRoute><Navigate to="/profiles" replace /></PrivateRoute>} />
      <Route path="/profiles" element={<PrivateRoute><ProfilesPage /></PrivateRoute>} />
      <Route path="/catalog" element={<PrivateRoute><CatalogPage /></PrivateRoute>} />
      <Route path="/history" element={<PrivateRoute><HistoryPage /></PrivateRoute>} />
      <Route path="/admin" element={<PrivateRoute><AdminPage /></PrivateRoute>} />

      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  )
}
