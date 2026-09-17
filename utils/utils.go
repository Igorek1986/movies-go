package utils

import (
	"github.com/agnivade/levenshtein"
)

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func Filter[T any](arr []T, fn func(i int, e T) bool) []T {
	var list []T
	for i, t := range arr {
		if !fn(i, t) {
			list = append(list, t)
		}
	}
	return list
}

// SimilarStr returns true if a and b are equal or within a small edit distance.
// Uses Levenshtein distance; for strings shorter than 4 runes requires exact match.
//
// The allowed distance grows with length (minLen/5+1) to tolerate transliteration/
// punctuation drift in longer titles, but is capped at 3: uncapped, a long shared
// prefix ("Место преступления: <city>" — a common Russian franchise-naming pattern
// for various *CSI*-style shows) let two different titles' distinguishing suffix
// differ by 5+ characters and still pass, matching e.g. "CSI: Miami" to "Cannes
// Confidential" (see dev/rutracker.md). Two genuinely-the-same titles rarely
// differ by more than a few characters regardless of overall length.
func SimilarStr(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	ra, rb := []rune(a), []rune(b)
	minLen := len(ra)
	if len(rb) < minLen {
		minLen = len(rb)
	}
	if minLen < 4 {
		return false
	}
	maxDist := min(minLen/5+1, 3)
	dist := levenshtein.ComputeDistance(a, b)
	return dist <= maxDist
}
