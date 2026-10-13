package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestSeasonNudgeKind(t *testing.T) {
	const dl = "2026-08-01 00:00:00"
	cases := []struct {
		name       string
		days       int
		hhmm, last string
		want       string
	}{
		{"far away", 12, "11:00", "", ""},
		{"week starts", 6, "10:30", "", "week"},
		{"week too early in the day", 6, "09:00", "", ""},
		{"week last day before final", 1, "12:00", "", "week"},
		{"week already sent", 3, "12:00", "week@" + dl, ""},
		{"final", 0, "10:30", "week@" + dl, "final"},
		{"final too early", 0, "08:00", "week@" + dl, ""},
		{"final already sent", 0, "23:00", "final@" + dl, ""},
		{"final replaces a missed week", 0, "11:00", "", "final"},
		{"week of the previous deadline", 5, "11:00", "week@2026-07-01 00:00:00", "week"},
		{"deadline passed", -1, "11:00", "", ""},
	}
	for _, tc := range cases {
		if got := seasonNudgeKind(tc.days, tc.hhmm, dl, tc.last); got != tc.want {
			t.Errorf("%s: seasonNudgeKind = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSeasonLastWeekText(t *testing.T) {
	rows := []storage.Standing{
		{Name: "Alice", Points: 40}, {Name: "Bob <b>", Points: 31}, {Name: "Cy", Points: 2},
	}
	got := seasonLastWeekText(7, rows, 40, 66)
	for _, want := range []string{"🏁", "Последняя неделя сезона 7", "Alice</b> 40", "Bob &lt;b&gt;</b> 31", "(-9)",
		"Игр: <b>40</b>/66", "не хватает <b>26</b> игр"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "Cy") {
		t.Errorf("third place must not be in the one-line post: %q", got)
	}
	tie := seasonLastWeekText(7, []storage.Standing{{Name: "A", Points: 5}, {Name: "B", Points: 5}}, 66, 66)
	if !strings.Contains(tie, "вровень") || strings.Contains(tie, "(-0)") {
		t.Errorf("tie rendering: %q", tie)
	}
	if strings.Contains(tie, "не хватает") {
		t.Errorf("floor met: no shortfall expected: %q", tie)
	}
	solo := seasonLastWeekText(7, []storage.Standing{{Name: "A", Points: 5}}, 65, 66)
	if strings.Contains(solo, "(") {
		t.Errorf("single player must have no gap: %q", solo)
	}
	if !strings.Contains(solo, "не хватает <b>1</b> игры") {
		t.Errorf("shortfall of one game: %q", solo)
	}
}

func TestSeasonFinalDayText(t *testing.T) {
	rows := []storage.Standing{
		{Name: "A", Points: 50}, {Name: "B", Points: 40}, {Name: "C", Points: 30}, {Name: "D", Points: 1},
	}
	got := seasonFinalDayText(7, rows)
	for _, want := range []string{"⏳", "Сегодня финал сезона 7", "🥇", "🥉"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, ">D<") {
		t.Errorf("only the top three are listed: %q", got)
	}
}

func TestSeasonExtendedText(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	until := time.Date(2026, 9, 1, 0, 0, 0, 0, loc).UTC()
	now := time.Date(2026, 8, 1, 0, 5, 0, 0, loc)
	se := &storage.Season{Number: 4, Target: 100, MinGames: 66}
	got := seasonExtendedText(se, 64, &storage.Standing{Name: "Joe", Points: 6}, until, now, loc)
	for _, want := range []string{"Сезон 4 продлён", "Новый финал: 31 августа", "(через 30 дней)",
		"не хватает <b>2</b> игр.", "Лидер: <b>Joe</b>, 6/100"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// nudgeFixture opens a chat with Alice and Bob and Alice's wins in the active
// season (3 points each) and a deadline of 1 Aug 2026 00:00 UTC.
func nudgeFixture(t *testing.T, chat int64, wins int) (*Bot, *storage.Season) {
	t.Helper()
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	alice, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	bob, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	for i := 0; i < wins; i++ {
		gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		if err != nil {
			t.Fatalf("CreatePendingGame: %v", err)
		}
		_ = st.AddPick(gid, alice.ID)
		_ = st.AddPick(gid, bob.ID)
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatalf("FinalizeGame: %v", err)
		}
	}
	if err := st.SetSeasonDeadline(se.ID, "2026-08-01 00:00:00"); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}
	return &Bot{st: st}, se
}

func TestCloseExpiredSeason_MarksWeekThenFinalOnce(t *testing.T) {
	const chat = int64(-9101)
	b, se := nudgeFixture(t, chat, 2)
	if err := b.st.UpdateSeasonMinGames(se.ID, 2); err != nil { // floor met: the final is real
		t.Fatalf("UpdateSeasonMinGames: %v", err)
	}
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}

	// 25 July 11:00 UTC: six days left, past the nudge time of day.
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	if got, _ := b.st.SeasonNudge(se.ID); got != "week@2026-08-01 00:00:00" {
		t.Errorf("marker after week nudge = %q", got)
	}
	// Final day, morning before the nudge time: nothing new.
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	if got, _ := b.st.SeasonNudge(se.ID); got != "week@2026-08-01 00:00:00" {
		t.Errorf("marker before final time = %q, want unchanged", got)
	}
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	if got, _ := b.st.SeasonNudge(se.ID); got != "final@2026-08-01 00:00:00" {
		t.Errorf("marker after final nudge = %q", got)
	}
}

// TestCloseExpiredSeason_NoFinalNudgeBelowFloor: short of the games floor the
// deadline extends the season instead of closing it, so "final today" would
// be a lie and is not sent (nor marked).
func TestCloseExpiredSeason_NoFinalNudgeBelowFloor(t *testing.T) {
	const chat = int64(-9104)
	b, se := nudgeFixture(t, chat, 2) // 2 games, floor stays at the default 66
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	if err := b.st.SetSeasonNudge(se.ID, "week@2026-08-01 00:00:00"); err != nil {
		t.Fatalf("SetSeasonNudge: %v", err)
	}
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	if got, _ := b.st.SeasonNudge(se.ID); got != "week@2026-08-01 00:00:00" {
		t.Errorf("marker = %q, want the final nudge skipped below the floor", got)
	}
}

func TestCloseExpiredSeason_NudgeSilentWithoutPoints(t *testing.T) {
	const chat = int64(-9102)
	b, se := nudgeFixture(t, chat, 0)
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	// Still marked, so a game on day two does not trigger a late "last week".
	if got, _ := b.st.SeasonNudge(se.ID); got != "week@2026-08-01 00:00:00" {
		t.Errorf("marker = %q", got)
	}
}

func TestCloseExpiredSeason_ExtendedSeasonGetsFreshNudges(t *testing.T) {
	const chat = int64(-9103)
	b, se := nudgeFixture(t, chat, 2) // 2 games: below the games floor
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	if err := b.st.SetSeasonNudge(se.ID, "final@2026-08-01 00:00:00"); err != nil {
		t.Fatalf("SetSeasonNudge: %v", err)
	}
	// Deadline passed with 2 of 66 games: extended to 1 Sep.
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 8, 1, 0, 5, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	got, _ := b.st.ActiveSeason(chat)
	if got.Deadline.String != "2026-09-01 00:00:00" {
		t.Fatalf("deadline = %q, want 2026-09-01 00:00:00", got.Deadline.String)
	}
	// 26 Aug: the new deadline is six days out and its own week nudge is due.
	if err := b.closeExpiredSeasonAt(ch, time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	if m, _ := b.st.SeasonNudge(se.ID); m != "week@2026-09-01 00:00:00" {
		t.Errorf("marker = %q, want week@2026-09-01 00:00:00", m)
	}
}
