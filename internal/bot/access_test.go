package bot

import "testing"

func TestDBAccessAllowed(t *testing.T) {
	const owner, other = int64(42), int64(7)
	cases := []struct {
		name       string
		ownerID    int64
		senderID   int64
		private    bool
		groupAdmin bool
		want       bool
	}{
		{"owner in private", owner, owner, true, false, true},
		{"owner in group, not admin", owner, owner, false, false, true},
		{"owner in group, admin", owner, owner, false, true, true},
		{"stranger in private", owner, other, true, false, false},
		{"stranger in private, claims admin", owner, other, true, true, false},
		{"group admin in group", owner, other, false, true, true},
		{"plain member in group", owner, other, false, false, false},
		{"no owner configured, private", 0, other, true, false, false},
		{"no owner configured, sender id 0", 0, 0, true, false, false},
		{"no owner configured, group admin", 0, other, false, true, true},
		{"no owner configured, plain member", 0, other, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dbAccessAllowed(tc.ownerID, tc.senderID, tc.private, tc.groupAdmin); got != tc.want {
				t.Errorf("dbAccessAllowed(%d, %d, private=%v, admin=%v) = %v, want %v",
					tc.ownerID, tc.senderID, tc.private, tc.groupAdmin, got, tc.want)
			}
		})
	}
}
