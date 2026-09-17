package utils

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ClearStr lowercases, folds Latin diacritics to their base letter (é→e,
// š→s, ...) via Unicode NFD decomposition + combining-mark removal, and
// keeps only [0-9a-zа-яё]. Diacritics used to be dropped outright (not
// folded), which both rejected correct matches ("Léo" → "lo", too short to
// compare) and let unrelated titles collide ("Leoš" → "leo", same as
// "Leo") — see dev/rutracker.md for the case that surfaced this.
func ClearStr(str string) string {
	str = norm.NFD.String(strings.ToLower(str))
	var b strings.Builder
	for _, r := range str {
		if unicode.Is(unicode.Mn, r) {
			continue // combining mark (the decomposed accent) — drop, keep base letter
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') || r == 'ё' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
