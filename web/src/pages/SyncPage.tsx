import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './SyncPage.module.scss'

interface SyncData {
  peer_url: string
  token: string
  interval_minutes: number
}

interface Toast {
  id: number
  text: string
  ok: boolean
}

export default function SyncPage() {
  const [data, setData] = useState<SyncData | null>(null)
  const [peerUrl, setPeerUrl] = useState('')
  const [token, setToken] = useState('')
  const [intervalMinutes, setIntervalMinutes] = useState(15)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])

  const isHub = peerUrl.trim() === ''

  function toast(text: string, ok = true) {
    const id = Date.now()
    setToasts(prev => [...prev, { id, text, ok }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 3000)
  }

  async function load() {
    setLoading(true)
    try {
      const r = await fetch('/api/admin/sync')
      if (r.ok) {
        const d: SyncData = await r.json()
        setData(d)
        setPeerUrl(d.peer_url)
        setToken(d.token)
        setIntervalMinutes(d.interval_minutes)
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function save() {
    setSaving(true)
    try {
      const r = await fetch('/api/admin/sync', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          peer_url: peerUrl,
          token,
          interval_minutes: intervalMinutes,
        }),
      })
      if (r.ok) {
        toast('Сохранено')
        await load()
      } else {
        const d = await r.json().catch(() => ({}))
        toast(d.error || 'Ошибка сохранения', false)
      }
    } finally {
      setSaving(false)
    }
  }

  async function generateToken() {
    if (token && !confirm('Сгенерировать новый токен? Старый перестанет работать у всех спутников, которые его используют.')) return
    const r = await fetch('/api/admin/sync/token', { method: 'POST' })
    if (r.ok) {
      const d = await r.json()
      setToken(d.token)
      setData(prev => prev ? { ...prev, token: d.token } : prev)
      toast('Токен сгенерирован')
    } else {
      toast('Не удалось сгенерировать токен', false)
    }
  }

  function copyToken() {
    navigator.clipboard?.writeText(token).then(() => toast('Скопировано'), () => toast('Не удалось скопировать', false))
  }

  const dirty = data !== null && (
    peerUrl !== data.peer_url ||
    token !== data.token ||
    intervalMinutes !== data.interval_minutes
  )

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Синхронизация между инстансами</h1>
          <div className={styles.headerActions}>
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        <p className={styles.desc}>
          Одно отношение — этот инстанс и один URL. Пустой URL — этот инстанс главный, к нему
          обращаются остальные. Указан URL — этот инстанс спутник: сам забирает оттуда карточки и
          play-события и сам же присылает туда свои (по токену). Читать данные (GET) можно всегда,
          у кого угодно — это открытая, безопасная операция, как <code>/np_popular</code> сегодня.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && (
          <>
            <div className={styles.section}>
              <h2 className={styles.sectionTitle}>{isHub ? 'Главный инстанс' : 'Спутник'}</h2>

              <div className={styles.fieldsRow}>
                <label className={styles.field}>
                  <span>URL главного инстанса (пусто — этот инстанс и есть главный)</span>
                  <input
                    type="text"
                    className={styles.input}
                    placeholder="https://example.com"
                    value={peerUrl}
                    onChange={e => setPeerUrl(e.target.value)}
                  />
                </label>
                {!isHub && (
                  <label className={styles.field}>
                    <span>Интервал (мин)</span>
                    <input
                      type="number"
                      min={1}
                      className={styles.inputNarrow}
                      value={intervalMinutes}
                      onChange={e => setIntervalMinutes(Math.max(1, parseInt(e.target.value) || 15))}
                    />
                  </label>
                )}
              </div>

              <div className={styles.field}>
                <span>
                  {isHub
                    ? 'Токен — сгенерируйте и вставьте этот же токен во всех спутников'
                    : 'Токен — тот же, что сгенерирован на главном инстансе'}
                </span>
                {isHub ? (
                  <div className={styles.tokenRow}>
                    <input
                      className={styles.tokenInput}
                      type="text"
                      readOnly
                      value={token}
                      placeholder="токен не задан — push от спутников приниматься не будет"
                      onFocus={e => e.target.select()}
                    />
                    {token && <button className={styles.btnSm} onClick={copyToken}>Копировать</button>}
                    <button className={styles.btnSm} onClick={generateToken}>{token ? 'Перегенерировать' : 'Сгенерировать'}</button>
                  </div>
                ) : (
                  <input
                    type="text"
                    className={styles.tokenInput}
                    placeholder="токен, сгенерированный на главном инстансе"
                    value={token}
                    onChange={e => setToken(e.target.value)}
                  />
                )}
              </div>

              {isHub && (
                <p className={styles.hint}>
                  Без токена GET по-прежнему открыт — карточки и события отдаются всем на чтение.
                  Токен нужен только чтобы принимать push от спутников без своего домена.
                </p>
              )}
              {!isHub && !token && (
                <p className={styles.hint}>
                  Без токена этот инстанс всё равно будет забирать карточки и события с главного —
                  просто не сможет присылать туда свои (если он сам недостижим снаружи).
                </p>
              )}
            </div>

            <div className={styles.saveRow}>
              <button className={styles.saveBtn} disabled={!dirty || saving} onClick={save}>
                {saving ? 'Сохранение…' : 'Сохранить'}
              </button>
            </div>
          </>
        )}
      </div>

      <div className={styles.toasts}>
        {toasts.map(t => (
          <div key={t.id} className={t.ok ? styles.toastOk : styles.toastErr}>{t.text}</div>
        ))}
      </div>
    </Layout>
  )
}
