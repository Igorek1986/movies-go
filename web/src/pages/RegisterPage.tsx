import { useState, FormEvent } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import styles from './AuthPage.module.scss'
import PasswordInput from '@/components/PasswordInput'

export default function RegisterPage() {
  const nav = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [password2, setPassword2] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (password !== password2) {
      setError('Пароли не совпадают')
      return
    }
    setLoading(true)
    try {
      const res = await fetch('/api/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })
      if (res.ok) {
        nav('/', { replace: true })
      } else {
        const data = await res.json().catch(() => ({}))
        setError(data.error || 'Ошибка регистрации')
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
        <h1 className={styles.title}>Регистрация</h1>

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

        <div className={styles.field}>
          <span>Пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="new-password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            disabled={loading}
            minLength={6}
            required
          />
          {password.length > 0 && password.length < 6 && (
            <span style={{ fontSize: '0.75rem', color: 'var(--color-danger, #e05252)' }}>минимум 6 символов</span>
          )}
        </div>

        <div className={styles.field}>
          <span>Повторите пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="new-password"
            value={password2}
            onChange={e => setPassword2(e.target.value)}
            disabled={loading}
            required
          />
          {password2.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: password === password2 ? 'var(--color-success, #4caf50)' : 'var(--color-danger, #e05252)' }}>
              {password === password2 ? 'Пароли совпадают' : 'Пароли не совпадают'}
            </span>
          )}
        </div>

        <button className={styles.btn} type="submit" disabled={loading}>
          {loading ? 'Регистрация…' : 'Создать аккаунт'}
        </button>

        <p className={styles.hint}>
          Уже есть аккаунт? <Link to="/login">Войти</Link>
        </p>
      </form>
    </div>
  )
}
