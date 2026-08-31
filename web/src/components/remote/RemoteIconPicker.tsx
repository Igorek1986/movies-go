import { useEffect, useRef } from 'react'
import { PROFILE_ICON_IDS, profileIconSrc } from '@/utils/profileIcon'
import { getGridCols } from '@/utils/scrollNav'
import styles from './RemoteIconPicker.module.scss'

interface RemoteIconPickerProps {
  current: string
  onSelect: (id: string) => void
  onClose: () => void
}

// Remote-navigable replacement for ProfilesPage's inline IconPicker — same
// grid of icons, but 2D Up/Down/Left/Right nav instead of mouse-only
// (getGridCols is the same offsetTop-based column counter CatalogPage's
// row-nav uses, since it's driven by the same kind of CSS grid). Caller
// renders this conditionally (like the current IconPicker) — it IS the open
// overlay, no internal open state of its own.
export function RemoteIconPicker({ current, onSelect, onClose }: RemoteIconPickerProps) {
  const gridRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = gridRef.current?.querySelector<HTMLElement>(`[data-icon="${current}"]`)
      ?? gridRef.current?.querySelector<HTMLElement>('[data-nav-item]')
    el?.focus()
  }, [current])

  function handleKeyDown(e: React.KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Escape' || e.key === 'Backspace') { e.preventDefault(); onClose(); return }
    if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return
    e.preventDefault()
    const items = Array.from(gridRef.current?.querySelectorAll<HTMLElement>('[data-nav-item]') ?? [])
    const i = items.indexOf(document.activeElement as HTMLElement)
    if (i === -1) return
    const cols = getGridCols(items)
    let next = i
    if (e.key === 'ArrowLeft') next = i - 1
    else if (e.key === 'ArrowRight') next = i + 1
    else if (e.key === 'ArrowUp') next = i - cols
    else if (e.key === 'ArrowDown') next = i + cols
    if (next < 0 || next >= items.length) return
    items[next]?.focus()
  }

  return (
    <div className={styles.overlay} data-remote-overlay onClick={onClose} onKeyDown={handleKeyDown}>
      <div ref={gridRef} className={styles.grid} onClick={e => e.stopPropagation()}>
        {PROFILE_ICON_IDS.map(id => (
          <button
            key={id}
            type="button"
            data-nav-item
            data-icon={id}
            className={`${styles.btn}${id === current ? ' ' + styles.btnActive : ''}`}
            onClick={() => onSelect(id)}
          >
            <img src={profileIconSrc(id)} alt={id} />
          </button>
        ))}
      </div>
    </div>
  )
}
