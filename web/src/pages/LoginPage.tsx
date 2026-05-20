import { useState, FormEvent } from 'react'
import { useNavigate, Link, useSearchParams } from 'react-router-dom'
import styles from './AuthPage.module.scss'
import PasswordInput from '@/components/PasswordInput'

export default function LoginPage() {
  const nav = useNavigate()
  const [searchParams] = useSearchParams()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const success = searchParams.get('success')

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        if (data.requires_2fa) {
          nav(`/verify-2fa?t=${data.pending_token}`, { replace: true })
        } else {
          nav('/', { replace: true })
        }
      } else {
        setError(data.error || 'Ошибка входа')
      }
    } catch {
      setError('Нет соединения с сервером')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className={styles.page}>
      <form className={styles.card} onSubmit={submit} noValidate>
        <h1 className={styles.title}>Вход</h1>

        {success === 'password_reset' && (
          <p className={styles.success}>Пароль успешно изменён. Войдите с новым паролем.</p>
        )}
        {error && <p className={styles.error}>{error}</p>}

        <label className={styles.field}>
          <span>Имя пользователя</span>
          <input
            className={styles.input}
            type="text"
            autoComplete="username"
            value={username}
            onChange={e => setUsername(e.target.value)}
            disabled={loading}
            required
          />
        </label>

        <label className={styles.field}>
          <span>Пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="current-password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            disabled={loading}
            required
          />
        </label>

        <button className={styles.btn} type="submit" disabled={loading}>
          {loading ? 'Вход…' : 'Войти'}
        </button>

        <p className={styles.hint}>
          Нет аккаунта? <Link to="/register">Зарегистрироваться</Link>
        </p>
        <p className={styles.hint}>
          <Link to="/forgot-password">Забыли пароль?</Link>
        </p>
      </form>
    </div>
  )
}
