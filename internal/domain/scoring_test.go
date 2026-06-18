package domain

import "testing"

func TestPointsForRank(t *testing.T) {
	table := DefaultPointsTable // [3,1,0]
	cases := []struct {
		rank, want int
	}{
		{1, 3},  // winner
		{2, 1},  // runner-up
		{3, 0},  // third
		{4, 0},  // beyond table -> 0
		{0, 0},  // invalid
		{-1, 0}, // invalid
	}
	for _, tc := range cases {
		if got := PointsForRank(table, tc.rank); got != tc.want {
			t.Errorf("PointsForRank(%v, %d) = %d, want %d", table, tc.rank, got, tc.want)
		}
	}
}

func TestPointsForRank_CustomTable(t *testing.T) {
	// A larger lobby table, e.g. for 5 players.
	table := []int{5, 3, 2, 1, 0}
	if got := PointsForRank(table, 3); got != 2 {
		t.Errorf("rank 3 = %d, want 2", got)
	}
	if got := PointsForRank(table, 6); got != 0 {
		t.Errorf("rank 6 = %d, want 0", got)
	}
}
