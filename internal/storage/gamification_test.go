package storage

import "testing"

func TestDuelPairs(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	g1 := makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	g2 := makeDuelGame(t, st, chat, se.ID, b.ID, b.ID, a.ID, a.ID)
	// A normal game must not appear in duel pairs.
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)

	pairs, err := st.DuelPairs(chat)
	if err != nil {
		t.Fatalf("DuelPairs: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("want 2 pairs, got %d: %+v", len(pairs), pairs)
	}
	if pairs[0].GameID != g1 || pairs[0].WinnerID != a.ID || pairs[0].LoserID != b.ID {
		t.Errorf("pair 0 wrong: %+v", pairs[0])
	}
	if pairs[1].GameID != g2 || pairs[1].WinnerID != b.ID || pairs[1].LoserID != a.ID {
		t.Errorf("pair 1 wrong: %+v", pairs[1])
	}
}

func TestDuelPairsSkipsSoloResult(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7002)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// A duel where only the winner was recorded has no loser to rate.
	gid, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	if err := st.SetDuelTarget(gid, b.ID); err != nil {
		t.Fatal(err)
	}
	st.AddPick(gid, a.ID)
	st.FinalizeGame(gid, se.PointsTable)

	pairs, err := st.DuelPairs(chat)
	if err != nil {
		t.Fatalf("DuelPairs: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("solo-result duel must be skipped, got %+v", pairs)
	}
}

func TestBestSecsBefore(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7003)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Game 1: medium, 200s. Game 2: hard, 150s. Game 3: medium (the "current" one).
	g1, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	st.AddPickTimed(g1, a.ID, 200)
	st.AddPick(g1, b.ID)
	st.FinalizeGame(g1, se.PointsTable)

	g2, _ := st.CreatePendingGame(chat, se.ID, a.ID, "hard", "hardcore")
	st.AddPickTimed(g2, a.ID, 150)
	st.AddPick(g2, b.ID)
	st.FinalizeGame(g2, se.PointsTable)

	g3, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	st.AddPickTimed(g3, a.ID, 180)
	st.AddPick(g3, b.ID)
	st.FinalizeGame(g3, se.PointsTable)

	// Difficulty-scoped: only the medium game before g3 counts.
	got, err := st.BestSecsBefore(chat, a.ID, "medium", g3)
	if err != nil {
		t.Fatalf("BestSecsBefore: %v", err)
	}
	if got != 200 {
		t.Errorf("medium before g3: want 200, got %d", got)
	}
	// Any-difficulty: the hard 150s is the career best before g3.
	if got, _ := st.BestSecsBefore(chat, a.ID, "", g3); got != 150 {
		t.Errorf("any before g3: want 150, got %d", got)
	}
	// No timed games before g1 → 0.
	if got, _ := st.BestSecsBefore(chat, a.ID, "medium", g1); got != 0 {
		t.Errorf("before g1: want 0, got %d", got)
	}
	// Bob has no timed games at all → 0.
	if got, _ := st.BestSecsBefore(chat, b.ID, "", g3); got != 0 {
		t.Errorf("bob: want 0, got %d", got)
	}
}

func TestFastestInSeason(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7004)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	if r, err := st.FastestInSeason(chat, se.ID); err != nil || r != nil {
		t.Fatalf("empty season: want nil, got %+v err %v", r, err)
	}

	g1, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	st.AddPickTimed(g1, a.ID, 200)
	st.AddPickTimed(g1, b.ID, 170)
	st.FinalizeGame(g1, se.PointsTable)

	r, err := st.FastestInSeason(chat, se.ID)
	if err != nil {
		t.Fatalf("FastestInSeason: %v", err)
	}
	if r == nil || r.Name != "Bob" || r.Secs != 170 || r.Difficulty != "medium" {
		t.Errorf("want Bob 170 medium, got %+v", r)
	}
}

func TestSeasonRanks(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7005)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID) // A wins
	makeNormalGame(t, st, chat, se.ID, b.ID, a.ID) // B wins
	// Duels must not leak into season ranks.
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	rows, err := st.SeasonRanks(chat, se.ID)
	if err != nil {
		t.Fatalf("SeasonRanks: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("want 4 rows (2 games × 2 players), got %d: %+v", len(rows), rows)
	}
	// Game order, rank order inside each game.
	if rows[0].Name != "Alice" || rows[0].Rank != 1 || rows[1].Name != "Bob" || rows[1].Rank != 2 {
		t.Errorf("game 1 rows wrong: %+v", rows[:2])
	}
	if rows[2].Name != "Bob" || rows[2].Rank != 1 || rows[3].Name != "Alice" || rows[3].Rank != 2 {
		t.Errorf("game 2 rows wrong: %+v", rows[2:])
	}
}

func TestSeasonByNumberAndMeta(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7007)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)

	// Close the season -> archived #1 with Alice as winner, new active #2.
	if _, err := st.CloseSeason(se, a.ID); err != nil {
		t.Fatal(err)
	}

	s1, err := st.SeasonByNumber(chat, 1)
	if err != nil || s1 == nil || s1.Status != "archived" {
		t.Fatalf("SeasonByNumber(1): %+v err %v", s1, err)
	}
	if s9, _ := st.SeasonByNumber(chat, 9); s9 != nil {
		t.Errorf("SeasonByNumber(9): want nil, got %+v", s9)
	}

	games, first, last, winner, err := st.SeasonMeta(chat, s1.ID)
	if err != nil {
		t.Fatalf("SeasonMeta: %v", err)
	}
	if games != 1 || first == "" || first != last || winner != "Alice" {
		t.Errorf("SeasonMeta: got games=%d first=%q last=%q winner=%q", games, first, last, winner)
	}

	nums, err := st.ArchivedNumbers(chat)
	if err != nil || len(nums) != 1 || nums[0] != 1 {
		t.Errorf("ArchivedNumbers: want [1], got %v err %v", nums, err)
	}
}

func TestDuelLeaderboardCarriesPlayerID(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7006)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	lb, err := st.DuelLeaderboard(chat)
	if err != nil {
		t.Fatalf("DuelLeaderboard: %v", err)
	}
	ids := map[string]int64{}
	for _, d := range lb {
		ids[d.Name] = d.PlayerID
	}
	if ids["Alice"] != a.ID || ids["Bob"] != b.ID {
		t.Errorf("player ids missing: %+v", lb)
	}
}
