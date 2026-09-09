import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import styles from './SyncPage.module.scss'

interface SyncItem {
  key: string
  label: string
  description: string
  enabled: boolean
}

interface SyncData {
  role: 'off' | 'gateway' | 'client'
  gateway_url: string
  interval_minutes: number
  push: SyncItem[]
  pull: SyncItem[]
}

interface Toast {
  id: number
  text: string
  ok: boolean
}

const ROLE_LABELS: Record<string, string> = {
  off: 'Выключено',
  gateway: 'Шлюз — принимает от клиентов',
  client: 'Клиент — шлёт на шлюз и подтягивает с него',
}

export default function SyncPage() {
  const [data, setData] = useState<SyncData | null>(null)
  const [role, setRole] = useState<SyncData['role']>('off')
  const [gatewayUrl, setGatewayUrl] = useState('')
  const [intervalMinutes, setIntervalMinutes] = useState(15)
  const [push, setPush] = useState<SyncItem[]>([])
  const [pull, setPull] = useState<SyncItem[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])

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
        setRole(d.role)
        setGatewayUrl(d.gateway_url)
        setIntervalMinutes(d.interval_minutes)
        setPush(d.push)
        setPull(d.pull)
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function save() {
    setSaving(true)
    try {
      const items: Record<string, boolean> = {}
      for (const it of [...push, ...pull]) items[it.key] = it.enabled
      const r = await fetch('/api/admin/sync', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          role,
          gateway_url: gatewayUrl,
          interval_minutes: intervalMinutes,
          items,
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

  function toggleItem(list: SyncItem[], setList: (v: SyncItem[]) => void, key: string) {
    setList(list.map(it => it.key === key ? { ...it, enabled: !it.enabled } : it))
  }

  const dirty = data !== null && (
    role !== data.role ||
    gatewayUrl !== data.gateway_url ||
    intervalMinutes !== data.interval_minutes ||
    [...push, ...pull].some(it => it.enabled !== [...data.push, ...data.pull].find(d => d.key === it.key)?.enabled)
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
          Обмен данными с другим инстансом movies-go (роль «шлюз»/«клиент») — push своих просмотров и
          pull того, чего нет локально. Подробности — <code>dev/instance-sync.md</code>.
        </p>

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && (
          <>
            <div className={styles.section}>
              <h2 className={styles.sectionTitle}>Роль</h2>
              <div className={styles.roleRow}>
                {(['off', 'gateway', 'client'] as const).map(r => (
                  <label key={r} className={styles.roleOption}>
                    <input type="radio" name="role" checked={role === r} onChange={() => setRole(r)} />
                    <span>{ROLE_LABELS[r]}</span>
                  </label>
                ))}
              </div>

              {role === 'client' && (
                <div className={styles.fieldsRow}>
                  <label className={styles.field}>
                    <span>URL шлюза</span>
                    <input
                      type="text"
                      className={styles.input}
                      placeholder="https://example.com"
                      value={gatewayUrl}
                      onChange={e => setGatewayUrl(e.target.value)}
                    />
                  </label>
                  <label className={styles.field}>
                    <span>Интервал pull (мин)</span>
                    <input
                      type="number"
                      min={1}
                      className={styles.inputNarrow}
                      value={intervalMinutes}
                      onChange={e => setIntervalMinutes(Math.max(1, parseInt(e.target.value) || 15))}
                    />
                  </label>
                </div>
              )}
            </div>

            <div className={styles.section}>
              <h2 className={styles.sectionTitle}>Push — что отправляем на шлюз</h2>
              {role !== 'client' && <p className={styles.hint}>Действует только при роли «клиент».</p>}
              <div className={styles.itemList}>
                {push.map(it => (
                  <label key={it.key} className={styles.itemRow}>
                    <input
                      type="checkbox"
                      checked={it.enabled}
                      onChange={() => toggleItem(push, setPush, it.key)}
                    />
                    <div className={styles.itemBody}>
                      <span className={styles.itemLabel}>{it.label}</span>
                      <span className={styles.itemDesc}>{it.description}</span>
                    </div>
                  </label>
                ))}
              </div>
            </div>

            <div className={styles.section}>
              <h2 className={styles.sectionTitle}>Pull — что подтягиваем с шлюза</h2>
              {role !== 'client' && <p className={styles.hint}>Действует только при роли «клиент».</p>}
              <div className={styles.itemList}>
                {pull.map(it => (
                  <label key={it.key} className={styles.itemRow}>
                    <input
                      type="checkbox"
                      checked={it.enabled}
                      onChange={() => toggleItem(pull, setPull, it.key)}
                    />
                    <div className={styles.itemBody}>
                      <span className={styles.itemLabel}>{it.label}</span>
                      <span className={styles.itemDesc}>{it.description}</span>
                    </div>
                  </label>
                ))}
              </div>
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
