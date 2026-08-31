import { useEffect, useRef, useState } from 'react'
import styles from './RemoteSelect.module.scss'

export interface RemoteSelectOption {
  value: string
  label: string
}

interface RemoteSelectProps {
  value: string
  onChange: (value: string) => void
  options: RemoteSelectOption[]
  disabled?: boolean
  placeholder?: string
  label?: string
  // Used where some OTHER element is the trigger (e.g. a "Год рождения"
  // button that already sets its own "editing this profile" state) — skips
  // rendering RemoteSelect's own trigger button and renders the overlay
  // list immediately; onCloseForced replaces the normal close()'s
  // "refocus my own trigger" (there isn't one in this mode).
  forceOpen?: boolean
  onCloseForced?: () => void
}

// Remote-navigable replacement for a native <select> — closed state is a
// button showing the current value (styled like ProfilesPage's .select),
// open state is a self-contained overlay list (own Up/Down/Enter/Escape
// scope, same shape as RemoteDialog) so it works the same whether driven by
// a mouse, a keyboard, or a TV remote's D-pad.
export function RemoteSelect({ value, onChange, options, disabled, placeholder, label, forceOpen, onCloseForced }: RemoteSelectProps) {
  const [openState, setOpen] = useState(false)
  const open = forceOpen || openState
  const listRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  const current = options.find(o => o.value === value)

  useEffect(() => {
    if (!open) return
    const el = listRef.current?.querySelector<HTMLElement>(`[data-value="${CSS.escape(value)}"]`)
      ?? listRef.current?.querySelector<HTMLElement>('[data-nav-item]')
    el?.focus()
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  function close() {
    if (forceOpen) { onCloseForced?.(); return }
    setOpen(false)
    triggerRef.current?.focus()
  }

  function pick(v: string) {
    onChange(v)
    close()
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Escape' || e.key === 'Backspace') { e.preventDefault(); close(); return }
    if (e.key === 'Enter') {
      e.preventDefault()
      const el = document.activeElement as HTMLElement | null
      const v = el?.dataset.value
      if (v !== undefined) pick(v)
      return
    }
    if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
      e.preventDefault()
      const items = Array.from(listRef.current?.querySelectorAll<HTMLElement>('[data-nav-item]') ?? [])
      const i = items.indexOf(document.activeElement as HTMLElement)
      const next = Math.min(Math.max(i + (e.key === 'ArrowDown' ? 1 : -1), 0), items.length - 1)
      items[next]?.focus()
    }
  }

  return (
    <>
      {!forceOpen && (
        <button
          ref={triggerRef}
          type="button"
          className={styles.trigger}
          data-nav-item
          disabled={disabled}
          onClick={() => setOpen(true)}
        >
          <span className={current ? styles.value : styles.placeholder}>
            {current?.label ?? placeholder ?? 'Выбрать'}
          </span>
          <span className={styles.chevron}>▾</span>
        </button>
      )}

      {open && (
        <div className={styles.overlay} data-remote-overlay onClick={close} onKeyDown={handleKeyDown}>
          <div ref={listRef} className={styles.list} onClick={e => e.stopPropagation()}>
            {label && <p className={styles.label}>{label}</p>}
            {options.map(o => (
              <button
                key={o.value}
                type="button"
                data-nav-item
                data-value={o.value}
                className={`${styles.option}${o.value === value ? ' ' + styles.optionActive : ''}`}
                onClick={() => pick(o.value)}
              >
                {o.label}
              </button>
            ))}
          </div>
        </div>
      )}
    </>
  )
}
