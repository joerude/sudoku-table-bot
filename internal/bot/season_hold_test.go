package bot

import (
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// TestCloseExpiredSeason_WaitsForOpenGame: with the games floor met, a game
// started before the deadline holds the close (the deadline is not moved)
// until it is recorded or cancelled; then the season closes.
func TestCloseExpiredSeason_WaitsForOpenGame(t *testing.T) {
	const chat = int64(-9201)
	b, se := nudgeFixture(t, chat, 4)
	if err := b.st.UpdateSeasonMinGames(se.ID, 4); err != nil { // 4 games: floor met
		t.Fatal(err)
	}
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}

	// Deadline one minute ahead of the pending game's creation clock.
	deadline := time.Now().UTC().Add(time.Minute)
	if err := b.st.SetSeasonDeadline(se.ID, fmtDBTime(deadline)); err != nil {
		t.Fatal(err)
	}
	gid, err := b.st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}

	after := deadline.Add(time.Minute)
	if err := b.closeExpiredSeasonAt(ch, after); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	held, _ := b.st.ActiveSeason(chat)
	if held.ID != se.ID {
		t.Fatalf("season closed while a game was still open (active id %d, want %d)", held.ID, se.ID)
	}
	if held.Deadline.String != fmtDBTime(deadline) {
		t.Errorf("deadline = %q, want it held at %q, not extended", held.Deadline.String, fmtDBTime(deadline))
	}

	if err := b.st.SoftDeleteGame(gid); err != nil {
		t.Fatal(err)
	}
	if err := b.closeExpiredSeasonAt(ch, after); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	got, _ := b.st.ActiveSeason(chat)
	if got.ID == se.ID {
		t.Fatal("season still active after the open game was cancelled")
	}
	archived, _ := b.st.ArchivedNumbers(chat)
	if len(archived) != 1 || archived[0] != se.Number {
		t.Errorf("archived = %v, want [%d]", archived, se.Number)
	}
}

// TestCloseExpiredSeason_UnderFloorExtendsDespiteOpenGame: short of the games
// floor the season is extended right away; an open game has nothing to hold,
// it simply lands in the same (still active) season.
func TestCloseExpiredSeason_UnderFloorExtendsDespiteOpenGame(t *testing.T) {
	const chat = int64(-9202)
	b, se := nudgeFixture(t, chat, 4) // 4 of the default 66 games
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}

	deadline := time.Now().UTC().Add(time.Minute)
	if err := b.st.SetSeasonDeadline(se.ID, fmtDBTime(deadline)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore"); err != nil {
		t.Fatal(err)
	}
	if err := b.closeExpiredSeasonAt(ch, deadline.Add(time.Minute)); err != nil {
		t.Fatalf("closeExpiredSeasonAt: %v", err)
	}
	got, _ := b.st.ActiveSeason(chat)
	if got.ID != se.ID {
		t.Fatalf("under-played season closed (active id %d, want %d)", got.ID, se.ID)
	}
	if got.Deadline.String == fmtDBTime(deadline) {
		t.Errorf("deadline not extended: %q", got.Deadline.String)
	}
}
