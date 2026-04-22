import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import styles from './ProfilesPage.module.scss'

interface Device {
  id: number
  name: string
  created_at: string
}

export default function ProfilesPage() {
  const { user } = useAuth()
  const nav = useNavigate()
  const [devices, setDevices] = useState<Device[]>([])
  const [pairingCode, setPairingCode] = useState('')
  const [linkCode, setLinkCode] = useState('')
  const [linkName, setLinkName] = useState('')
  const [linkError, setLinkError] = useState('')
  const [linkLoading, setLinkLoading] = useState(false)
  const [renameId, setRenameId] = useState<number | null>(null)
  const [renameName, setRenameName] = useState('')

  async function fetchDevices() {
    const res = await fetch('/api/devices')
    if (res.ok) setDevices(await res.json())
  }

  useEffect(() => { fetchDevices() }, [])

  async function handleGetCode() {
    const res = await fetch('/device/code')
    if (res.ok) {
      const data = await res.json()
      setPairingCode(data.code)
    }
  }

  async function handleLink(e: React.FormEvent) {
    e.preventDefault()
    setLinkError('')
    setLinkLoading(true)
    try {
      const res = await fetch('/api/device/link', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: linkCode, name: linkName || 'Lampa' }),
      })
      if (res.ok) {
        setLinkCode('')
        setLinkName('')
        setPairingCode('')
        fetchDevices()
      } else {
        const d = await res.json().catch(() => ({}))
        setLinkError(d.error || 'Ошибка привязки')
      }
    } finally {
      setLinkLoading(false)
    }
  }

  async function handleDelete(id: number) {
    if (!confirm('Удалить устройство?')) return
    await fetch(`/api/devices/${id}`, { method: 'DELETE' })
    fetchDevices()
  }

  async function handleRename(id: number) {
    if (!renameName.trim()) return
    await fetch(`/api/devices/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: renameName }),
    })
    setRenameId(null)
    fetchDevices()
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Устройства</h1>
        <button className={styles.btnSecondary} onClick={() => nav('/logout')}>Выйти</button>
      </header>

      {/* Device list */}
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Мои устройства</h2>
        {devices.length === 0 && (
          <p className={styles.empty}>Нет привязанных устройств</p>
        )}
        <ul className={styles.deviceList}>
          {devices.map(d => (
            <li key={d.id} className={styles.deviceItem}>
              {renameId === d.id ? (
                <div className={styles.renameRow}>
                  <input
                    className={styles.input}
                    value={renameName}
                    onChange={e => setRenameName(e.target.value)}
                    autoFocus
                  />
                  <button className={styles.btnPrimary} onClick={() => handleRename(d.id)}>Сохранить</button>
                  <button className={styles.btnSecondary} onClick={() => setRenameId(null)}>Отмена</button>
                </div>
              ) : (
                <>
                  <span className={styles.deviceName}>{d.name}</span>
                  <span className={styles.deviceDate}>
                    {new Date(d.created_at).toLocaleDateString('ru-RU')}
                  </span>
                  <div className={styles.deviceActions}>
                    <button className={styles.btnIcon} onClick={() => { setRenameId(d.id); setRenameName(d.name) }} title="Переименовать">✏️</button>
                    <button className={styles.btnIcon} onClick={() => handleDelete(d.id)} title="Удалить">🗑️</button>
                  </div>
                </>
              )}
            </li>
          ))}
        </ul>
      </section>

      {/* Pairing section */}
      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Привязать Lampa</h2>
        <p className={styles.hint}>
          Откройте Lampa → Настройки → Аккаунт → Подключить. На экране появится код — введите его ниже.
        </p>

        <form className={styles.linkForm} onSubmit={handleLink}>
          {linkError && <p className={styles.error}>{linkError}</p>}
          <div className={styles.formRow}>
            <input
              className={styles.input}
              placeholder="Код из Lampa (6 цифр)"
              value={linkCode}
              onChange={e => setLinkCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              maxLength={6}
              required
            />
            <input
              className={styles.input}
              placeholder="Название устройства (необязательно)"
              value={linkName}
              onChange={e => setLinkName(e.target.value)}
            />
          </div>
          <button className={styles.btnPrimary} type="submit" disabled={linkLoading || linkCode.length < 6}>
            {linkLoading ? 'Привязка…' : 'Привязать'}
          </button>
        </form>

        <div className={styles.divider}>или</div>

        <p className={styles.hint}>Если Lampa ждёт код на экране — нажмите кнопку и покажите код в приложении:</p>
        <button className={styles.btnSecondary} onClick={handleGetCode}>
          Показать код для Lampa
        </button>
        {pairingCode && (
          <div className={styles.code}>{pairingCode}</div>
        )}
      </section>

      <div className={styles.userInfo}>
        Вы вошли как <strong>{user?.username}</strong> ({user?.role})
      </div>
    </div>
  )
}
