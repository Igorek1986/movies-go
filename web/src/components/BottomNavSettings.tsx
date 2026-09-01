import { useEffect, useState } from 'react'
import { useAuth } from '@/hooks/useAuth'
import { BOTTOM_NAV_ICONS } from '@/components/BottomNav'
import {
  BOTTOM_NAV_OPTIONS, DEFAULT_KEYS, MIN_ITEMS, MAX_ITEMS,
  DEFAULT_POSITION, resolveBottomNavKeys, resolveBottomNavPosition,
  saveBottomNavConfig, type BottomNavPosition,
} from '@/utils/bottomNavConfig'
// Reuses ProfilesPage's own <details>/<summary>/checkbox styles for a
// consistent look — this settings block only lives on that page today.
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import styles from './BottomNavSettings.module.scss'

interface Props {
  // See CardLayoutSettings — bare skips the <details>/<summary> wrapper for
  // ProfilesRemoteView's drill-down screens. Classic omits it.
  bare?: boolean
}

// Settings UI for the mobile bottom bar (see components/BottomNav). Lives on
// the "Настройки" (/profiles) page — saves per-account via
// saveBottomNavKeys, which also notifies any mounted Layout to update its
// bar immediately (see the bottomnav:update listener there).
export function BottomNavSettings({ bare }: Props = {}) {
  const { user } = useAuth()
  const [keys, setKeys] = useState<string[]>(DEFAULT_KEYS)
  const [position, setPosition] = useState<BottomNavPosition>(DEFAULT_POSITION)
  useEffect(() => {
    if (user) {
      setKeys(resolveBottomNavKeys(user.bottom_nav_keys))
      setPosition(resolveBottomNavPosition(user.bottom_nav_position))
    }
  }, [user])
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)

  function toggle(key: string) {
    setMsg(null)
    setKeys(prev => {
      if (prev.includes(key)) {
        if (prev.length <= MIN_ITEMS) return prev
        return prev.filter(k => k !== key)
      }
      if (prev.length >= MAX_ITEMS) return prev
      return [...prev, key]
    })
  }

  function move(key: string, dir: -1 | 1) {
    setMsg(null)
    setKeys(prev => {
      const i = prev.indexOf(key)
      const j = i + dir
      if (j < 0 || j >= prev.length) return prev
      const next = [...prev]
      ;[next[i], next[j]] = [next[j], next[i]]
      return next
    })
  }

  async function handleSave() {
    setSaving(true)
    setMsg(null)
    const ok = await saveBottomNavConfig({ keys, position })
    setMsg(ok ? 'Сохранено' : 'Ошибка сохранения')
    setSaving(false)
  }

  // Selected first (in their actual bar order, with up/down controls),
  // unselected after (checkbox only) — so the visible order matches the bar.
  const ordered = [
    ...keys.map(k => BOTTOM_NAV_OPTIONS.find(o => o.key === k)!),
    ...BOTTOM_NAV_OPTIONS.filter(o => !keys.includes(o.key)),
  ]

  const body = (
    <>
      <p className={pageStyles.hint}>
        Кнопки нижней панели на телефоне — от {MIN_ITEMS} до {MAX_ITEMS}, стрелками меняешь порядок.
      </p>
      <p className={pageStyles.hint}>
        Расположение на планшетном экране (768–1024px) — на телефоне панель всегда снизу.
      </p>
      <p className={pageStyles.hint}>
        На десктопе эта же панель вызывается стрелками ← / → (когда фокус не внутри строки с настройками) — всплывает сбоку, между кнопками переключаетесь стрелками ↑ / ↓, закрывается клавишей Escape или стрелкой в обратную сторону.
      </p>
      <div className={styles.positionRow} data-row-id="bottomnav-position">
        <label className={pageStyles.checkLabel}>
          <input type="radio" name="bottomNavPosition" data-nav-item checked={position === 'bottom'} onChange={() => setPosition('bottom')} />
          Снизу
        </label>
        <label className={pageStyles.checkLabel}>
          <input type="radio" name="bottomNavPosition" data-nav-item checked={position === 'right'} onChange={() => setPosition('right')} />
          Справа
        </label>
        <label className={pageStyles.checkLabel}>
          <input type="radio" name="bottomNavPosition" data-nav-item checked={position === 'left'} onChange={() => setPosition('left')} />
          Слева
        </label>
      </div>
      <ul className={styles.list}>
        {ordered.map(opt => {
          const idx = keys.indexOf(opt.key)
          const active = idx !== -1
          return (
            <li key={opt.key} className={styles.item} data-row-id={`bottomnav-item-${opt.key}`}>
              <label className={pageStyles.checkLabel}>
                <input type="checkbox" data-nav-item checked={active} onChange={() => toggle(opt.key)} />
                <span className={styles.icon}>{BOTTOM_NAV_ICONS[opt.key]}</span>
                {opt.label}
              </label>
              {active && (
                <div className={styles.orderBtns}>
                  <button type="button" data-nav-item disabled={idx === 0} onClick={() => move(opt.key, -1)} aria-label="Выше">↑</button>
                  <button type="button" data-nav-item disabled={idx === keys.length - 1} onClick={() => move(opt.key, 1)} aria-label="Ниже">↓</button>
                </div>
              )}
            </li>
          )
        })}
      </ul>
      {msg && <p className={msg === 'Сохранено' ? pageStyles.successText : pageStyles.errorText}>{msg}</p>}
      <div className={styles.actions} data-row-id="bottomnav-actions">
        <button type="button" className={pageStyles.btnSm} data-nav-item onClick={() => setKeys(DEFAULT_KEYS)}>Сбросить</button>
        <button type="button" className={pageStyles.btnPrimary} data-nav-item onClick={handleSave} disabled={saving}>
          {saving ? 'Сохранение…' : 'Сохранить'}
        </button>
      </div>
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Панель навигации</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
