package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestLocalDayBounds(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	now := time.Date(2026, 7, 21, 1, 30, 0, 0, loc) // 20 July 19:30 UTC
	from, to := localDayBounds(now, loc)
	if want := time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC); !from.Equal(want) {
		t.Errorf("from = %s, want %s", from, want)
	}
	if want := time.Date(2026, 7, 21, 18, 0, 0, 0, time.UTC); !to.Equal(want) {
		t.Errorf("to = %s, want %s", to, want)
	}
}

func TestDailyNudge(t *testing.T) {
	st := openTempStore(t)
	b := &Bot{st: st}
	loc := time.FixedZone("UTC+6", 6*3600)
	now := time.Now().In(loc)

	// Chat with no active players: stay quiet.
	const empty = int64(-9301)
	if err := st.EnsureChat(empty, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := b.dailyNudge(empty, now, loc); err != nil || got != "" {
		t.Errorf("no players: got %q, %v; want quiet", got, err)
	}

	const chat = int64(-9302)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	bb, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Players, nothing played: nudge to /play, not to /result.
	got, err := b.dailyNudge(chat, now, loc)
	if err != nil || got != dailyNudgeText {
		t.Fatalf("idle chat: got %q, %v; want the nudge", got, err)
	}
	if !strings.Contains(got, "/play") || strings.Contains(got, "/result") {
		t.Errorf("nudge text must point at /play: %q", got)
	}

	// A pending game is waiting for its result: do not nag.
	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := b.dailyNudge(chat, now, loc); got != "" {
		t.Errorf("pending game: got %q, want quiet", got)
	}
	_ = st.SoftDeleteGame(gid)

	// A game recorded today: quiet (the local-day boundary itself is covered
	// by the storage test).
	done, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	_ = st.AddPick(done, a.ID)
	_ = st.AddPick(done, bb.ID)
	if err := st.FinalizeGame(done, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.dailyNudge(chat, now, loc); got != "" {
		t.Errorf("game played today: got %q, want quiet", got)
	}
	// Next local day: nothing played yet, nudge again.
	if got, _ := b.dailyNudge(chat, now.Add(24*time.Hour), loc); got != dailyNudgeText {
		t.Errorf("next day: got %q, want the nudge", got)
	}
}

func TestDigestFinalLine(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{Number: 5, Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true}}
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, loc)
	rows := []storage.Standing{{Name: "Alice", Points: 30}, {Name: "Bob", Points: 27}}

	got := digestFinalLine(se, rows, now, loc)
	for _, want := range []string{"До финала 18 дней", "<b>Alice</b> +3", "второй: Bob"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	tie := digestFinalLine(se, []storage.Standing{{Name: "A", Points: 5}, {Name: "B", Points: 5}}, now, loc)
	if !strings.Contains(tie, "поровну") {
		t.Errorf("tie: %q", tie)
	}
	today := digestFinalLine(se, rows, time.Date(2026, 7, 31, 10, 0, 0, 0, loc), loc)
	if !strings.Contains(today, "Финал сезона сегодня") {
		t.Errorf("final day: %q", today)
	}
	if got := digestFinalLine(se, rows, time.Date(2026, 8, 2, 10, 0, 0, 0, loc), loc); got != "" {
		t.Errorf("after the deadline: %q, want empty", got)
	}
	if got := digestFinalLine(&storage.Season{}, rows, now, loc); got != "" {
		t.Errorf("no deadline: %q, want empty", got)
	}
	zero := digestFinalLine(se, []storage.Standing{{Name: "A"}, {Name: "B"}}, now, loc)
	if strings.Contains(zero, "·") {
		t.Errorf("zero points: no lead info expected: %q", zero)
	}
}
