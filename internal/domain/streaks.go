package domain

import "time"

// WinStreak counts leading wins (rank==1) in a newest-first rank slice.
func WinStreak(ranks []int) int {
	n := 0
	for _, r := range ranks {
		if r != 1 {
			break
		}
		n++
	}
	return n
}

// LongestWinRun returns the longest run of consecutive wins (rank==1) in a
// chronologically ordered rank slice — used for the season-end awards.
func LongestWinRun(ranks []int) int {
	best, cur := 0, 0
	for _, r := range ranks {
		if r != 1 {
			cur = 0
			continue
		}
		cur++
		if cur > best {
			best = cur
		}
	}
	return best
}

// addDays returns the YYYY-MM-DD date n days from the given date; on parse
// failure it returns the input unchanged (callers treat that as a broken chain).
func addDays(date string, n int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// DayStreak counts consecutive calendar days with at least one play, anchored
// at the most recent played day, but only if that day is today or yesterday
// (otherwise the streak is considered broken and returns 0). dates may contain
// duplicates and any order; today is YYYY-MM-DD in the chat's timezone.
func DayStreak(dates []string, today string) int {
	set := make(map[string]bool, len(dates))
	for _, d := range dates {
		set[d] = true
	}
	cur := today
	if !set[cur] {
		cur = addDays(today, -1)
		if !set[cur] {
			return 0
		}
	}
	n := 0
	for set[cur] {
		n++
		cur = addDays(cur, -1)
	}
	return n
}

// BadgeInput is the cross-season career data a player's badges are computed from.
type BadgeInput struct {
	Wins       int
	Games      int
	BestSecs   int
	WinStreak  int
	DayStreak  int
	SeasonsWon int
}

// BadgeStatus is one badge's earned state and progress toward its threshold.
// Cur/Target drive the "cur/target" progress hint on locked badges. Time is set
// only for the ⚡ speed badge, where lower is better and Cur/Target don't apply.
type BadgeStatus struct {
	Emoji  string
	Earned bool
	Cur    int
	Target int
	Time   bool
}

// BadgeProgress returns every badge with its earned state and progress toward
// its threshold, in the fixed display order. Badges() is derived from this so
// thresholds and order live in exactly one place.
func BadgeProgress(in BadgeInput) []BadgeStatus {
	return []BadgeStatus{
		{Emoji: "🏅", Earned: in.SeasonsWon >= 1, Cur: in.SeasonsWon, Target: 1}, // season champion
		{Emoji: "🔥", Earned: in.WinStreak >= 3, Cur: in.WinStreak, Target: 3},   // hot streak
		{Emoji: "⚡", Earned: in.BestSecs > 0 && in.BestSecs < 120, Cur: in.BestSecs, Target: 120, Time: true}, // sub-2:00 solve
		{Emoji: "💪", Earned: in.Wins >= 10, Cur: in.Wins, Target: 10},   // 10+ wins
		{Emoji: "💯", Earned: in.Wins >= 50, Cur: in.Wins, Target: 50},   // 50+ wins
		{Emoji: "🎯", Earned: in.Games >= 100, Cur: in.Games, Target: 100}, // 100+ games
		{Emoji: "📅", Earned: in.DayStreak >= 7, Cur: in.DayStreak, Target: 7}, // week-long play streak
	}
}

// Badges returns the emoji badges a player has earned, in a fixed display order.
func Badges(in BadgeInput) []string {
	var b []string
	for _, s := range BadgeProgress(in) {
		if s.Earned {
			b = append(b, s.Emoji)
		}
	}
	return b
}
