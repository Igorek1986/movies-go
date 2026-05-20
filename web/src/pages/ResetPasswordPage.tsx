import { useState, FormEvent } from 'react'
import { Link, useSearchParams, useNavigate } from 'react-router-dom'
import styles from './AuthPage.module.scss'
import PasswordInput from '@/components/PasswordInput'

export default function ResetPasswordPage() {
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const token = searchParams.get('token') ?? ''

  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  if (!token) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <p className={styles.error}>Ссылка недействительна или устарела.</p>
          <p className={styles.hint}><Link to="/forgot-password">Запросить новый код</Link></p>
        </div>
      </div>
    )
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (newPassword !== confirmPassword) {
      setError('Пароли не совпадают')
      return
    }
    if (newPassword.length < 6) {
      setError('Пароль должен быть не короче 6 символов')
      return
    }
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/reset-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, new_password: newPassword }),
      })
      if (res.ok) {
        navigate('/login?success=password_reset', { replace: true })
      } else {
        const data = await res.json().catch(() => ({}))
        setError(data.error === 'invalid or expired code'
          ? 'Неверный или истёкший токен.'
          : (data.error || 'Ошибка'))
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
        <h1 className={styles.title}>Новый пароль</h1>
        <p className={styles.subtitle}>Ссылка действует 15 минут</p>

        {error && <p className={styles.error}>{error}</p>}

        <div className={styles.field}>
          <span>Новый пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="new-password"
            value={newPassword}
            onChange={e => setNewPassword(e.target.value)}
            autoFocus
            minLength={6}
            required
          />
          {newPassword.length > 0 && newPassword.length < 6 && (
            <span style={{ fontSize: '0.75rem', color: 'var(--color-danger, #e05252)' }}>минимум 6 символов</span>
          )}
        </div>

        <div className={styles.field}>
          <span>Повторите пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="new-password"
            value={confirmPassword}
            onChange={e => setConfirmPassword(e.target.value)}
            required
          />
          {confirmPassword.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: newPassword === confirmPassword ? 'var(--color-success, #4caf50)' : 'var(--color-danger, #e05252)' }}>
              {newPassword === confirmPassword ? 'Пароли совпадают' : 'Пароли не совпадают'}
            </span>
          )}
        </div>

        <button
          className={styles.btn}
          type="submit"
          disabled={loading || !newPassword || !confirmPassword}
        >
          {loading ? 'Сохранение…' : 'Сохранить пароль'}
        </button>

        <p className={styles.hint}><Link to="/login">← Вернуться ко входу</Link></p>
      </form>
    </div>
  )
}
