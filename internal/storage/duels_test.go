package storage

import "testing"

// makeDuelGame creates a completed duel game between winner (rank 1) and loser (rank 2).
func makeDuelGame(t *testing.T, st *Store, chatID, seasonID, creatorID, winnerID, loserID, targetID int64) int64 {
	t.Helper()
	gid, err := st.CreatePendingGame(chatID, seasonID, creatorID, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.SetDuelTarget(gid, targetID); err != nil {
		t.Fatalf("SetDuelTarget: %v", err)
	}
	if err := st.AddPick(gid, winnerID); err != nil {
		t.Fatalf("AddPick winner: %v", err)
	}
	if err := st.AddPick(gid, loserID); err != nil {
		t.Fatalf("AddPick loser: %v", err)
	}
	se, err := st.ActiveSeason(chatID)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame: %v", err)
	}
	return gid
}

// makeNormalGame creates a completed non-duel game with winner then loser.
func makeNormalGame(t *testing.T, st *Store, chatID, seasonID, winnerID, loserID int64) int64 {
	t.Helper()
	gid, err := st.CreatePendingGame(chatID, seasonID, winnerID, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.AddPick(gid, winnerID); err != nil {
		t.Fatalf("AddPick winner: %v", err)
	}
	if err := st.AddPick(gid, loserID); err != nil {
		t.Fatalf("AddPick loser: %v", err)
	}
	se, err := st.ActiveSeason(chatID)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame: %v", err)
	}
	return gid
}

// TestSeasonExcludesDuels: Standings and SpeedFor/Speedboard count normal games only.
func TestSeasonExcludesDuels(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-2001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Normal game: A beats B → A gets 3 pts, B gets 1 pt.
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)

	// Duel game: A beats B again — should NOT add to season standings.
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	standings, err := st.Standings(chat, se.ID)
	if err != nil {
		t.Fatalf("Standings: %v", err)
	}
	pts := map[string]int{}
	for _, s := range standings {
		pts[s.Name] = s.Points
	}
	if pts["Alice"] != 3 {
		t.Errorf("Alice: want 3 pts (normal game only), got %d", pts["Alice"])
	}
	if pts["Bob"] != 1 {
		t.Errorf("Bob: want 1 pt (normal game only), got %d", pts["Bob"])
	}

	// SpeedFor/Speedboard: seed one normal timed game and one duel timed game.
	gNorm, err := st.CreatePendingGame(chat, se.ID, a.ID, "hard", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame normal timed: %v", err)
	}
	if err := st.AddPickTimed(gNorm, a.ID, 100); err != nil {
		t.Fatalf("AddPickTimed normal: %v", err)
	}
	if err := st.FinalizeGame(gNorm, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame normal timed: %v", err)
	}

	gDuel, err := st.CreatePendingGame(chat, se.ID, a.ID, "hard", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame duel timed: %v", err)
	}
	if err := st.SetDuelTarget(gDuel, b.ID); err != nil {
		t.Fatalf("SetDuelTarget: %v", err)
	}
	if err := st.AddPickTimed(gDuel, a.ID, 50); err != nil {
		t.Fatalf("AddPickTimed duel: %v", err)
	}
	if err := st.FinalizeGame(gDuel, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame duel timed: %v", err)
	}

	sp, err := st.SpeedFor(chat, se.ID, a.ID, "hard")
	if err != nil {
		t.Fatalf("SpeedFor: %v", err)
	}
	if sp.Games != 1 {
		t.Errorf("SpeedFor: want 1 game (duel excluded), got %d", sp.Games)
	}
	if sp.BestSecs != 100 {
		t.Errorf("SpeedFor: want best=100 (duel excluded), got %d", sp.BestSecs)
	}

	ranked, fewer, err := st.Speedboard(chat, se.ID, "hard", 1)
	if err != nil {
		t.Fatalf("Speedboard: %v", err)
	}
	total := len(ranked) + len(fewer)
	found := false
	allRows := append(ranked, fewer...)
	for _, r := range allRows {
		if r.Name == "Alice" {
			if r.Games != 1 {
				t.Errorf("Speedboard Alice: want 1 game (duel excluded), got %d", r.Games)
			}
			if r.AvgSecs != 100 {
				t.Errorf("Speedboard Alice: want avg=100 (duel excluded), got %d", r.AvgSecs)
			}
			found = true
		}
	}
	if total == 0 || !found {
		t.Errorf("Speedboard: expected Alice in results, got ranked=%v fewer=%v", ranked, fewer)
	}
}

// TestDuelRecord: after mixed duel results, record reflects only duel games.
func TestDuelRecord(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-2002)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Duel 1: A beats B.
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	// Duel 2: B beats A.
	makeDuelGame(t, st, chat, se.ID, b.ID, b.ID, a.ID, a.ID)
	// Normal game: A beats B — must NOT affect DuelRecord.
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)

	aWins, aLosses, err := st.DuelRecord(chat, a.ID)
	if err != nil {
		t.Fatalf("DuelRecord A: %v", err)
	}
	if aWins != 1 || aLosses != 1 {
		t.Errorf("DuelRecord A: want (1,1), got (%d,%d)", aWins, aLosses)
	}

	bWins, bLosses, err := st.DuelRecord(chat, b.ID)
	if err != nil {
		t.Fatalf("DuelRecord B: %v", err)
	}
	if bWins != 1 || bLosses != 1 {
		t.Errorf("DuelRecord B: want (1,1), got (%d,%d)", bWins, bLosses)
	}
}

// TestDuelLeaderboard: ordered by wins; players with 0 duels excluded.
func TestDuelLeaderboard(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-2003)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol") // never plays a duel

	// A beats B twice, B beats A once → A 2 wins, B 1 win.
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, b.ID, b.ID, a.ID, a.ID)
	// Carol plays only a normal game — should be absent from DuelLeaderboard.
	makeNormalGame(t, st, chat, se.ID, c.ID, a.ID)

	lb, err := st.DuelLeaderboard(chat)
	if err != nil {
		t.Fatalf("DuelLeaderboard: %v", err)
	}
	if len(lb) != 2 {
		t.Fatalf("DuelLeaderboard: want 2 entries, got %d: %+v", len(lb), lb)
	}
	// A should be first (2 wins > 1 win).
	if lb[0].Name != "Alice" || lb[0].Wins != 2 || lb[0].Losses != 1 {
		t.Errorf("lb[0]: want Alice 2W/1L, got %+v", lb[0])
	}
	if lb[1].Name != "Bob" || lb[1].Wins != 1 || lb[1].Losses != 2 {
		t.Errorf("lb[1]: want Bob 1W/2L, got %+v", lb[1])
	}
}

// TestHeadToHead: wins against each other only; third-player duels don't count.
func TestHeadToHead(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-2004)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol")

	// A beats B: 2 times.
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	// B beats A: 1 time.
	makeDuelGame(t, st, chat, se.ID, b.ID, b.ID, a.ID, a.ID)
	// A beats C: should NOT count toward A-B head-to-head.
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, c.ID, c.ID)

	aWins, bWins, err := st.HeadToHead(chat, a.ID, b.ID)
	if err != nil {
		t.Fatalf("HeadToHead: %v", err)
	}
	if aWins != 2 || bWins != 1 {
		t.Errorf("HeadToHead(A,B): want (2,1), got (%d,%d)", aWins, bWins)
	}
}
