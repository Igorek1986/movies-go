import { useState, useEffect } from 'react'

interface User {
  id: number
  username: string
  role: string
  is_admin: boolean
  totp_enabled: boolean
  must_change_password: boolean
  backup_codes_count: number
  bottom_nav_keys?: string[] | null
  bottom_nav_position?: string | null
  card_layout?: string | null
  browse_layout?: string | null
  settings_layout?: string | null
  theme?: string | null
}

interface AuthState {
  user: User | null
  loading: boolean
}

// Последний известный пользователь — переживает ремаунт компонента (каждый
// Layout вызывает useAuth заново). Без него на каждом ремаунте страницы (в т.ч.
// при pull-to-refresh) верхняя/нижняя панели на миг собирались с дефолтами,
// а потом перестраивались под настройки аккаунта — заметное мигание.
// Свежий /api/me всё равно запрашивается и перекрывает его.
let lastUser: User | null = null

export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>(() => ({ user: lastUser, loading: lastUser === null }))

  useEffect(() => {
    fetch('/api/me')
      .then(r => r.ok ? r.json() : null)
      .then(user => { lastUser = user; setState({ user, loading: false }) })
      .catch(() => { lastUser = null; setState({ user: null, loading: false }) })
  }, [])

  return state
}
