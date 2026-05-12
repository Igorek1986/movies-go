import { useState, FormEvent } from 'react'
import { useSearchParams, Link, useNavigate } from 'react-router-dom'
import styles from './AuthPage.module.scss'

export default function Verify2FAPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const token = searchParams.get('t') ?? ''

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!token) return
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/verify-2fa', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, code }),
      })
      const data = await res.json()
      if (res.ok) {
        navigate('/', { replace: true })
        window.location.reload()
      } else {
        setError('Неверный код. Попробуйте ещё раз.')
        setCode('')
        if (data.new_token) {
          setSearchParams({ t: data.new_token })
        }
      }
    } catch {
      setError('Нет соединения')
    } finally {
      setLoading(false)
    }
  }

  if (!token) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <p className={styles.error}>Ссылка недействительна.</p>
          <p className={styles.hint}><Link to="/login">← Войти</Link></p>
        </div>
      </div>
    )
  }

  return (
    <div className={styles.page}>
      <form className={styles.card} onSubmit={submit} noValidate>
        <h1 className={styles.title}>Двухфакторная аутентификация</h1>
        <p className={styles.subtitle}>Введите код из приложения-аутентификатора</p>

        {error && <p className={styles.error}>{error}</p>}

        <label className={styles.field}>
          <span>Код аутентификации</span>
          <input
            className={styles.codeInput}
            type="text"
            inputMode="numeric"
            maxLength={10}
            placeholder="123456"
            autoComplete="one-time-code"
            value={code}
            onChange={e => setCode(e.target.value.replace(/\s/g, ''))}
            autoFocus
            required
          />
        </label>

        <p className={styles.hint} style={{ textAlign: 'left', marginTop: '-0.5rem' }}>
          Также можно ввести резервный код (формат XXXX-XXXX)
        </p>

        <button className={styles.btn} type="submit" disabled={loading || code.length < 6}>
          {loading ? 'Проверка…' : 'Войти'}
        </button>

        <p className={styles.hint}>
          <Link to="/login">← Назад ко входу</Link>
        </p>
      </form>
    </div>
  )
}
