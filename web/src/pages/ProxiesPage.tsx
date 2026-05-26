import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import PasswordInput from '@/components/PasswordInput'
import styles from './ProxiesPage.module.scss'

interface ProxyConfig {
  id: number
  name: string
  type: 'socks5'
  config: string
  enabled: boolean
  priority: number
  created_at: string
}

interface ProxyRoute {
  route: string
  label: string
  enabled: boolean
  proxy_ids: number[]
}

interface TestResult {
  ok: boolean
  status?: number
  error?: string
  ms: number
}

interface Toast {
  id: number
  text: string
  ok: boolean
}

interface FormState {
  name: string
  type: 'socks5'
  s5host: string
  s5port: string
  s5login: string
  s5password: string
  enabled: boolean
  priority: number
}

const EMPTY_FORM: FormState = {
  name: '', type: 'socks5',
  s5host: '', s5port: '1080', s5login: '', s5password: '',
  enabled: true, priority: 0,
}

function parseSocks5(config: string): { host: string; port: string; login: string; password: string } {
  try {
    const u = new URL(config)
    return {
      host: u.hostname,
      port: u.port || '1080',
      login: u.username ? decodeURIComponent(u.username) : '',
      password: u.password ? decodeURIComponent(u.password) : '',
    }
  } catch {
    const bare = config.replace(/^socks5h?:\/\//, '')
    const lastColon = bare.lastIndexOf(':')
    return lastColon > 0
      ? { host: bare.slice(0, lastColon), port: bare.slice(lastColon + 1), login: '', password: '' }
      : { host: bare, port: '1080', login: '', password: '' }
  }
}

function buildSocks5Url(host: string, port: string, login: string, password: string): string {
  const hostport = `${host}:${port}`
  if (!login) return `socks5://${hostport}`
  const u = encodeURIComponent(login)
  const p = encodeURIComponent(password)
  return `socks5://${u}:${p}@${hostport}`
}

function formToConfig(f: FormState): string {
  return buildSocks5Url(f.s5host, f.s5port, f.s5login, f.s5password)
}

function configToForm(c: ProxyConfig): FormState {
  const { host, port, login, password } = parseSocks5(c.config)
  return { name: c.name, type: 'socks5', s5host: host, s5port: port, s5login: login, s5password: password, enabled: c.enabled, priority: c.priority }
}

function configDisplay(c: ProxyConfig): string {
  const { host, port, login } = parseSocks5(c.config)
  return login ? `${host}:${port} (${login})` : `${host}:${port}`
}

export default function ProxiesPage() {
  const [configs, setConfigs] = useState<ProxyConfig[]>([])
  const [routes, setRoutes] = useState<ProxyRoute[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [form, setForm] = useState<FormState>(EMPTY_FORM)
  const [editId, setEditId] = useState<number | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [testing, setTesting] = useState<number | null>(null)
  const [testResults, setTestResults] = useState<Record<number, TestResult>>({})

  function toast(text: string, ok = true) {
    const id = Date.now()
    setToasts(prev => [...prev, { id, text, ok }])
    setTimeout(() => setToasts(prev => prev.filter(t => t.id !== id)), 3000)
  }

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const r = await fetch('/api/admin/proxies/')
      if (!r.ok) throw new Error()
      const d = await r.json()
      setConfigs(d.configs ?? [])
      setRoutes(d.routes ?? [])
    } catch {
      toast('Ошибка загрузки', false)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  function openAdd() {
    setForm(EMPTY_FORM)
    setEditId(null)
    setShowForm(true)
  }

  function openEdit(c: ProxyConfig) {
    setForm(configToForm(c))
    setEditId(c.id)
    setShowForm(true)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const config = formToConfig(form)
    if (!config) { toast('Заполните поля', false); return }
    setSaving(true)
    try {
      const url = editId ? `/api/admin/proxies/${editId}` : '/api/admin/proxies/'
      const method = editId ? 'PUT' : 'POST'
      const r = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: form.name, type: form.type, config, enabled: form.enabled, priority: form.priority }),
      })
      if (!r.ok) throw new Error()
      toast(editId ? 'Сохранено' : 'Добавлено')
      setShowForm(false)
      load()
    } catch {
      toast('Ошибка сохранения', false)
    } finally {
      setSaving(false)
    }
  }

  async function handleDelete(id: number) {
    if (!confirm('Удалить прокси?')) return
    const r = await fetch(`/api/admin/proxies/${id}`, { method: 'DELETE' })
    if (r.ok) { toast('Удалено'); load() }
    else toast('Ошибка', false)
  }

  async function handleTest(id: number) {
    setTesting(id)
    try {
      const r = await fetch(`/api/admin/proxies/${id}/test`, { method: 'POST' })
      const d = await r.json()
      setTestResults(prev => ({ ...prev, [id]: d }))
    } catch {
      setTestResults(prev => ({ ...prev, [id]: { ok: false, error: 'network error', ms: 0 } }))
    } finally {
      setTesting(null)
    }
  }

  async function handleRouteSave() {
    setSaving(true)
    try {
      const r = await fetch('/api/admin/proxies/routing', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(routes),
      })
      if (!r.ok) throw new Error()
      toast('Маршрутизация сохранена')
    } catch {
      toast('Ошибка сохранения', false)
    } finally {
      setSaving(false)
    }
  }

  function updateRoute(route: string, patch: Partial<ProxyRoute>) {
    setRoutes(prev => prev.map(r => r.route === route ? { ...r, ...patch } : r))
  }

  function sf(patch: Partial<FormState>) { setForm(f => ({ ...f, ...patch })) }

  return (
    <Layout wide>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Прокси</h1>
          <div className={styles.headerLinks}>
            <Link to="/admin" className={styles.backLink}>Админ</Link>
          </div>
        </div>

        {/* ── Proxy list ──────────────────────────────────────────────────────── */}
        <div className={styles.section}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>Конфигурации прокси</h2>
            <button className={styles.btnAdd} onClick={openAdd}>+ Добавить</button>
          </div>

          {loading && <p className={styles.empty}>Загрузка…</p>}
          {!loading && configs.length === 0 && (
            <p className={styles.empty}>Прокси не настроены</p>
          )}

          {configs.map(c => {
            const tr = testResults[c.id]
            return (
              <div key={c.id} className={`${styles.proxyRow}${!c.enabled ? ' ' + styles.disabled : ''}`}>
                <div className={styles.proxyInfo}>
                  <span className={styles.proxyName}>{c.name}</span>
                  <span className={styles.proxyConfig}>{configDisplay(c)}</span>
                  {!c.enabled && <span className={styles.disabledBadge}>выкл</span>}
                  <span className={styles.priorityLabel}>p:{c.priority}</span>
                </div>
                <div className={styles.proxyActions}>
                  {tr && (
                    <span className={tr.ok ? styles.testOk : styles.testFail}>
                      {tr.ok ? `✓ ${tr.ms}ms` : `✗ ${tr.error ?? tr.status}`}
                    </span>
                  )}
                  <button className={styles.btnTest} onClick={() => handleTest(c.id)} disabled={testing === c.id}>
                    {testing === c.id ? '…' : 'Тест'}
                  </button>
                  <button className={styles.btnEdit} onClick={() => openEdit(c)}>Изменить</button>
                  <button className={styles.btnDel} onClick={() => handleDelete(c.id)}>Удалить</button>
                </div>
              </div>
            )
          })}
        </div>

        {/* ── Add / edit form ──────────────────────────────────────────────────── */}
        {showForm && (
          <div className={styles.section}>
            <h2 className={styles.sectionTitle}>{editId ? 'Редактировать прокси' : 'Новый прокси'}</h2>
            <form onSubmit={handleSubmit} className={styles.form}>
              <div className={styles.formRow}>
                <label className={styles.label}>Название</label>
                <input className={styles.input} value={form.name} onChange={e => sf({ name: e.target.value })} placeholder="Мой прокси" required />
              </div>
              <div className={styles.formRow}>
                <label className={styles.label}>Хост</label>
                <input className={styles.input} value={form.s5host} onChange={e => sf({ s5host: e.target.value })} placeholder="vps.example.com" required />
              </div>
              <div className={styles.formRow}>
                <label className={styles.label}>Порт</label>
                <input className={styles.inputSmall} type="number" value={form.s5port} onChange={e => sf({ s5port: e.target.value })} placeholder="1080" required min="1" max="65535" />
              </div>
              <div className={styles.formRow}>
                <label className={styles.label}>Логин</label>
                <input className={styles.input} value={form.s5login} onChange={e => sf({ s5login: e.target.value })} placeholder="необязательно" autoComplete="off" />
              </div>
              <div className={styles.formRow}>
                <label className={styles.label}>Пароль</label>
                <PasswordInput className={styles.input} value={form.s5password} onChange={e => sf({ s5password: e.target.value })} placeholder="необязательно" autoComplete="new-password" />
              </div>

              <div className={styles.formRow}>
                <label className={styles.label}>Приоритет</label>
                <input className={styles.inputSmall} type="number" value={form.priority} onChange={e => sf({ priority: Number(e.target.value) })} />
                <span className={styles.hint}>меньше = выше приоритет</span>
              </div>
              <div className={styles.formRow}>
                <label className={styles.checkLabel}>
                  <input type="checkbox" checked={form.enabled} onChange={e => sf({ enabled: e.target.checked })} />
                  Включён
                </label>
              </div>
              <div className={styles.formButtons}>
                <button type="submit" className={styles.btnSave} disabled={saving}>{saving ? 'Сохранение…' : 'Сохранить'}</button>
                <button type="button" className={styles.btnCancel} onClick={() => setShowForm(false)}>Отмена</button>
              </div>
            </form>
          </div>
        )}

        {/* ── Routing ─────────────────────────────────────────────────────────── */}
        <div className={styles.section}>
          <div className={styles.sectionHeader}>
            <h2 className={styles.sectionTitle}>Маршрутизация</h2>
            <button className={styles.btnSave} onClick={handleRouteSave} disabled={saving}>Сохранить</button>
          </div>
          <p className={styles.hint}>Выберите для каких запросов использовать прокси</p>

          {routes.map(rt => {
            const enabledConfigs = configs.filter(c => c.enabled)
            const allSelected = enabledConfigs.length > 0 && enabledConfigs.every(c => rt.proxy_ids.includes(c.id))
            return (
            <div key={rt.route} className={styles.routeRow}>
              <label className={styles.checkLabel}>
                <input type="checkbox" checked={rt.enabled} onChange={e => updateRoute(rt.route, { enabled: e.target.checked })} />
                <span className={styles.routeLabel}>{rt.label}</span>
              </label>
              <div className={`${styles.proxyCheckList}${!rt.enabled ? ' ' + styles.disabled : ''}`}>
                {configs.length === 0
                  ? <span className={styles.hint}>нет прокси</span>
                  : <>
                    <button
                      type="button"
                      className={styles.btnToggleAll}
                      disabled={!rt.enabled}
                      onClick={() => updateRoute(rt.route, {
                        proxy_ids: allSelected ? [] : enabledConfigs.map(c => c.id),
                      })}
                    >
                      {allSelected ? 'Снять все' : 'Выбрать все'}
                    </button>
                    {configs.map(c => {
                      const checked = rt.proxy_ids.includes(c.id)
                      return (
                        <label key={c.id} className={`${styles.proxyCheckItem}${!c.enabled ? ' ' + styles.proxyCheckDisabled : ''}`}>
                          <input
                            type="checkbox"
                            checked={checked}
                            disabled={!rt.enabled || !c.enabled}
                            onChange={() => {
                              const ids = checked
                                ? rt.proxy_ids.filter(id => id !== c.id)
                                : [...rt.proxy_ids, c.id]
                              updateRoute(rt.route, { proxy_ids: ids })
                            }}
                          />
                          <span>{c.name}</span>
                        </label>
                      )
                    })}
                  </>
                }
              </div>
            </div>
          )})}

          {routes.length === 0 && !loading && <p className={styles.empty}>Нет маршрутов</p>}
        </div>

        {/* Toasts */}
        <div className={styles.toasts}>
          {toasts.map(t => (
            <div key={t.id} className={`${styles.toast} ${t.ok ? styles.toastOk : styles.toastErr}`}>{t.text}</div>
          ))}
        </div>
      </div>
    </Layout>
  )
}
