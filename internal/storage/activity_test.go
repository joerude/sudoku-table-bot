package storage

import "testing"

// TestInitiativeStats: games are attributed to their creator (by tg id), split
// into season games and duels, with a 30-day window and the last time each
// player actually played.
func TestInitiativeStats(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")

	// NOTE: the shared makeNormalGame/makeDuelGame helpers pass a *player* id as
	// created_by, but InitiativeStats matches created_by against players.tg_id.
	// Seed locally with real tg ids instead.
	seed := func(creatorTg int64, duel bool) {
		t.Helper()
		gid, err := st.CreatePendingGame(chat, se.ID, creatorTg, "medium", "hardcore")
		if err != nil {
			t.Fatalf("CreatePendingGame: %v", err)
		}
		if duel {
			if err := st.SetDuelTarget(gid, b.ID); err != nil {
				t.Fatalf("SetDuelTarget: %v", err)
			}
		}
		if err := st.AddPick(gid, a.ID); err != nil {
			t.Fatalf("AddPick a: %v", err)
		}
		if err := st.AddPick(gid, b.ID); err != nil {
			t.Fatalf("AddPick b: %v", err)
		}
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatalf("FinalizeGame: %v", err)
		}
	}
	// Alice starts two season games and one duel; Bob starts nothing.
	seed(11, false)
	seed(11, false)
	seed(11, true)

	rows, err := st.InitiativeStats(chat, "1970-01-01 00:00:00")
	if err != nil {
		t.Fatalf("InitiativeStats: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (%+v)", len(rows), rows)
	}
	if rows[0].Name != "Alice" {
		t.Errorf("rows[0] = %s, want Alice first (most initiated)", rows[0].Name)
	}
	if rows[0].GamesAll != 2 || rows[0].DuelsAll != 1 {
		t.Errorf("Alice: games=%d duels=%d, want 2/1", rows[0].GamesAll, rows[0].DuelsAll)
	}
	if rows[0].Games30 != 2 || rows[0].Duels30 != 1 {
		t.Errorf("Alice 30d: games=%d duels=%d, want 2/1", rows[0].Games30, rows[0].Duels30)
	}
	if rows[1].GamesAll != 0 || rows[1].DuelsAll != 0 {
		t.Errorf("Bob: games=%d duels=%d, want 0/0", rows[1].GamesAll, rows[1].DuelsAll)
	}
	// Bob never started a game but did play in them.
	if rows[1].LastPlayed == "" {
		t.Errorf("Bob: LastPlayed empty, want the last game he played in")
	}

	// A window that excludes everything zeroes the 30-day counters but keeps
	// the all-time ones.
	rows, err = st.InitiativeStats(chat, "2099-01-01 00:00:00")
	if err != nil {
		t.Fatalf("InitiativeStats future window: %v", err)
	}
	if rows[0].GamesAll != 2 || rows[0].Games30 != 0 {
		t.Errorf("Alice future window: all=%d 30d=%d, want 2/0", rows[0].GamesAll, rows[0].Games30)
	}
}

// TestInitiativeStatsIgnoresUnknownCreator: games created by a tg id that maps
// to no player (legacy rows) are not attributed to anyone.
func TestInitiativeStatsIgnoresUnknownCreator(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")

	gid, err := st.CreatePendingGame(chat, se.ID, 999, "medium", "hardcore") // stranger
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.AddPick(gid, a.ID); err != nil {
		t.Fatalf("AddPick: %v", err)
	}
	if err := st.AddPick(gid, b.ID); err != nil {
		t.Fatalf("AddPick: %v", err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame: %v", err)
	}

	rows, err := st.InitiativeStats(chat, "1970-01-01 00:00:00")
	if err != nil {
		t.Fatalf("InitiativeStats: %v", err)
	}
	for _, r := range rows {
		if r.GamesAll != 0 || r.DuelsAll != 0 {
			t.Errorf("%s credited with a stranger's game: %+v", r.Name, r)
		}
	}
}
