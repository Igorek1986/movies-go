import { useState, useEffect, FormEvent } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './AuthPage.module.scss'

interface SetupData {
  qr_data_url: string
  secret: string
}

export default function Setup2FAPage() {
  const [setup, setSetup] = useState<SetupData | null>(null)
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [backupCodes, setBackupCodes] = useState<string[] | null>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    fetch('/api/setup-2fa')
      .then(r => r.json())
      .then(setSetup)
      .catch(() => setError('Ошибка загрузки'))
  }, [])

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const res = await fetch('/api/setup-2fa', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code }),
      })
      const data = await res.json()
      if (res.ok) {
        setBackupCodes(data.backup_codes)
      } else {
        setError(data.error === 'invalid code' ? 'Неверный код — попробуйте ещё раз' : (data.error || 'Ошибка'))
      }
    } catch {
      setError('Нет соединения')
    } finally {
      setLoading(false)
    }
  }

  function copySecret() {
    if (setup) navigator.clipboard.writeText(setup.secret).catch(() => {})
  }

  function copyAllCodes() {
    if (!backupCodes) return
    navigator.clipboard.writeText(backupCodes.join('\n')).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }).catch(() => {})
  }

  if (backupCodes) {
    return (
      <Layout>
        <div className={styles.page} style={{ minHeight: 'auto', paddingTop: '3rem' }}>
          <div className={styles.card} style={{ maxWidth: 440 }}>
            <h1 className={styles.title}>2FA включена</h1>
            <p className={styles.success}>Двухфакторная аутентификация успешно активирована.</p>
            <p className={styles.backupTitle}>Резервные коды — сохраните их в надёжном месте:</p>
            <div className={styles.backupGrid}>
              {backupCodes.map(c => (
                <span key={c} className={styles.backupCode}>{c}</span>
              ))}
            </div>
            <button className={styles.btnSecondary} onClick={copyAllCodes}>
              {copied ? '✓ Скопировано' : 'Копировать все'}
            </button>
            <p className={styles.hint}>
              <Link to="/profiles">← Вернуться в профиль</Link>
            </p>
          </div>
        </div>
      </Layout>
    )
  }

  return (
    <Layout>
      <div className={styles.page} style={{ minHeight: 'auto', paddingTop: '3rem' }}>
        <form className={styles.card} style={{ maxWidth: 440 }} onSubmit={submit} noValidate>
          <h1 className={styles.title}>Двухфакторная аутентификация</h1>
          <p className={styles.subtitle}>Сканируйте QR-код в приложении-аутентификаторе</p>

          {error && <p className={styles.error}>{error}</p>}

          {setup?.qr_data_url && (
            <div className={styles.qrWrap}>
              <img src={setup.qr_data_url} alt="QR-код для 2FA" />
            </div>
          )}

          {setup?.secret && (
            <details className={styles.secretBlock}>
              <summary>Ввести ключ вручную</summary>
              <div className={styles.secretBox}>{setup.secret}</div>
              <button type="button" className={styles.btnSecondary} onClick={copySecret}>
                Копировать ключ
              </button>
            </details>
          )}

          <label className={styles.field}>
            <span>Введите 6-значный код из приложения</span>
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

          <button className={styles.btn} type="submit" disabled={loading || code.length < 6}>
            {loading ? 'Проверка…' : 'Подтвердить и включить 2FA'}
          </button>

          <p className={styles.hint}>
            <Link to="/profiles">Отмена</Link>
          </p>
        </form>
      </div>
    </Layout>
  )
}
