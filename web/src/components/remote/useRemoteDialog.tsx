import { useRef, useState } from 'react'
import { RemoteDialog } from './RemoteDialog'

interface PendingDialog {
  mode: 'confirm' | 'prompt'
  title?: string
  message: string
  initialValue?: string
  multiline?: boolean
  danger?: boolean
  confirmLabel?: string
  resolve: (value: boolean | string | null) => void
}

interface ConfirmOpts {
  title?: string
  danger?: boolean
  confirmLabel?: string
}

interface PromptOpts {
  title?: string
  multiline?: boolean
}

// Promise-based drop-in for window.confirm()/window.prompt(), backed by
// RemoteDialog — lets ProfilesRemoteView pass confirmDialog/promptDialog as
// useProfilesPageState's confirmFn/promptFn with a purely mechanical edit at
// each call site (await confirmDialog(...) instead of confirm(...)).
export function useRemoteDialog() {
  const [pending, setPending] = useState<PendingDialog | null>(null)
  // Whatever had focus when the dialog opened (the button that triggered
  // it — rename/delete/edit params/etc, dozens of different call sites
  // sharing this one hook) — restored on close so answering a confirm
  // doesn't strand focus back at the top of the row-nav.
  const triggerRef = useRef<HTMLElement | null>(null)

  function confirmDialog(message: string, opts?: ConfirmOpts): Promise<boolean> {
    triggerRef.current = document.activeElement as HTMLElement | null
    return new Promise(resolve => {
      setPending({ mode: 'confirm', message, resolve: v => resolve(v as boolean), ...opts })
    })
  }

  function promptDialog(message: string, initialValue = '', opts?: PromptOpts): Promise<string | null> {
    triggerRef.current = document.activeElement as HTMLElement | null
    return new Promise(resolve => {
      setPending({ mode: 'prompt', message, initialValue, resolve: v => resolve(v as string | null), ...opts })
    })
  }

  function settle(value: boolean | string | null) {
    pending?.resolve(value)
    setPending(null)
    triggerRef.current?.focus()
    triggerRef.current = null
  }

  const dialogEl = pending ? (
    <RemoteDialog
      title={pending.title}
      message={pending.message}
      mode={pending.mode}
      initialValue={pending.initialValue}
      multiline={pending.multiline}
      danger={pending.danger}
      confirmLabel={pending.confirmLabel}
      onConfirm={value => settle(pending.mode === 'confirm' ? true : (value ?? null))}
      onCancel={() => settle(pending.mode === 'confirm' ? false : null)}
    />
  ) : null

  return { dialogEl, confirmDialog, promptDialog }
}
