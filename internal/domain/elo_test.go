package domain

import "testing"

func TestEloUpdateEqualRatings(t *testing.T) {
	w, l := EloUpdate(1000, 1000)
	if w != 1016 || l != 984 {
		t.Errorf("equal ratings: want 1016/984, got %d/%d", w, l)
	}
}

func TestEloUpdateUnderdogPaysMore(t *testing.T) {
	// An underdog win must move ratings more than a favourite win.
	underdog, _ := EloUpdate(900, 1100)
	favourite, _ := EloUpdate(1100, 900)
	if underdog-900 <= favourite-1100 {
		t.Errorf("underdog gain %d should exceed favourite gain %d",
			underdog-900, favourite-1100)
	}
}

func TestEloUpdateConservesPoints(t *testing.T) {
	w, l := EloUpdate(1050, 970)
	if w+l != 1050+970 {
		t.Errorf("points not conserved: %d + %d != %d", w, l, 1050+970)
	}
}

func TestEloRatingsFoldsHistory(t *testing.T) {
	// A beats B twice: A above start, B below, total conserved.
	pairs := [][2]int64{{1, 2}, {1, 2}}
	r := EloRatings(pairs)
	if r[1] <= EloStart || r[2] >= EloStart {
		t.Errorf("want A > %d > B, got A=%d B=%d", EloStart, r[1], r[2])
	}
	if r[1]+r[2] != 2*EloStart {
		t.Errorf("total not conserved: %d", r[1]+r[2])
	}
	// The second win against a weaker opponent pays less than the first.
	first := EloRatings(pairs[:1])
	if r[1]-first[1] >= first[1]-EloStart {
		t.Errorf("second win gain %d should be below first win gain %d",
			r[1]-first[1], first[1]-EloStart)
	}
}

func TestEloRatingsEmpty(t *testing.T) {
	if r := EloRatings(nil); len(r) != 0 {
		t.Errorf("want empty map, got %v", r)
	}
}

func TestLongestWinRun(t *testing.T) {
	cases := []struct {
		name  string
		ranks []int
		want  int
	}{
		{"empty", nil, 0},
		{"no wins", []int{2, 3, 2}, 0},
		{"all wins", []int{1, 1, 1}, 3},
		{"run in middle", []int{2, 1, 1, 3, 1}, 2},
		{"dnf breaks run", []int{1, 0, 1}, 1},
	}
	for _, c := range cases {
		if got := LongestWinRun(c.ranks); got != c.want {
			t.Errorf("%s: want %d, got %d", c.name, c.want, got)
		}
	}
}
