package bot

import (
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// TestCloseExpiredSeason_WaitsForOpenGame: a game started before the deadline
// holds the close until it is recorded or cancelled; then the season closes.
func TestCloseExpiredSeason_WaitsForOpenGame(t *testing.T) {
	const chat = int64(-9201)
	b, se := nudgeFixture(t, chat, 4) // 12 points: above the close threshold
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
	if got, _ := b.st.ActiveSeason(chat); got.ID != se.ID {
		t.Fatalf("season closed while a game was still open (active id %d, want %d)", got.ID, se.ID)
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
