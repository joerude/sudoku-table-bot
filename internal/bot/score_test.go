package bot

import (
	"errors"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// scoreFixture is a chat with two players and a pending game with both picked.
func scoreFixture(t *testing.T, chat int64) (*Bot, *storage.Store, *storage.Season, *storage.Game) {
	t.Helper()
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	st.AddPick(gid, a.ID)
	st.AddPick(gid, b.ID)
	g, err := st.GameByID(gid)
	if err != nil || g == nil {
		t.Fatalf("GameByID: %v", err)
	}
	return &Bot{st: st}, st, se, g
}

// TestScoreAndCheckTwiceReportsNotPending: the second scoring of the same game
// (double tap on «Готово», or manual finalize racing auto-record) must surface
// storage.ErrGameNotPending so callers can exit without a second result post.
func TestScoreAndCheckTwiceReportsNotPending(t *testing.T) {
	b, st, _, g := scoreFixture(t, -8001)
	if _, _, err := b.scoreAndCheck(g); err != nil {
		t.Fatalf("first scoreAndCheck: %v", err)
	}
	result, seasonEnd, err := b.scoreAndCheck(g)
	if !errors.Is(err, storage.ErrGameNotPending) {
		t.Fatalf("second scoreAndCheck: want ErrGameNotPending, got %v", err)
	}
	if result != "" || seasonEnd != "" {
		t.Errorf("second scoreAndCheck must not render anything, got %q / %q", result, seasonEnd)
	}
	rows, _ := st.GameResults(g.ID)
	if len(rows) != 2 || rows[0].Points != 3 || rows[1].Points != 1 {
		t.Errorf("points changed by the second call: %+v", rows)
	}
}

// TestScoreAndCheckAfterReopen: edit → reopen → record again still scores.
func TestScoreAndCheckAfterReopen(t *testing.T) {
	b, st, _, g := scoreFixture(t, -8002)
	if _, _, err := b.scoreAndCheck(g); err != nil {
		t.Fatalf("first scoreAndCheck: %v", err)
	}
	if err := st.ReopenGame(g.ID); err != nil {
		t.Fatalf("ReopenGame: %v", err)
	}
	players, _ := st.ListPlayers(-8002)
	// Re-record in the opposite order.
	for i := len(players) - 1; i >= 0; i-- {
		if err := st.AddPick(g.ID, players[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := b.scoreAndCheck(g); err != nil {
		t.Fatalf("scoreAndCheck after reopen: %v", err)
	}
	rows, _ := st.GameResults(g.ID)
	if len(rows) != 2 || rows[0].Name != "Bob" || rows[0].Points != 3 {
		t.Errorf("re-recorded result wrong: %+v", rows)
	}
}
