import { useLayoutEffect, useRef, type RefObject } from 'react'

const locks = new Set<RefObject<HTMLElement | null>>()
let restorePage: (() => void) | undefined
let lastTouch: { x: number; y: number } | undefined

export function isPageScrollLocked(): boolean {
  return locks.size > 0
}

function onTouchStart(e: TouchEvent) {
  const touch = e.touches.length === 1 ? e.touches[0] : undefined
  lastTouch = touch ? { x: touch.clientX, y: touch.clientY } : undefined
}

function onTouchMove(e: TouchEvent) {
  // Keep pinch zoom available; only single-finger page scrolling is locked.
  if (e.touches.length !== 1 || !lastTouch) return
  const touch = e.touches[0]
  const dx = touch.clientX - lastTouch.x
  const dy = touch.clientY - lastTouch.y
  lastTouch = { x: touch.clientX, y: touch.clientY }
  if (dx === 0 && dy === 0) return

  const target = e.target instanceof Element ? e.target : null
  // The innermost lock owns the gesture when a dialog is inside a drawer.
  let root: HTMLElement | null = null
  for (const ref of locks) {
    if (target && ref.current?.contains(target) && (!root || root.contains(ref.current))) {
      root = ref.current
    }
  }

  const vertical = Math.abs(dy) >= Math.abs(dx)
  for (let el = root ? target : null; el; el = el.parentElement) {
    const style = getComputedStyle(el)
    const overflow = vertical ? style.overflowY : style.overflowX
    const position = vertical ? el.scrollTop : el.scrollLeft
    const size = vertical ? el.clientHeight : el.clientWidth
    const extent = vertical ? el.scrollHeight : el.scrollWidth
    const delta = vertical ? dy : dx
    // Allow drums, textareas and lists to scroll, but stop the gesture at
    // their edges instead of letting iOS hand it to the background page.
    if (/^(auto|scroll)$/.test(overflow) && extent > size &&
        (delta > 0 ? position > 0 : position + size < extent - 1)) return
    if (el === root) break
  }
  if (e.cancelable) e.preventDefault()
}

function lockElement(el: HTMLElement): () => void {
  const properties = ['overflow', 'overflow-x', 'overflow-y',
    'overscroll-behavior', 'overscroll-behavior-x', 'overscroll-behavior-y']
  const previous = Array.from(el.style)
    .filter(name => properties.includes(name))
    .map(name => [name, el.style.getPropertyValue(name), el.style.getPropertyPriority(name)])
  el.style.setProperty('overflow', 'hidden')
  el.style.setProperty('overscroll-behavior', 'none')
  return () => {
    for (const name of properties) el.style.removeProperty(name)
    for (const [name, value, priority] of previous) el.style.setProperty(name, value, priority)
  }
}

// Attach the returned ref to the overlay/drawer that may scroll internally.
// Overflow on body alone is insufficient in iOS standalone mode. Lock the
// document too and guard touchmove, without fixing/repositioning body (that
// makes the fixed bottom navigation jump in iPhone PWA).
export function usePageScrollLock(enabled = true) {
  const rootRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    if (!enabled) return
    if (locks.size === 0) {
      const x = window.scrollX
      const y = window.scrollY
      const href = window.location.href
      const restoreHtml = lockElement(document.documentElement)
      const restoreBody = lockElement(document.body)
      document.addEventListener('touchstart', onTouchStart, { passive: true })
      document.addEventListener('touchmove', onTouchMove, { passive: false })
      restorePage = () => {
        document.removeEventListener('touchstart', onTouchStart)
        document.removeEventListener('touchmove', onTouchMove)
        lastTouch = undefined
        restoreBody()
        restoreHtml()
        if (window.location.href === href && (window.scrollX !== x || window.scrollY !== y)) {
          window.scrollTo({ left: x, top: y, behavior: 'instant' })
        }
      }
    }
    locks.add(rootRef)
    return () => {
      locks.delete(rootRef)
      if (locks.size === 0) {
        restorePage?.()
        restorePage = undefined
      }
    }
  }, [enabled])

  return rootRef
}
