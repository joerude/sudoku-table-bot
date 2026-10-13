package domain

import "testing"

func TestWinStreak(t *testing.T) {
	cases := []struct {
		ranks []int
		want  int
	}{
		{[]int{1, 1, 2, 1}, 2},
		{[]int{2, 1, 1}, 0},
		{[]int{1, 1, 1}, 3},
		{nil, 0},
	}
	for _, c := range cases {
		if got := WinStreak(c.ranks); got != c.want {
			t.Errorf("WinStreak(%v)=%d want %d", c.ranks, got, c.want)
		}
	}
}

func TestDayStreak(t *testing.T) {
	// played today, yesterday, two days ago → 3
	dates := []string{"2026-06-24", "2026-06-23", "2026-06-22"}
	if got := DayStreak(dates, "2026-06-24"); got != 3 {
		t.Errorf("consecutive: got %d want 3", got)
	}
	// gap breaks it: today + 3 days ago → 1
	if got := DayStreak([]string{"2026-06-24", "2026-06-21"}, "2026-06-24"); got != 1 {
		t.Errorf("gap: got %d want 1", got)
	}
	// not played today but yesterday → streak still alive, counts back
	if got := DayStreak([]string{"2026-06-23", "2026-06-22"}, "2026-06-24"); got != 2 {
		t.Errorf("ended-yesterday: got %d want 2", got)
	}
	// last play older than yesterday → broken
	if got := DayStreak([]string{"2026-06-21"}, "2026-06-24"); got != 0 {
		t.Errorf("stale: got %d want 0", got)
	}
	if got := DayStreak(nil, "2026-06-24"); got != 0 {
		t.Errorf("empty: got %d want 0", got)
	}
}

func TestBadges(t *testing.T) {
	all := Badges(BadgeInput{Wins: 50, Games: 100, BestSecs: 90, WinStreak: 3, DayStreak: 7, SeasonsWon: 1})
	for _, want := range []string{"🏅", "🔥", "⚡", "💪", "💯", "🎯", "📅"} {
		found := false
		for _, b := range all {
			if b == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Badges full set missing %q (got %v)", want, all)
		}
	}
	if none := Badges(BadgeInput{Wins: 1, Games: 1}); len(none) != 0 {
		t.Errorf("no badges expected, got %v", none)
	}
	// BestSecs==0 (no timed games) must NOT grant the sub-2:00 badge
	if b := Badges(BadgeInput{BestSecs: 0}); len(b) != 0 {
		t.Errorf("BestSecs 0 should grant nothing, got %v", b)
	}
}

func TestBadgeProgress(t *testing.T) {
	ps := BadgeProgress(BadgeInput{Wins: 34, Games: 60, BestSecs: 0, WinStreak: 2, DayStreak: 3, SeasonsWon: 2})
	by := map[string]BadgeStatus{}
	for _, p := range ps {
		by[p.Emoji] = p
	}
	// 🏅 earned with the actual championship count carried in Cur.
	if c := by["🏅"]; !c.Earned || c.Cur != 2 {
		t.Errorf("🏅: want earned cur=2, got %+v", c)
	}
	// 💯 locked, progress 34/50.
	if c := by["💯"]; c.Earned || c.Cur != 34 || c.Target != 50 {
		t.Errorf("💯: want locked 34/50, got %+v", c)
	}
	// ⚡ locked and flagged as a time badge (Cur==0 means no timed solves yet).
	if c := by["⚡"]; c.Earned || !c.Time {
		t.Errorf("⚡: want locked time badge, got %+v", c)
	}
	// Badges() must agree with BadgeProgress earned flags, same order.
	var earned []string
	for _, p := range ps {
		if p.Earned {
			earned = append(earned, p.Emoji)
		}
	}
	got := Badges(BadgeInput{Wins: 34, Games: 60, BestSecs: 0, WinStreak: 2, DayStreak: 3, SeasonsWon: 2})
	if len(got) != len(earned) {
		t.Fatalf("Badges vs BadgeProgress mismatch: %v vs %v", got, earned)
	}
	for i := range got {
		if got[i] != earned[i] {
			t.Errorf("order mismatch at %d: %q vs %q", i, got[i], earned[i])
		}
	}
}

// A badge keyed on the best run is kept after the streak breaks and is not
// re-earned by the next win, so it is announced only once.
func TestBestRunBadgesAreSticky(t *testing.T) {
	hist := []int{1, 1, 1, 2} // chronological: won 3, then lost
	atPeak := Badges(BadgeInput{WinStreak: LongestWinRun(hist[:3])})
	afterLoss := Badges(BadgeInput{WinStreak: LongestWinRun(hist)})
	afterWin := Badges(BadgeInput{WinStreak: LongestWinRun(append(hist, 1))})
	for name, got := range map[string][]string{"peak": atPeak, "loss": afterLoss, "win": afterWin} {
		if len(got) != 1 || got[0] != "🔥" {
			t.Errorf("%s: want [🔥], got %v", name, got)
		}
	}
}

func TestLongestDayStreak(t *testing.T) {
	cases := []struct {
		name  string
		dates []string
		want  int
	}{
		{"empty", nil, 0},
		{"single", []string{"2026-07-05"}, 1},
		{"duplicates collapse", []string{"2026-07-05", "2026-07-05"}, 1},
		{"run of three, any order", []string{"2026-07-05", "2026-07-03", "2026-07-04"}, 3},
		{"older run is longer", []string{"2026-06-01", "2026-06-02", "2026-06-03", "2026-06-04", "2026-07-05"}, 4},
		{"gap splits", []string{"2026-07-01", "2026-07-02", "2026-07-04", "2026-07-05"}, 2},
		{"month boundary", []string{"2026-06-30", "2026-07-01"}, 2},
	}
	for _, tc := range cases {
		if got := LongestDayStreak(tc.dates); got != tc.want {
			t.Errorf("%s: want %d, got %d", tc.name, tc.want, got)
		}
	}
}
