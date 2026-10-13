package storage

import "testing"

// A declined duel is soft-deleted but stays status='pending'; it must not
// trigger the "unrecorded game" nudge.
func TestStalePendingGamesSkipsDeleted(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	live, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame live: %v", err)
	}
	gone, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame gone: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE games SET created_at = datetime('now', '-2 hours')`); err != nil {
		t.Fatalf("age games: %v", err)
	}
	if err := st.SoftDeleteGame(gone); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}

	got, err := st.StalePendingGames(45)
	if err != nil {
		t.Fatalf("StalePendingGames: %v", err)
	}
	if len(got) != 1 || got[0].GameID != live {
		t.Fatalf("got %+v, want only the live game %d", got, live)
	}
}
