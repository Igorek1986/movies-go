import { useState, FormEvent, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import styles from './AuthPage.module.scss'
import PasswordInput from '@/components/PasswordInput'
import { useAuth } from '@/hooks/useAuth'
import { useAppConfig } from '@/hooks/useAppConfig'
import { validatePasswordStrength } from '@/utils/passwordStrength'

// Shown instead of any private page when the account was created by an admin
// (generated login+password, see registration_disabled) and hasn't set its
// own password yet — see PrivateShell in App.tsx, which redirects here.
export default function ForceChangePasswordPage() {
  const nav = useNavigate()
  const { user, loading } = useAuth()
  const { config } = useAppConfig()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [newPassword2, setNewPassword2] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (loading) return
    if (!user) nav('/login', { replace: true })
    else if (!user.must_change_password) nav('/catalog', { replace: true })
  }, [loading, user, nav])

  const newPasswordError = validatePasswordStrength(newPassword, config?.password_blocklist)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (newPasswordError) {
      setError(newPasswordError)
      return
    }
    if (newPassword !== newPassword2) {
      setError('Пароли не совпадают')
      return
    }
    setSubmitting(true)
    try {
      const res = await fetch('/api/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      })
      if (res.ok) {
        // Full reload so useAuth re-fetches /api/me with must_change_password cleared.
        window.location.href = '/'
      } else {
        const data = await res.json().catch(() => ({}))
        setError(data.error || 'Ошибка смены пароля')
      }
    } catch {
      setError('Нет соединения с сервером')
    } finally {
      setSubmitting(false)
    }
  }

  async function logout() {
    await fetch('/api/logout', { method: 'POST' }).catch(() => {})
    window.location.href = '/login'
  }

  if (loading || !user || !user.must_change_password) return null

  return (
    <div className={styles.page}>
      <form className={styles.card} onSubmit={submit} noValidate>
        <h1 className={styles.title}>Смена пароля</h1>
        <p className={styles.hint}>
          Это первый вход по паролю, выданному администратором. Придумайте свой пароль, чтобы продолжить.
        </p>

        {error && <p className={styles.error}>{error}</p>}

        <label className={styles.field}>
          <span>Текущий пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="current-password"
            value={currentPassword}
            onChange={e => setCurrentPassword(e.target.value)}
            disabled={submitting}
            required
          />
        </label>

        <div className={styles.field}>
          <span>Новый пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="new-password"
            value={newPassword}
            onChange={e => setNewPassword(e.target.value)}
            disabled={submitting}
            minLength={8}
            required
          />
          {newPassword.length > 0 && newPasswordError && (
            <span style={{ fontSize: '0.75rem', color: 'var(--color-danger, #e05252)' }}>{newPasswordError}</span>
          )}
        </div>

        <div className={styles.field}>
          <span>Повторите новый пароль</span>
          <PasswordInput
            className={styles.input}
            autoComplete="new-password"
            value={newPassword2}
            onChange={e => setNewPassword2(e.target.value)}
            disabled={submitting}
            required
          />
          {newPassword2.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: newPassword === newPassword2 ? 'var(--color-success, #4caf50)' : 'var(--color-danger, #e05252)' }}>
              {newPassword === newPassword2 ? 'Пароли совпадают' : 'Пароли не совпадают'}
            </span>
          )}
        </div>

        <button className={styles.btn} type="submit" disabled={submitting}>
          {submitting ? 'Сохранение…' : 'Сохранить и продолжить'}
        </button>

        <p className={styles.hint}>
          <a href="#" onClick={e => { e.preventDefault(); logout() }}>Выйти</a>
        </p>
      </form>
    </div>
  )
}
