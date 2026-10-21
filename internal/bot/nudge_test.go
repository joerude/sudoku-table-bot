package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

var utc6 = time.FixedZone("UTC+6", 6*3600)

// at builds a local time in October 2026 (the 5th, 12th and 19th are Mondays).
func at(day, hh, mm int) time.Time { return time.Date(2026, 10, day, hh, mm, 0, 0, utc6) }

func TestFirstPerDay(t *testing.T) {
	starts := []string{
		"2026-10-19 08:00:00", // 14:00 on the 19th
		"2026-10-19 05:00:00", // 11:00 on the 19th: that day's first
		"2026-10-19 19:30:00", // 01:30 on the 20th: still the 19th's session
		"2026-10-20 21:00:00", // 03:00 on the 21st: the 20th's only game
	}
	got := firstPerDay(starts, utc6)
	if len(got) != 2 || !got[0].Equal(at(19, 11, 0)) || !got[1].Equal(at(21, 3, 0)) {
		t.Errorf("firstPerDay = %v", got)
	}
}

func TestNudgeClock(t *testing.T) {
	thin := []time.Time{at(19, 14, 0), at(20, 15, 0), at(21, 16, 0), at(22, 17, 0)}
	if got, ok := nudgeClock(thin, time.Monday, "12:45"); !ok || got != "12:45" {
		t.Errorf("thin history keeps the configured time: %q %v", got, ok)
	}

	sparse := []time.Time{
		at(5, 19, 1), at(12, 21, 50), at(19, 22, 34), // Mondays
		at(8, 11, 50), at(15, 15, 52), at(22, 17, 51), // Thursdays
		at(7, 13, 0), at(21, 14, 0), // two Wednesdays
	}
	if got, ok := nudgeClock(sparse, time.Monday, "12:45"); !ok || got != "21:50" {
		t.Errorf("Monday = %q %v, want the median first game 21:50", got, ok)
	}
	if got, ok := nudgeClock(sparse, time.Thursday, "12:45"); !ok || got != "15:55" {
		t.Errorf("Thursday = %q %v, want 15:52 rounded up to 15:55", got, ok)
	}
	// A chat drifting away (8 game days in six weeks) is not left alone on
	// its rare weekdays: the median over all days stands in.
	if got, ok := nudgeClock(sparse, time.Sunday, "12:45"); !ok || got != "15:55" {
		t.Errorf("sparse chat, rare weekday = %q %v, want the overall median 15:52 → 15:55", got, ok)
	}

	// An active chat (15+ game days) keeps its quiet weekdays quiet.
	active := append([]time.Time{}, sparse...)
	for d := 1; d <= 30 && len(active) < 16; d++ {
		if t := at(d, 14, 0); t.Weekday() == time.Tuesday || t.Weekday() == time.Friday {
			active = append(active, t)
		}
	}
	if _, ok := nudgeClock(active, time.Sunday, "12:45"); ok {
		t.Errorf("an active chat that never plays on Sundays gets no Sunday nudge")
	}
	if _, ok := nudgeClock(active, time.Wednesday, "12:45"); ok {
		t.Errorf("two Wednesdays out of six are not this chat's habit")
	}

	even := []time.Time{at(5, 19, 0), at(12, 20, 0), at(19, 21, 0), at(26, 22, 0), at(6, 9, 0)}
	if got, _ := nudgeClock(even, time.Monday, "12:45"); got != "20:00" {
		t.Errorf("even count takes the lower middle, a time somebody really played: %q", got)
	}

	// Friday sessions that start after midnight belong to Friday.
	late := []time.Time{at(10, 0, 20), at(17, 0, 35), at(24, 0, 30), at(6, 14, 0), at(7, 14, 0)}
	if got, ok := nudgeClock(late, time.Friday, "12:45"); !ok || got != "23:55" {
		t.Errorf("Friday night owls = %q %v, want the latest slot of the day 23:55", got, ok)
	}
}

func TestAvoidDigest(t *testing.T) {
	cases := []struct {
		at   string
		wd   time.Weekday
		want string
	}{
		{"10:00", time.Monday, "10:15"},
		{"09:45", time.Monday, "10:15"},
		{"10:15", time.Monday, "10:15"},
		{"09:40", time.Monday, "09:40"},
		{"10:00", time.Tuesday, "10:00"},
	}
	for _, c := range cases {
		if got := avoidDigest(c.at, c.wd); got != c.want {
			t.Errorf("avoidDigest(%s, %s) = %s, want %s", c.at, c.wd, got, c.want)
		}
	}
}

func TestNudgeDue(t *testing.T) {
	cases := []struct {
		now, at    string
		due, stale bool
	}{
		{"15:24", "15:25", false, false},
		{"15:25", "15:25", true, false},
		{"18:25", "15:25", true, false},
		{"18:26", "15:25", true, true},
		{"9:30", "09:00", true, false},
	}
	for _, c := range cases {
		due, stale := nudgeDue(c.now, c.at)
		if due != c.due || stale != c.stale {
			t.Errorf("nudgeDue(%s, %s) = %v, %v; want %v, %v", c.now, c.at, due, stale, c.due, c.stale)
		}
	}
}

func TestNudgeSilenced(t *testing.T) {
	ignored := func(t time.Time) nudgeRecord { return nudgeRecord{At: t} }
	played := func(t time.Time) nudgeRecord { return nudgeRecord{At: t, Played: true} }
	cases := []struct {
		name string
		recs []nudgeRecord
		now  time.Time
		want bool
	}{
		{"no history", nil, at(21, 15, 0), false},
		{"one ignored", []nudgeRecord{ignored(at(20, 15, 0))}, at(21, 15, 0), false},
		{"last one worked", []nudgeRecord{played(at(20, 15, 0)), ignored(at(19, 15, 0))}, at(21, 15, 0), false},
		{"two ignored: next day quiet", []nudgeRecord{ignored(at(20, 15, 0)), ignored(at(19, 15, 0))}, at(21, 15, 0), true},
		{"two ignored: second day quiet", []nudgeRecord{ignored(at(20, 15, 0)), ignored(at(19, 15, 0))}, at(22, 15, 0), true},
		{"two ignored: third day speaks again", []nudgeRecord{ignored(at(20, 15, 0)), ignored(at(19, 15, 0))}, at(23, 9, 0), false},
	}
	for _, c := range cases {
		if got := nudgeSilenced(c.recs, c.now, utc6); got != c.want {
			t.Errorf("%s: nudgeSilenced = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSmartNudgeText(t *testing.T) {
	seen := map[string]bool{}
	for day := 19; day < 25; day++ {
		got := smartNudgeText("", at(day, 15, 0))
		seen[got] = true
		if !strings.Contains(got, "/play") || strings.ContainsAny(got, "—–") || strings.Contains(got, "🌙") {
			t.Errorf("bad nudge %q", got)
		}
	}
	if len(seen) < 3 {
		t.Errorf("the nudge must not repeat the same words every day, got %d variants", len(seen))
	}
	if got := smartNudgeText("⏭ <b>Вика</b> в 1 очке от 2-го места: хватит одной победы", at(19, 15, 0)); got !=
		"⏭ <b>Вика</b> в 1 очке от 2-го места: хватит одной победы\nПартию? /play" {
		t.Errorf("a live stake leads the nudge: %q", got)
	}
}

// nudgeChat is a chat with two players and no games: thin history, so the
// smart nudge falls back to the configured 21:00.
func nudgeChat(t *testing.T, chat int64) (*Bot, *storage.Store, *time.Location) {
	t.Helper()
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	st.RegisterPlayer(chat, 1, "Alice")
	st.RegisterPlayer(chat, 2, "Bob")
	ch, _ := st.GetChat(chat)
	return &Bot{st: st}, st, loadLoc(ch.TZ)
}

func lastNudge(t *testing.T, st *storage.Store, chat int64) []storage.Event {
	t.Helper()
	ev, err := st.RecentEvents(chat, evNudgeSent, 10)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestRemindDailySmartFallsBackOnThinHistory(t *testing.T) {
	const chat = int64(-7501)
	b, st, loc := nudgeChat(t, chat)
	day := time.Date(2026, 10, 21, 20, 59, 0, 0, loc)
	b.remindDailyAt(day)
	if ev := lastNudge(t, st, chat); len(ev) != 0 {
		t.Fatalf("not 21:00 yet: %+v", ev)
	}
	b.remindDailyAt(day.Add(time.Minute))
	ev := lastNudge(t, st, chat)
	if len(ev) != 1 || ev[0].Detail != "smart" {
		t.Fatalf("want one smart nudge at 21:00, got %+v", ev)
	}
	b.remindDailyAt(day.Add(30 * time.Minute))
	if ev := lastNudge(t, st, chat); len(ev) != 1 {
		t.Errorf("at most one nudge a day, got %d", len(ev))
	}
}

func TestRemindDailySilencedAfterTwoIgnored(t *testing.T) {
	const chat = int64(-7502)
	b, st, loc := nudgeChat(t, chat)
	for _, d := range []int{19, 20} {
		st.LogEvent(storage.Event{ChatID: chat, Kind: evNudgeSent, Detail: "smart",
			At: fmtDBTime(time.Date(2026, 10, d, 21, 0, 0, 0, loc))})
	}
	b.remindDailyAt(time.Date(2026, 10, 21, 21, 5, 0, 0, loc))
	if ev := lastNudge(t, st, chat); len(ev) != 2 {
		t.Errorf("two ignored nudges in a row: the bot keeps quiet, got %d events", len(ev))
	}
	if ch, _ := st.GetChat(chat); ch.LastDaily != "2026-10-21" {
		t.Errorf("a silenced day still counts as handled, last_daily = %q", ch.LastDaily)
	}
}

func TestRemindDailySilenceCountsOnlySmartNudges(t *testing.T) {
	const chat = int64(-7504)
	b, st, loc := nudgeChat(t, chat)
	for _, d := range []int{19, 20} { // ignored classic nudges from before the switch
		st.LogEvent(storage.Event{ChatID: chat, Kind: evNudgeSent, Detail: "fixed",
			At: fmtDBTime(time.Date(2026, 10, d, 21, 0, 0, 0, loc))})
	}
	b.remindDailyAt(time.Date(2026, 10, 21, 21, 0, 0, 0, loc))
	if ev := lastNudge(t, st, chat); len(ev) != 3 || ev[0].Detail != "smart" {
		t.Errorf("old classic nudges must not silence the smart one: %+v", ev)
	}
}

func TestRemindDailyFixedWhenSwitchedOff(t *testing.T) {
	const chat = int64(-7503)
	b, st, loc := nudgeChat(t, chat)
	st.SetFeature(chat, featNudge, false)
	for _, d := range []int{19, 20} { // ignored nudges do not silence the classic one
		st.LogEvent(storage.Event{ChatID: chat, Kind: evNudgeSent, Detail: "fixed",
			At: fmtDBTime(time.Date(2026, 10, d, 21, 0, 0, 0, loc))})
	}
	b.remindDailyAt(time.Date(2026, 10, 21, 21, 0, 0, 0, loc))
	ev := lastNudge(t, st, chat)
	if len(ev) != 3 || ev[0].Detail != "fixed" {
		t.Errorf("switched off: the classic nudge at 21:00, got %+v", ev)
	}
}
