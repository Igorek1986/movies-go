import { useState, useEffect } from 'react'

interface User {
  id: number
  username: string
  role: string
  is_admin: boolean
  totp_enabled: boolean
  backup_codes_count: number
}

interface AuthState {
  user: User | null
  loading: boolean
}

export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>({ user: null, loading: true })

  useEffect(() => {
    fetch('/api/me')
      .then(r => r.ok ? r.json() : null)
      .then(user => setState({ user, loading: false }))
      .catch(() => setState({ user: null, loading: false }))
  }, [])

  return state
}
