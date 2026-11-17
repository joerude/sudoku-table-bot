package domain

import (
	"reflect"
	"testing"
)

func TestCoopTiers(t *testing.T) {
	cases := []struct {
		min  int
		want []int
	}{
		{66, []int{13, 23, 33}},
		{5, []int{1, 2, 3}}, // 1, 1.75 and 2.5 rounded, kept strictly increasing
		{1, []int{1, 2, 3}},
		{0, nil},
		{-3, nil},
	}
	for _, c := range cases {
		if got := CoopTiers(c.min); !reflect.DeepEqual(got, c.want) {
			t.Errorf("CoopTiers(%d) = %v, want %v", c.min, got, c.want)
		}
	}
}

func TestCoopStars(t *testing.T) {
	tiers := []int{13, 23, 33}
	cases := []struct{ n, want int }{
		{0, 0}, {12, 0}, {13, 1}, {22, 1}, {23, 2}, {33, 3}, {70, 3},
	}
	for _, c := range cases {
		if got := CoopStars(c.n, tiers); got != c.want {
			t.Errorf("CoopStars(%d) = %d, want %d", c.n, got, c.want)
		}
	}
	if got := CoopStars(5, nil); got != 0 {
		t.Errorf("no tiers: CoopStars = %d, want 0", got)
	}
}
