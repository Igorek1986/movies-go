import { useEffect, useRef, useState } from 'react'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { useMenuOrder } from '@/hooks/useMenuOrder'
import { fetchCatalogCategories, collapseCollectionsBlock, type CatalogCategory } from '@/utils/catalogCategories'
import { registerUnsavedChangesGuard } from '@/utils/unsavedChangesGuard'
// Reuses ProfilesPage's own <details>/<summary>/checkbox/button styles, and
// BottomNavSettings' up/down-list layout — same shape of problem (reorder +
// toggle a list of items, explicit Save/Reset).
import pageStyles from '@/pages/profiles/ProfilesClassicView.module.scss'
import styles from './BottomNavSettings.module.scss'

interface Props {
  // See CardLayoutSettings — bare skips the <details>/<summary> wrapper for
  // ProfilesRemoteView's drill-down screens. Classic omits it.
  bare?: boolean
}

interface Row extends CatalogCategory {
  hidden: boolean
}

// Long-press before a drag starts — long enough that just steadying your
// finger on the handle before deciding to move it doesn't misfire.
const DRAG_HOLD_MS = 300
// Auto-scroll the nearest scrollable ancestor once the dragged row gets this
// close to its top/bottom edge — otherwise reordering across a category
// list this long meant never seeing where you were dropping it. Generous on
// purpose: triggering only once the finger is almost at the literal edge
// left barely any room to react before running into the fixed bottom nav
// bar (Layout.tsx's [data-bottom-nav]) — see getDragBounds below, which
// also excludes that bar's own height from the usable area entirely.
const AUTO_SCROLL_EDGE_PX = 110
const AUTO_SCROLL_MAX_SPEED = 16 // px per animation frame, at the very edge

// "Порядок и видимость категорий" — та же per-профильная настройка, что
// numparser_menu_sort/numparser_menu_hide в np.js (Lampa), общая на профиль
// (см. useMenuOrder). Действует на строки Каталога в обоих режимах
// отображения (Hero и Classic) — см. applyMenuOrder в CatalogPage.tsx.
export function MenuOrderSettings({ bare }: Props = {}) {
  const { activeProfile } = useActiveProfile()
  const profileId = activeProfile?.profile_id ?? ''
  const { order, setOrder, hidden, setHidden, orderLoaded, hiddenLoaded } = useMenuOrder(profileId)
  const [rows, setRows] = useState<Row[] | null>(null)
  // Natural (pre-merge) order — from the fetch itself, before layering the
  // saved numparser_menu_sort on top — used by "Сбросить", so it restores
  // today's default arrangement instead of just unhiding everything in
  // whatever order the user had left it.
  const naturalOrderRef = useRef<CatalogCategory[]>([])
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)

  // Снимок последнего СОХРАНЁННОГО состояния (id+hidden каждой строки, в
  // порядке) — сверяется с текущим rows, чтобы знать, есть ли несохранённые
  // правки (см. registerUnsavedChangesGuard ниже, использует
  // ProfilesRemoteView's goBack при выходе назад).
  const savedSnapshotRef = useRef<string>('')
  function snapshotOf(list: Row[]): string {
    return JSON.stringify(list.map(r => [r.id, r.hidden]))
  }

  useEffect(() => {
    if (!orderLoaded || !hiddenLoaded) return
    let cancelled = false
    fetchCatalogCategories().then(cats => {
      if (cancelled) return
      const collapsed = collapseCollectionsBlock(cats)
      naturalOrderRef.current = collapsed
      const map = new Map(collapsed.map(c => [c.id, c]))
      const merged: Row[] = []
      for (const id of order) {
        const c = map.get(id)
        if (c) { merged.push({ ...c, hidden: hidden.includes(id) }); map.delete(id) }
      }
      for (const c of map.values()) merged.push({ ...c, hidden: hidden.includes(c.id) })
      setRows(merged)
      savedSnapshotRef.current = snapshotOf(merged)
    })
    return () => { cancelled = true }
    // Меняющиеся order/hidden после первой загрузки не должны перестраивать
    // список заново (пользователь мог уже сам его переставить локально, ещё
    // не сохранив) — только первичная загрузка, когда оба флага стали true.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderLoaded, hiddenLoaded])

  // После перемещения ↑/↓ строка могла уйти за край видимой области (список
  // категорий длинный), и если кнопка, на которой был фокус, стала
  // disabled на новой границе — фокус улетал в никуда. Держим и то, и
  // другое привязанным к id перемещённой строки, а не к индексу.
  const lastMovedIdRef = useRef<string | null>(null)
  useEffect(() => {
    const id = lastMovedIdRef.current
    if (!id || !rows) return
    lastMovedIdRef.current = null
    const el = document.querySelector<HTMLElement>(`[data-row-id="menu-order-${CSS.escape(id)}"]`)
    el?.scrollIntoView({ block: 'nearest' })
    el?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true })
  }, [rows])

  function move(idx: number, dir: -1 | 1) {
    setMsg(null)
    setRows(prev => {
      if (!prev) return prev
      const j = idx + dir
      if (j < 0 || j >= prev.length) return prev
      lastMovedIdRef.current = prev[idx].id
      const next = [...prev]
      ;[next[idx], next[j]] = [next[j], next[idx]]
      return next
    })
  }

  function toggle(idx: number) {
    setMsg(null)
    setRows(prev => {
      if (!prev) return prev
      const next = [...prev]
      next[idx] = { ...next[idx], hidden: !next[idx].hidden }
      return next
    })
  }

  function handleReset() {
    setMsg(null)
    setRows(naturalOrderRef.current.map(c => ({ ...c, hidden: false })))
  }

  function setAllHidden(nextHidden: boolean) {
    setMsg(null)
    setRows(prev => prev && prev.map(r => ({ ...r, hidden: nextHidden })))
  }

  async function handleSave() {
    if (!rows) return
    setSaving(true)
    setMsg(null)
    const ok1 = await setOrder(rows.map(r => r.id))
    const ok2 = await setHidden(rows.filter(r => r.hidden).map(r => r.id))
    if (ok1 && ok2) savedSnapshotRef.current = snapshotOf(rows)
    setMsg(ok1 && ok2 ? 'Сохранено' : 'Ошибка сохранения')
    setSaving(false)
  }

  // Проверяется ProfilesRemoteView'ой goBack перед тем, как реально выйти из
  // этого экрана (Backspace/клик "‹ Назад"/клик за пределами панели) — вместо
  // молчаливой потери несохранённых правок предлагает сохранить. rowsRef/
  // handleSaveRef держат актуальные значения без пересоздания эффекта на
  // каждое изменение rows.
  const rowsRef = useRef<Row[] | null>(null)
  rowsRef.current = rows
  const handleSaveRef = useRef(handleSave)
  handleSaveRef.current = handleSave
  useEffect(() => {
    registerUnsavedChangesGuard({
      isDirty: () => {
        const current = rowsRef.current
        return !!current && snapshotOf(current) !== savedSnapshotRef.current
      },
      save: () => handleSaveRef.current(),
    })
    return () => registerUnsavedChangesGuard(null)
  }, [])

  // ── Touch: a dedicated handle drags a row; tapping anywhere else on it
  // toggles ────────────────────────────────────────────────────────────────
  // Dragging needs touch-action:none from the very first touch (see
  // .dragRow in BottomNavSettings.module.scss) — a browser locks in whether
  // a touch sequence pans the page right at touchstart, not once some later
  // hold timer confirms a drag. Putting that on the WHOLE row meant an
  // ordinary scroll swipe could never start on top of a category either
  // (this list is most of the screen) — scoping it to a small handle icon
  // instead leaves the rest of the row free for normal scrolling, same as
  // any other touch-reorderable list.
  const listRef = useRef<HTMLUListElement>(null)
  const dragIdxRef = useRef<number | null>(null)
  const [dragIdx, setDragIdx] = useState<number | null>(null)
  const holdTimerRef = useRef<number | null>(null)

  function clearHoldTimer() {
    if (holdTimerRef.current != null) { window.clearTimeout(holdTimerRef.current); holdTimerRef.current = null }
  }

  // React attaches onPointerMove/onTouchMove as passive listeners (a browser/
  // React default, for scroll performance) — calling preventDefault() inside
  // one is silently ignored. Track the drag with a real
  // addEventListener({passive: false}) on window instead, for the gesture's
  // whole lifetime; cleaned up on pointerup/pointercancel or unmount.
  const dragCleanupRef = useRef<(() => void) | null>(null)

  function startDragging(idx: number) {
    dragIdxRef.current = idx
    setDragIdx(idx)

    // The nearest scrollable ancestor of the list — ProfilesRemoteView's
    // mobile bottom sheet scrolls its own .sectionBody, not the window;
    // Classic layout scrolls the window itself. Auto-scroll (below) needs
    // to move whichever one is actually responsible for this list's
    // vertical space, not always window.
    let scrollEl: HTMLElement | (Window & typeof globalThis) = window
    for (let node = listRef.current?.parentElement; node; node = node.parentElement) {
      const cs = getComputedStyle(node)
      if ((cs.overflowY === 'auto' || cs.overflowY === 'scroll') && node.scrollHeight > node.clientHeight) {
        scrollEl = node
        break
      }
    }

    // Usable vertical range for the edge check below — not always the raw
    // viewport: ProfilesRemoteView's mobile bottom sheet scrolls its own
    // .sectionBody (whatever scrollEl resolved to above), which already
    // stops short of the fixed bottom nav; Classic layout scrolls the whole
    // window, which does NOT exclude that bar on its own (it's a
    // position:fixed overlay on top of the page, see Layout.tsx's
    // [data-bottom-nav]) — subtracting its height here is what stops the
    // drop target from ending up hidden behind it.
    let top = 0
    let bottom = window.innerHeight
    if (scrollEl !== window) {
      const r = (scrollEl as HTMLElement).getBoundingClientRect()
      top = r.top
      bottom = r.bottom
    }
    const bottomNav = document.querySelector<HTMLElement>('[data-bottom-nav]')
    if (bottomNav) {
      const navTop = bottomNav.getBoundingClientRect().top
      if (navTop < bottom) bottom = navTop
    }

    let autoScrollDy = 0
    let rafId: number | null = null
    function autoScrollLoop() {
      if (autoScrollDy !== 0) scrollEl.scrollBy(0, autoScrollDy)
      rafId = requestAnimationFrame(autoScrollLoop)
    }
    rafId = requestAnimationFrame(autoScrollLoop)

    function onMove(e: PointerEvent) {
      if (e.pointerType !== 'touch') return
      e.preventDefault()
      const from = dragIdxRef.current
      if (from === null) return

      const clamp01 = (v: number) => Math.max(0, Math.min(1, v))
      if (e.clientY < top + AUTO_SCROLL_EDGE_PX) {
        autoScrollDy = -AUTO_SCROLL_MAX_SPEED * clamp01(1 - (e.clientY - top) / AUTO_SCROLL_EDGE_PX)
      } else if (e.clientY > bottom - AUTO_SCROLL_EDGE_PX) {
        autoScrollDy = AUTO_SCROLL_MAX_SPEED * clamp01(1 - (bottom - e.clientY) / AUTO_SCROLL_EDGE_PX)
      } else {
        autoScrollDy = 0
      }

      const items = Array.from(listRef.current?.querySelectorAll<HTMLElement>('[data-drag-item]') ?? [])
      const overIdx = items.findIndex(el => {
        const r = el.getBoundingClientRect()
        return e.clientY >= r.top && e.clientY <= r.bottom
      })
      if (overIdx === -1 || overIdx === from) return
      setRows(prev => {
        if (!prev) return prev
        const next = [...prev]
        const [movedRow] = next.splice(from, 1)
        next.splice(overIdx, 0, movedRow)
        return next
      })
      dragIdxRef.current = overIdx
      setDragIdx(overIdx)
    }

    function onEnd() {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onEnd)
      window.removeEventListener('pointercancel', onEnd)
      if (rafId != null) cancelAnimationFrame(rafId)
      dragCleanupRef.current = null
      dragIdxRef.current = null
      setDragIdx(null)
    }

    window.addEventListener('pointermove', onMove, { passive: false })
    window.addEventListener('pointerup', onEnd)
    window.addEventListener('pointercancel', onEnd)
    dragCleanupRef.current = onEnd
  }

  useEffect(() => () => dragCleanupRef.current?.(), [])

  function handleHandlePointerDown(e: React.PointerEvent<HTMLSpanElement>, idx: number) {
    if (e.pointerType !== 'touch') return
    clearHoldTimer()
    holdTimerRef.current = window.setTimeout(() => {
      holdTimerRef.current = null
      startDragging(idx)
    }, DRAG_HOLD_MS)
  }

  // A tap anywhere on the row (its own padding, the label, the text) toggles
  // it — except the handle and the ↑/↓ buttons, which handle themselves.
  // The checkbox's own native label-click forwarding already toggles it
  // once for a tap landing there; excluding label/input here (not just
  // guarding a synthetic tap ourselves) avoids toggling it a second time.
  function handleRowClick(e: React.MouseEvent<HTMLLIElement>, idx: number) {
    const target = e.target as HTMLElement
    if (target.closest('button, input, label, [data-drag-handle]')) return
    toggle(idx)
  }

  const body = (
    <>
      <p className={pageStyles.hint}>
        Порядок и видимость строк Каталога (общая для профиля на всех устройствах) — стрелками (или на телефоне удержанием значка ⠿) меняешь порядок, тапом/галочкой скрываешь ненужные.
      </p>
      {!rows && <p className={pageStyles.hint}>Загрузка…</p>}
      {rows && (
        <>
          <div className={styles.actions} data-row-id="menu-order-bulk">
            <button type="button" className={pageStyles.btnSm} data-nav-item onClick={() => setAllHidden(false)}>Выделить все</button>
            <button type="button" className={pageStyles.btnSm} data-nav-item onClick={() => setAllHidden(true)}>Снять все</button>
          </div>
          <ul className={styles.list} ref={listRef}>
            {rows.map((row, idx) => (
              <li
                key={row.id}
                data-drag-item
                data-row-id={`menu-order-${row.id}`}
                className={`${styles.item}${dragIdx === idx ? ' ' + styles.itemDragging : ''}`}
                onClick={e => handleRowClick(e, idx)}
              >
                <label className={pageStyles.checkLabel}>
                  <input type="checkbox" data-nav-item checked={!row.hidden} onChange={() => toggle(idx)} />
                  {row.name}
                </label>
                <div className={styles.orderBtns}>
                  <button type="button" className={styles.arrowBtn} data-nav-item disabled={idx === 0} onClick={() => move(idx, -1)} aria-label="Выше">↑</button>
                  <button type="button" className={styles.arrowBtn} data-nav-item disabled={idx === rows.length - 1} onClick={() => move(idx, 1)} aria-label="Ниже">↓</button>
                  <span
                    className={`${styles.dragHandle} ${styles.dragRow}`}
                    data-drag-handle
                    aria-hidden="true"
                    onPointerDown={e => handleHandlePointerDown(e, idx)}
                    onPointerUp={clearHoldTimer}
                    onPointerCancel={clearHoldTimer}
                  >⠿</span>
                </div>
              </li>
            ))}
          </ul>
          {msg && <p className={msg === 'Сохранено' ? pageStyles.successText : pageStyles.errorText}>{msg}</p>}
          <div className={styles.actions} data-row-id="menu-order-actions">
            <button type="button" className={pageStyles.btnSm} data-nav-item onClick={handleReset}>Сбросить</button>
            <button type="button" className={pageStyles.btnPrimary} data-nav-item onClick={handleSave} disabled={saving}>
              {saving ? 'Сохранение…' : 'Сохранить'}
            </button>
          </div>
        </>
      )}
    </>
  )

  if (bare) return <div className={pageStyles.detailsBody}>{body}</div>

  return (
    <details className={pageStyles.details}>
      <summary className={pageStyles.summary}>Порядок и видимость категорий</summary>
      <div className={pageStyles.detailsBody}>{body}</div>
    </details>
  )
}
