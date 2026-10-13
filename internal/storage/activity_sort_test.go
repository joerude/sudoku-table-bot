package storage

import "testing"

// TestInitiativeStatsSortsByRecentWindow: a player who started many games long
// ago ranks below one who is active in the last 30 days.
func TestInitiativeStatsSortsByRecentWindow(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")

	seed := func(creatorTg int64, completedAt string) {
		t.Helper()
		gid, err := st.CreatePendingGame(chat, se.ID, creatorTg, "medium", "hardcore")
		if err != nil {
			t.Fatal(err)
		}
		_ = st.AddPick(gid, a.ID)
		_ = st.AddPick(gid, b.ID)
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.Exec(`UPDATE games SET completed_at=? WHERE id=?`, completedAt, gid); err != nil {
			t.Fatal(err)
		}
	}
	// Alice: three old games. Bob: one recent game.
	seed(11, "2026-01-05 10:00:00")
	seed(11, "2026-01-06 10:00:00")
	seed(11, "2026-01-07 10:00:00")
	seed(12, "2026-07-15 10:00:00")

	rows, err := st.InitiativeStats(chat, "2026-06-20 00:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "Bob" || rows[1].Name != "Alice" {
		t.Fatalf("order = %+v, want Bob (1 recent) before Alice (3 old)", rows)
	}
	if rows[0].Games30 != 1 || rows[1].GamesAll != 3 || rows[1].Games30 != 0 {
		t.Errorf("counters wrong: %+v", rows)
	}
}
