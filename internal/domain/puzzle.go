package domain

import "strings"

// EncodePuzzle packs a grid into 81 digits row by row, 0 for an empty cell:
// the form games.puzzle stores.
func EncodePuzzle(g [9][9]int) string {
	var b strings.Builder
	b.Grow(81)
	for _, row := range g {
		for _, v := range row {
			b.WriteByte(byte('0' + v))
		}
	}
	return b.String()
}

// ParsePuzzle reads EncodePuzzle's form back; false for anything else.
func ParsePuzzle(s string) ([9][9]int, bool) {
	var g [9][9]int
	if len(s) != 81 {
		return g, false
	}
	for i := 0; i < 81; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return [9][9]int{}, false
		}
		g[i/9][i%9] = int(c - '0')
	}
	return g, true
}
