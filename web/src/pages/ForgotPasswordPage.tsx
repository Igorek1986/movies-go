import { useState, FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import styles from './AuthPage.module.scss'
import PasswordInput from '@/components/PasswordInput'

type Step = 'username' | 'code'

export default function ForgotPasswordPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>('username')
  const [botName, setBotName] = useState('')
  const [username, setUsername] = useState('')
  const [code, setCode] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function submitUsername(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/forgot-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username }),
      })
      const data = await res.json().catch(() => ({}))
      if (res.ok) {
        setBotName(data.bot_name || '')
        setStep('code')
      } else if (res.status === 422) {
        setError('К этому аккаунту не привязан Telegram. Обратитесь в поддержку.')
      } else {
        setError(data.error || 'Ошибка сервера')
      }
    } catch {
      setError('Нет соединения с сервером')
    } finally {
      setLoading(false)
    }
  }

  async function submitCode(e: FormEvent) {
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
        body: JSON.stringify({ token: code, new_password: newPassword }),
      })
      if (res.ok) {
        navigate('/login?success=password_reset', { replace: true })
      } else {
        const data = await res.json().catch(() => ({}))
        setError(data.error === 'invalid or expired code'
          ? 'Неверный или истёкший код. Запросите новый.'
          : (data.error || 'Ошибка'))
      }
    } catch {
      setError('Нет соединения с сервером')
    } finally {
      setLoading(false)
    }
  }

  if (step === 'code') {
    return (
      <div className={styles.page}>
        <form className={styles.card} onSubmit={submitCode} noValidate>
          <h1 className={styles.title}>Введите код из Telegram</h1>
          {botName && (
            <p className={styles.subtitle}>
              Бот @{botName} отправил 6-значный код для @{username}
            </p>
          )}

          {error && <p className={styles.error}>{error}</p>}

          <label className={styles.field}>
            <span>Код из Telegram</span>
            <input
              className={styles.codeInput}
              type="text"
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              placeholder="123456"
              autoComplete="one-time-code"
              value={code}
              onChange={e => setCode(e.target.value.replace(/\D/g, ''))}
              autoFocus
              required
            />
          </label>

          <label className={styles.field}>
            <span>Новый пароль</span>
            <PasswordInput
              className={styles.input}
              autoComplete="new-password"
              value={newPassword}
              onChange={e => setNewPassword(e.target.value)}
              minLength={6}
              required
            />
          </label>

          <label className={styles.field}>
            <span>Повторите пароль</span>
            <PasswordInput
              className={styles.input}
              autoComplete="new-password"
              value={confirmPassword}
              onChange={e => setConfirmPassword(e.target.value)}
              required
            />
          </label>

          <button
            className={styles.btn}
            type="submit"
            disabled={loading || code.length < 6 || !newPassword || !confirmPassword}
          >
            {loading ? 'Сохранение…' : 'Сохранить пароль'}
          </button>

          <p className={styles.hint}>
            <button
              type="button"
              className={styles.btnSecondary}
              style={{ width: '100%' }}
              onClick={() => { setStep('username'); setError(''); setCode('') }}
            >
              ← Запросить новый код
            </button>
          </p>
          <p className={styles.hint}><Link to="/login">← Вернуться ко входу</Link></p>
        </form>
      </div>
    )
  }

  return (
    <div className={styles.page}>
      <form className={styles.card} onSubmit={submitUsername} noValidate>
        <h1 className={styles.title}>Восстановление пароля</h1>
        <p className={styles.subtitle}>
          Введите имя пользователя — код придёт в Telegram-бот
        </p>

        {error && <p className={styles.error}>{error}</p>}

        <label className={styles.field}>
          <span>Имя пользователя</span>
          <input
            className={styles.input}
            type="text"
            autoComplete="username"
            value={username}
            onChange={e => setUsername(e.target.value)}
            autoFocus
            required
          />
        </label>

        <button className={styles.btn} type="submit" disabled={loading || !username}>
          {loading ? 'Отправка…' : 'Получить код'}
        </button>

        <p className={styles.hint}><Link to="/login">← Вернуться ко входу</Link></p>
      </form>
    </div>
  )
}
