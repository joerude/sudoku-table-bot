package bot

import "testing"

func TestOriginArgs(t *testing.T) {
	cases := []struct {
		name       string
		parts      []string
		difficulty string
		mode       string
	}{
		{"full", []string{"game", "hard", "original"}, "hard", "original"},
		{"defaults", []string{"duel"}, "medium", "hardcore"},
		{"difficulty only", []string{"duel", "extreme"}, "extreme", "hardcore"},
		{"bad difficulty", []string{"game", "nope", "original"}, "medium", "original"},
		{"bad mode", []string{"game", "easy", "nope"}, "easy", "hardcore"},
	}
	for _, tc := range cases {
		d, m := originArgs(tc.parts)
		if d != tc.difficulty || m != tc.mode {
			t.Errorf("%s: originArgs(%v) = (%q, %q), want (%q, %q)",
				tc.name, tc.parts, d, m, tc.difficulty, tc.mode)
		}
	}
}
