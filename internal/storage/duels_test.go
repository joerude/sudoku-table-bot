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

// makeSoloDuelGame creates a completed duel game with only the winner finishing (no loser row).
func makeSoloDuelGame(t *testing.T, st *Store, chatID, seasonID, creatorID, winnerID, targetID int64) int64 {
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
	se, err := st.ActiveSeason(chatID)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame: %v", err)
	}
	return gid
}

// TestRecentDuels: newest-first, correct Winner/Loser; solo duel has empty Loser.
func TestRecentDuels(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// First game: A beats B (both finish).
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	// Second game: A wins solo (B didn't finish).
	makeSoloDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID)

	matches, err := st.RecentDuels(chat, 10)
	if err != nil {
		t.Fatalf("RecentDuels: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("RecentDuels: want 2 matches, got %d: %+v", len(matches), matches)
	}
	// Newest first: solo game was created second → index 0.
	if matches[0].Winner != "Alice" {
		t.Errorf("matches[0].Winner: want Alice, got %q", matches[0].Winner)
	}
	if matches[0].Loser != "" {
		t.Errorf("matches[0].Loser: want empty (solo), got %q", matches[0].Loser)
	}
	// Older game: A beat B.
	if matches[1].Winner != "Alice" {
		t.Errorf("matches[1].Winner: want Alice, got %q", matches[1].Winner)
	}
	if matches[1].Loser != "Bob" {
		t.Errorf("matches[1].Loser: want Bob, got %q", matches[1].Loser)
	}
}

// makeDNFDuelGame creates a completed duel where the opponent DNF'd (rank 0).
func makeDNFDuelGame(t *testing.T, st *Store, chatID, seasonID, creatorID, winnerID, dnfID, targetID int64) int64 {
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
	if err := st.AddDNF(gid, dnfID); err != nil {
		t.Fatalf("AddDNF: %v", err)
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

// TestDuelDNFCountsAsLoss: an opponent stored via AddDNF (rank 0) must count as
// a loss in DuelRecord, DuelLeaderboard, and RecentDuels.
func TestDuelDNFCountsAsLoss(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Alice wins; Bob DNF (rank 0).
	makeDNFDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	// DuelRecord: Bob must show losses=1 (not 0).
	_, bLosses, err := st.DuelRecord(chat, b.ID)
	if err != nil {
		t.Fatalf("DuelRecord Bob: %v", err)
	}
	if bLosses != 1 {
		t.Errorf("DuelRecord Bob DNF: want losses=1, got %d", bLosses)
	}

	// Alice must show wins=1, losses=0.
	aWins, aLosses, err := st.DuelRecord(chat, a.ID)
	if err != nil {
		t.Fatalf("DuelRecord Alice: %v", err)
	}
	if aWins != 1 || aLosses != 0 {
		t.Errorf("DuelRecord Alice: want (1,0), got (%d,%d)", aWins, aLosses)
	}

	// DuelLeaderboard: Bob shows 0-1.
	lb, err := st.DuelLeaderboard(chat)
	if err != nil {
		t.Fatalf("DuelLeaderboard: %v", err)
	}
	lbIdx := map[string]DuelStanding{}
	for _, d := range lb {
		lbIdx[d.Name] = d
	}
	if lbIdx["Bob"].Wins != 0 || lbIdx["Bob"].Losses != 1 {
		t.Errorf("DuelLeaderboard Bob: want 0W/1L, got %+v", lbIdx["Bob"])
	}

	// RecentDuels: the DNF opponent must appear as Loser.
	matches, err := st.RecentDuels(chat, 5)
	if err != nil {
		t.Fatalf("RecentDuels: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("RecentDuels: want 1 match, got %d", len(matches))
	}
	if matches[0].Winner != "Alice" {
		t.Errorf("RecentDuels: winner want Alice, got %q", matches[0].Winner)
	}
	if matches[0].Loser != "Bob" {
		t.Errorf("RecentDuels: loser want Bob (DNF), got %q", matches[0].Loser)
	}
}

// TestDuelNormalLossStillWorks: a normal rank-2 loss still reports correctly.
func TestDuelNormalLossStillWorks(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5002)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Alice wins (rank 1), Bob loses (rank 2).
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	aWins, aLosses, _ := st.DuelRecord(chat, a.ID)
	bWins, bLosses, _ := st.DuelRecord(chat, b.ID)
	if aWins != 1 || aLosses != 0 {
		t.Errorf("Alice normal win: want (1,0), got (%d,%d)", aWins, aLosses)
	}
	if bWins != 0 || bLosses != 1 {
		t.Errorf("Bob normal loss: want (0,1), got (%d,%d)", bWins, bLosses)
	}

	matches, err := st.RecentDuels(chat, 5)
	if err != nil {
		t.Fatalf("RecentDuels: %v", err)
	}
	if len(matches) != 1 || matches[0].Loser != "Bob" {
		t.Errorf("RecentDuels normal: want Loser=Bob, got %+v", matches)
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

// TestHeadToHeadAll: every pair reported once (lower id = A); solo duels and
// third-player duels are handled correctly.
func TestHeadToHeadAll(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-2104)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol")

	// A beats B x2, B beats A x1  → pair (A,B): 2–1
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, b.ID, b.ID, a.ID, a.ID)
	// A beats C x1 → pair (A,C): 1–0
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, c.ID, c.ID)
	// Solo duel (only winner recorded) must NOT create a phantom pair.
	makeSoloDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID)

	pairs, err := st.HeadToHeadAll(chat)
	if err != nil {
		t.Fatalf("HeadToHeadAll: %v", err)
	}
	idx := map[[2]int64]H2HPair{}
	for _, p := range pairs {
		idx[[2]int64{p.AID, p.BID}] = p
	}
	ab, ok := idx[[2]int64{a.ID, b.ID}]
	if !ok {
		t.Fatalf("A-B pair missing: %+v", pairs)
	}
	if ab.AName != "Alice" || ab.BName != "Bob" || ab.AWins != 2 || ab.BWins != 1 {
		t.Errorf("A-B: want Alice 2 / Bob 1, got %+v", ab)
	}
	ac, ok := idx[[2]int64{a.ID, c.ID}]
	if !ok {
		t.Fatalf("A-C pair missing: %+v", pairs)
	}
	if ac.AWins != 1 || ac.BWins != 0 {
		t.Errorf("A-C: want 1–0, got %+v", ac)
	}
	if len(pairs) != 2 {
		t.Errorf("want exactly 2 pairs (A-B, A-C), got %d: %+v", len(pairs), pairs)
	}
}

// makeTimedDuelGame: winner finishes in winnerSecs, loser DNF (NULL duration).
func makeTimedDuelGame(t *testing.T, st *Store, chatID, seasonID, winnerID, winnerSecs, loserID int64) int64 {
	t.Helper()
	gid, err := st.CreatePendingGame(chatID, seasonID, winnerID, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.SetDuelTarget(gid, loserID); err != nil {
		t.Fatalf("SetDuelTarget: %v", err)
	}
	if err := st.AddPickTimed(gid, winnerID, winnerSecs); err != nil {
		t.Fatalf("AddPickTimed: %v", err)
	}
	if err := st.AddDNF(gid, loserID); err != nil {
		t.Fatalf("AddDNF: %v", err)
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

// TestDuelSpeedAndSpeedFor: duel solve times aggregate per player; normal games
// and NULL durations are excluded; fastest-first ordering.
func TestDuelSpeedAndSpeedFor(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-2105)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Alice wins two timed duels (50, 100); Bob wins one (200).
	makeTimedDuelGame(t, st, chat, se.ID, a.ID, 50, b.ID)
	makeTimedDuelGame(t, st, chat, se.ID, a.ID, 100, b.ID)
	makeTimedDuelGame(t, st, chat, se.ID, b.ID, 200, a.ID)
	// A normal timed game must NOT count toward duel speed.
	gNorm, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	st.AddPickTimed(gNorm, a.ID, 5)
	st.FinalizeGame(gNorm, se.PointsTable)

	board, err := st.DuelSpeed(chat)
	if err != nil {
		t.Fatalf("DuelSpeed: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("DuelSpeed: want 2 rows, got %d: %+v", len(board), board)
	}
	if board[0].Name != "Alice" || board[0].AvgSecs != 75 || board[0].BestSecs != 50 || board[0].Games != 2 {
		t.Errorf("board[0]: want Alice avg75 best50 games2, got %+v", board[0])
	}
	if board[1].Name != "Bob" || board[1].AvgSecs != 200 || board[1].Games != 1 {
		t.Errorf("board[1]: want Bob avg200 games1, got %+v", board[1])
	}

	sp, err := st.DuelSpeedFor(chat, a.ID)
	if err != nil {
		t.Fatalf("DuelSpeedFor: %v", err)
	}
	if sp.Games != 2 || sp.AvgSecs != 75 || sp.BestSecs != 50 {
		t.Errorf("DuelSpeedFor(Alice): want games2 avg75 best50, got %+v", sp)
	}

	// A player with no timed duel → Games 0.
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol")
	spC, err := st.DuelSpeedFor(chat, c.ID)
	if err != nil {
		t.Fatalf("DuelSpeedFor Carol: %v", err)
	}
	if spC.Games != 0 {
		t.Errorf("DuelSpeedFor(Carol): want games0, got %+v", spC)
	}
}
