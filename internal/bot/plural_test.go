package bot

import "testing"

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "игр"}, {1, "игра"}, {2, "игры"}, {3, "игры"}, {4, "игры"}, {5, "игр"},
		{11, "игр"}, {12, "игр"}, {14, "игр"}, {21, "игра"}, {22, "игры"},
		{101, "игра"}, {111, "игр"}, {112, "игр"}, {-1, "игра"},
	}
	for _, tc := range cases {
		if got := plural(tc.n, "игра", "игры", "игр"); got != tc.want {
			t.Errorf("plural(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
