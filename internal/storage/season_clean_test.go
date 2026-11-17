package storage

import "testing"

// TestSeasonCleanGames: a board counts when nobody who joined quit; a game with
// a rank-0 row, a duel and a deleted game do not, and until cuts later games.
func TestSeasonCleanGames(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4301)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	clean := func() (int, error) { return st.SeasonCleanGames(chat, se.ID, "") }

	first := makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	if n, err := clean(); err != nil || n != 1 {
		t.Fatalf("a clean game: got %d, %v; want 1", n, err)
	}

	// Bob joined and quit: the game was played, but it is not a clean board.
	dnf, err := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPick(dnf, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDNF(dnf, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.FinalizeGame(dnf, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	gone := makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	if err := st.SoftDeleteGame(gone); err != nil {
		t.Fatal(err)
	}
	if n, _ := clean(); n != 1 {
		t.Errorf("DNF game, duel and deleted game must not count, got %d", n)
	}

	// until cuts later games off.
	second := makeNormalGame(t, st, chat, se.ID, b.ID, a.ID)
	for id, at := range map[int64]string{first: "2026-07-01 10:00:00", second: "2026-07-02 10:00:00"} {
		if _, err := st.db.Exec(`UPDATE games SET completed_at=? WHERE id=?`, at, id); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := st.SeasonCleanGames(chat, se.ID, "2026-07-01 10:00:00"); n != 1 {
		t.Errorf("until the first game: got %d, want 1", n)
	}
	if n, _ := st.SeasonCleanGames(chat, se.ID, "2026-07-02 10:00:00"); n != 2 {
		t.Errorf("until the second game: got %d, want 2", n)
	}
}
