// Lets whichever settings screen is currently drilled into (see
// ProfilesRemoteView's goBack) register "do I have unsaved changes, and how
// do I save them" — checked right before actually navigating back out, so a
// save prompt can be shown instead of silently discarding local edits.
// ProfilesRemoteView only ever has one drill-down screen mounted at a time,
// so a single module-level slot is enough — no need for a list/registry.
export interface UnsavedChangesGuard {
  isDirty: () => boolean
  save: () => Promise<void>
}

let guard: UnsavedChangesGuard | null = null

export function registerUnsavedChangesGuard(g: UnsavedChangesGuard | null): void {
  guard = g
}

export function getUnsavedChangesGuard(): UnsavedChangesGuard | null {
  return guard
}
