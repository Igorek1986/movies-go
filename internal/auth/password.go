package auth

import (
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	return string(b), err
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// passwordMinReqHint — the concrete minimum restated in every rejection
// message below, so a failing password always tells the user exactly what
// to do next instead of just "too weak".
const passwordMinReqHint = "минимум 8 символов, строчные и заглавные буквы, цифры"

// ValidatePasswordStrength enforces the site-wide password policy: at least
// 8 characters, at least one lowercase letter, one uppercase letter and one
// digit (a special character is welcome but not required), not a
// known-weak password, and not a trivial sequence/repeat (e.g. "12345678",
// "abcdefgh", "aaaaaaaa"). blocklist is the admin-editable list of banned
// passwords (see store.PasswordBlocklist / the password_blocklist setting),
// compared case-insensitively — this function has no built-in list, callers
// are expected to load it from settings. Used by registration, self-service
// change-password and the reset-password flow — see handleRegister/
// handleChangePassword in api/auth.go and handleAPIResetPassword in
// api/web_pages.go. Mirrors web/src/utils/passwordStrength.ts for real-time
// UI feedback; keep both in sync. Returns "" if acceptable, otherwise a
// Russian message ready to show the user.
func ValidatePasswordStrength(password string, blocklist []string) string {
	if len(password) < 8 {
		return "Слишком короткий — нужно " + passwordMinReqHint
	}
	lower := strings.ToLower(password)
	for _, w := range blocklist {
		if strings.ToLower(strings.TrimSpace(w)) == lower {
			return "Такой пароль слишком распространён — нужно " + passwordMinReqHint
		}
	}
	var hasLower, hasUpper, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLower || !hasUpper || !hasDigit {
		return "Нужно: " + passwordMinReqHint
	}
	if isSequentialOrRepeated(password) {
		return "Пароль слишком предсказуем — нужно " + passwordMinReqHint
	}
	return ""
}

// isSequentialOrRepeated catches "12345678"/"87654321"/"abcdefgh" (every
// character one step from its neighbor, ascending or descending) and
// "aaaaaaaa" (every character identical) — passwords that pass the
// char-class check above but are still trivially guessable.
func isSequentialOrRepeated(password string) bool {
	runes := []rune(password)
	allSame, ascending, descending := true, true, true
	for i := 1; i < len(runes); i++ {
		if runes[i] != runes[0] {
			allSame = false
		}
		if runes[i]-runes[i-1] != 1 {
			ascending = false
		}
		if runes[i-1]-runes[i] != 1 {
			descending = false
		}
	}
	return allSame || ascending || descending
}
