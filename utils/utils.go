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
	dist := levenshtein.ComputeDistance(a, b)
	return dist <= minLen/5+1
}
