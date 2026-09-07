// Site-wide password policy, checked client-side for real-time feedback.
// Mirrors internal/auth/password.go's ValidatePasswordStrength — keep both
// in sync. The server re-validates independently; this is UX only.
// blocklist comes from GET /api/config's password_blocklist (admin-editable,
// see the password_blocklist setting) — see useAppConfig.
// Used by RegisterPage, ForceChangePasswordPage and ResetPasswordPage.

const MIN_REQ_HINT = 'минимум 8 символов, строчные и заглавные буквы, цифры'

function isSequentialOrRepeated(password: string): boolean {
  const codes = Array.from(password, c => c.codePointAt(0)!)
  let allSame = true, ascending = true, descending = true
  for (let i = 1; i < codes.length; i++) {
    if (codes[i] !== codes[0]) allSame = false
    if (codes[i] - codes[i - 1] !== 1) ascending = false
    if (codes[i - 1] - codes[i] !== 1) descending = false
  }
  return allSame || ascending || descending
}

// Returns "" if the password is acceptable, otherwise a message to show the user.
export function validatePasswordStrength(password: string, blocklist: string[] = []): string {
  if (password.length < 8) return `Слишком короткий — нужно ${MIN_REQ_HINT}`
  const lower = password.toLowerCase()
  if (blocklist.some(w => w.trim().toLowerCase() === lower)) {
    return `Такой пароль слишком распространён — нужно ${MIN_REQ_HINT}`
  }
  const hasLower = /\p{Ll}/u.test(password)
  const hasUpper = /\p{Lu}/u.test(password)
  const hasDigit = /\p{Nd}/u.test(password)
  if (!hasLower || !hasUpper || !hasDigit) return `Нужно: ${MIN_REQ_HINT}`
  if (isSequentialOrRepeated(password)) return `Пароль слишком предсказуем — нужно ${MIN_REQ_HINT}`
  return ''
}
