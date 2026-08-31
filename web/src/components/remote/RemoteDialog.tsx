import { useEffect, useRef, useState } from 'react'
import styles from './RemoteDialog.module.scss'

export interface RemoteDialogProps {
  title?: string
  message: string
  mode: 'confirm' | 'prompt'
  multiline?: boolean
  initialValue?: string
  danger?: boolean
  confirmLabel?: string
  cancelLabel?: string
  // Plain one-button notice (replaces window.alert()) — hides the Cancel
  // button, since there's nothing to decide between.
  hideCancel?: boolean
  onConfirm: (value?: string) => void
  onCancel: () => void
}

// Remote-navigable replacement for window.confirm()/window.prompt() — used
// by ProfilesRemoteView (via useRemoteDialog) wherever the Classic view
// still calls the native dialog directly. Self-contained keydown scope, same
// idea as Layout's own floating side panel (Escape/Backspace cancels, Enter
// confirms, Up/Down cycles focus) — stopPropagation so the page's own
// [data-row-id] row-nav listener never also reacts to the same keypress.
export function RemoteDialog({
  title, message, mode, multiline, initialValue, danger,
  confirmLabel, cancelLabel, hideCancel, onConfirm, onCancel,
}: RemoteDialogProps) {
  const [value, setValue] = useState(initialValue ?? '')
  const rootRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement | HTMLTextAreaElement>(null)

  useEffect(() => {
    if (mode === 'prompt') inputRef.current?.focus()
    else rootRef.current?.querySelector<HTMLElement>('[data-nav-item]')?.focus()
  }, [mode])

  function handleKeyDown(e: React.KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Escape' || e.key === 'Backspace') {
      // Backspace inside a text field edits it, doesn't cancel the dialog.
      if (e.key === 'Backspace' && (e.target as HTMLElement)?.tagName !== 'BUTTON') return
      e.preventDefault()
      onCancel()
      return
    }
    if (e.key === 'Enter' && mode === 'prompt' && !multiline && e.target === inputRef.current) {
      e.preventDefault()
      onConfirm(value)
      return
    }
    if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && rootRef.current) {
      // A multiline textarea uses Up/Down to move the cursor between lines —
      // only cycle focus between OK/Cancel once the cursor is already on
      // the first/last line (there's no Tab key on a remote to get out
      // otherwise), same "only at the edge" idea as everywhere else.
      if ((e.target as HTMLElement)?.tagName === 'TEXTAREA') {
        const area = e.target as HTMLTextAreaElement
        const start = area.selectionStart ?? 0
        const end = area.selectionEnd ?? area.value.length
        const atFirstLine = area.value.slice(0, start).indexOf('\n') === -1
        const atLastLine = area.value.slice(end).indexOf('\n') === -1
        if (e.key === 'ArrowUp' && !atFirstLine) return
        if (e.key === 'ArrowDown' && !atLastLine) return
      }
      const focusable = Array.from(rootRef.current.querySelectorAll<HTMLElement>('[data-nav-item]'))
      const i = focusable.indexOf(document.activeElement as HTMLElement)
      if (i === -1) return
      e.preventDefault()
      const next = Math.min(Math.max(i + (e.key === 'ArrowDown' ? 1 : -1), 0), focusable.length - 1)
      focusable[next]?.focus()
    }
  }

  return (
    <div className={styles.overlay} onClick={onCancel} onKeyDown={handleKeyDown}>
      <div ref={rootRef} className={styles.dialog} onClick={e => e.stopPropagation()}>
        {title && <h3 className={styles.title}>{title}</h3>}
        <p className={styles.message}>{message}</p>
        {mode === 'prompt' && (
          multiline ? (
            <textarea
              ref={inputRef as React.RefObject<HTMLTextAreaElement>}
              className={styles.textarea}
              data-nav-item
              value={value}
              onChange={e => setValue(e.target.value)}
              rows={8}
            />
          ) : (
            <input
              ref={inputRef as React.RefObject<HTMLInputElement>}
              className={styles.input}
              data-nav-item
              value={value}
              onChange={e => setValue(e.target.value)}
            />
          )
        )}
        <div className={styles.actions}>
          {!hideCancel && (
            <button type="button" className={styles.btnSecondary} data-nav-item onClick={onCancel}>
              {cancelLabel ?? 'Отмена'}
            </button>
          )}
          <button
            type="button"
            className={`${styles.btnPrimary}${danger ? ' ' + styles.danger : ''}`}
            data-nav-item
            onClick={() => onConfirm(value)}
          >
            {confirmLabel ?? (mode === 'confirm' ? 'Да' : 'Сохранить')}
          </button>
        </div>
      </div>
    </div>
  )
}
