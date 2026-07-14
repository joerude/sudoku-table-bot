package bot

import "testing"

func TestParseIDOrigin(t *testing.T) {
	cases := []struct {
		in     string
		id     int64
		origin string
	}{
		{"12", 12, ""},
		{"12:duel:hard", 12, "duel:hard"},
		{"7:play", 7, "play"},
		{"", 0, ""},
		{"34:game:medium:hardcore", 34, "game:medium:hardcore"},
	}
	for _, tc := range cases {
		id, origin := parseIDOrigin(tc.in)
		if id != tc.id || origin != tc.origin {
			t.Errorf("parseIDOrigin(%q) = (%d, %q), want (%d, %q)",
				tc.in, id, origin, tc.id, tc.origin)
		}
	}
}
