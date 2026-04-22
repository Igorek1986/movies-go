import { useState, FormEvent } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import styles from './AuthPage.module.scss'

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

        <label className={styles.field}>
          <span>Пароль</span>
          <input
            className={styles.input}
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            disabled={loading}
            required
          />
        </label>

        <label className={styles.field}>
          <span>Повторите пароль</span>
          <input
            className={styles.input}
            type="password"
            autoComplete="new-password"
            value={password2}
            onChange={e => setPassword2(e.target.value)}
            disabled={loading}
            required
          />
        </label>

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
