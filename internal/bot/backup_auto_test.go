package bot

import (
	"testing"
	"time"
)

func TestWeeklyBackupDue(t *testing.T) {
	loc := time.FixedZone("test", 6*3600)
	at := func(day, hour, min int) time.Time { // 2026-11-23 is a Monday
		return time.Date(2026, 11, day, hour, min, 0, 0, loc)
	}
	const thisWeek, prevWeek = "2026-W48", "2026-W47"
	cases := []struct {
		name string
		now  time.Time
		last string
		want bool
	}{
		{"Monday after the hour, never sent", at(23, 10, 0), "", true},
		{"Monday evening, last week was sent", at(23, 21, 30), prevWeek, true},
		{"Monday before the hour", at(23, 9, 59), prevWeek, false},
		{"same week already sent, later on Monday", at(23, 18, 0), thisWeek, false},
		{"Tuesday of an unsent week catches up", at(24, 12, 0), prevWeek, true},
		{"Sunday of a sent week", at(29, 23, 0), thisWeek, false},
		{"next Monday", at(30, 10, 5), thisWeek, true},
	}
	for _, tc := range cases {
		if got := weeklyBackupDue(tc.now, tc.last); got != tc.want {
			t.Errorf("%s: due = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := backupWeek(at(23, 10, 0)); got != thisWeek {
		t.Errorf("backupWeek = %q, want %q", got, thisWeek)
	}
}
