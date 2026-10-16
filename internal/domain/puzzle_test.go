package domain

import "testing"

func TestPuzzleRoundTrip(t *testing.T) {
	var g [9][9]int
	g[0][3], g[4][4], g[8][8] = 6, 4, 9
	s := EncodePuzzle(g)
	if len(s) != 81 || s[3] != '6' || s[40] != '4' || s[80] != '9' {
		t.Fatalf("encoded %q", s)
	}
	back, ok := ParsePuzzle(s)
	if !ok || back != g {
		t.Errorf("round trip lost data: %v", back)
	}
	for _, bad := range []string{"", "12", s[:80] + "x"} {
		if _, ok := ParsePuzzle(bad); ok {
			t.Errorf("ParsePuzzle(%q) must fail", bad)
		}
	}
}
