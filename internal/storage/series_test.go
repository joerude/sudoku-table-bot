package storage

import "testing"

// TestSeriesSettle: a bo3 is decided at two game wins; a game nobody finished
// does not count; deleting a game takes its win back.
func TestSeriesSettle(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	sid, err := st.CreateSeries(chat, a.ID, b.ID, 3, "hard")
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	inSeries := func(gid int64) int64 {
		t.Helper()
		if err := st.SetGameSeries(gid, sid); err != nil {
			t.Fatalf("SetGameSeries: %v", err)
		}
		return gid
	}

	g1 := inSeries(makeDuelGame(t, st, chat, se.ID, 1, a.ID, b.ID, b.ID))
	// Void game: both joined, nobody finished.
	void, _ := st.CreatePendingGame(chat, se.ID, 1, "hard", "hardcore")
	_ = st.SetDuelTarget(void, b.ID)
	_ = st.AddDNF(void, a.ID)
	_ = st.AddDNF(void, b.ID)
	if err := st.FinalizeGame(void, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame void: %v", err)
	}
	inSeries(void)

	got, games, err := st.SettleSeries(sid)
	if err != nil {
		t.Fatalf("SettleSeries: %v", err)
	}
	if got.Status != "active" || CompletedCount(games) != 2 {
		t.Fatalf("after 1 win + void: status=%s completed=%d, want active/2", got.Status, CompletedCount(games))
	}

	g3 := inSeries(makeDuelGame(t, st, chat, se.ID, 1, a.ID, b.ID, b.ID))
	got, games, _ = st.SettleSeries(sid)
	aw, bw := SeriesScore(games, a.ID, b.ID)
	if got.Status != "done" || got.WinnerID.Int64 != a.ID || aw != 2 || bw != 0 {
		t.Fatalf("after 2 wins: status=%s winner=%d score=%d:%d", got.Status, got.WinnerID.Int64, aw, bw)
	}
	if w, l, _ := st.SeriesHeadToHead(chat, a.ID, b.ID); w != 1 || l != 0 {
		t.Fatalf("SeriesHeadToHead = %d:%d, want 1:0", w, l)
	}
	if pairs, _ := st.SeriesPairs(chat); len(pairs) != 1 || pairs[0].AWins+pairs[0].BWins != 1 {
		t.Fatalf("SeriesPairs = %+v", pairs)
	}

	if err := st.SoftDeleteGame(g3); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}
	got, _ = st.SeriesByID(sid) // the delete itself re-settles the series
	if got.Status != "active" || got.WinnerID.Valid {
		t.Fatalf("after delete: status=%s winner=%v, want active, none", got.Status, got.WinnerID)
	}
	if s, _ := st.SeriesOfGame(g1); s == nil || s.ID != sid {
		t.Fatalf("SeriesOfGame(g1) = %+v", s)
	}
}

// TestSeriesCancelAndAbandon: a declined first challenge cancels the series,
// one with a recorded game survives the cancel; idle series get abandoned.
func TestSeriesCancelAndAbandon(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3002)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	s1, _ := st.CreateSeries(chat, a.ID, b.ID, 5, "easy")
	g, _ := st.CreatePendingGame(chat, se.ID, 1, "easy", "hardcore")
	_ = st.SetGameSeries(g, s1)
	if err := st.CancelSeriesOfGame(g); err != nil {
		t.Fatalf("CancelSeriesOfGame: %v", err)
	}
	if got, _ := st.SeriesByID(s1); got.Status != "cancelled" {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}

	s2, _ := st.CreateSeries(chat, a.ID, b.ID, 3, "easy")
	played := makeDuelGame(t, st, chat, se.ID, 1, a.ID, b.ID, b.ID)
	_ = st.SetGameSeries(played, s2)
	_ = st.CancelSeriesOfGame(played)
	if got, _ := st.SeriesByID(s2); got.Status != "active" {
		t.Fatalf("played series status = %s, want active", got.Status)
	}

	if out, _ := st.AbandonStaleSeries(120); len(out) != 0 {
		t.Fatalf("fresh series abandoned: %+v", out)
	}
	if _, err := st.db.Exec(`UPDATE games SET created_at=datetime('now','-3 hours'),
		completed_at=datetime('now','-3 hours') WHERE id=?`, played); err != nil {
		t.Fatal(err)
	}
	out, err := st.AbandonStaleSeries(120)
	if err != nil || len(out) != 1 || out[0].ID != s2 {
		t.Fatalf("AbandonStaleSeries = %+v, %v; want series %d", out, err, s2)
	}
}

// TestSeriesLaterGames: a later series game nobody started burns (the first
// game is a challenge and is left to ExpireStaleDuels), and the whole series
// counts as one challenge in ChallengeStats.
func TestSeriesLaterGames(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3003)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	sid, _ := st.CreateSeries(chat, a.ID, b.ID, 3, "easy")
	first := makeDuelGame(t, st, chat, se.ID, 1, a.ID, b.ID, b.ID)
	_ = st.SetGameSeries(first, sid)
	// Game 2, started by Bob's tap: accepted at once, never played.
	next, _ := st.CreatePendingGame(chat, se.ID, 2, "easy", "hardcore")
	_ = st.SetDuelTarget(next, a.ID)
	_ = st.SetGameSeries(next, sid)
	if _, err := st.AcceptDuel(next); err != nil {
		t.Fatal(err)
	}

	stats, err := st.ChallengeStats(chat)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range stats {
		if c.Issued+c.Received != 1 {
			t.Errorf("%s: issued=%d received=%d, want one challenge in total", c.Name, c.Issued, c.Received)
		}
	}

	if out, _ := st.ExpireStaleSeriesGames(20, nil); len(out) != 0 {
		t.Fatalf("fresh game burnt: %+v", out)
	}
	if out, _ := st.AbandonStaleSeries(0); len(out) != 0 {
		t.Fatalf("series with a waiting game abandoned: %+v", out)
	}
	_, _ = st.db.Exec(`UPDATE games SET created_at=datetime('now','-30 minutes') WHERE id IN (?, ?)`, first, next)
	if out, _ := st.ExpireStaleSeriesGames(20, func(StaleSeriesGame) bool { return true }); len(out) != 0 {
		t.Fatalf("kept game burnt: %+v", out)
	}
	out, err := st.ExpireStaleSeriesGames(20, nil)
	if err != nil || len(out) != 1 || out[0].GameID != next {
		t.Fatalf("ExpireStaleSeriesGames = %+v, %v; want game %d", out, err, next)
	}
	if p, _ := st.ActivePendingGame(chat); p != nil {
		t.Errorf("pending slot still taken by game %d", p.ID)
	}
	if got, _ := st.SeriesByID(sid); got.Status != "active" {
		t.Errorf("series status = %s, want active (the button starts the game again)", got.Status)
	}
}
