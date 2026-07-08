package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestGameFlowAndStandings(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-100)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	if se.Number != 1 || se.Target != domain.DefaultTarget {
		t.Fatalf("unexpected season: %+v", se)
	}

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	cp, _, _ := st.RegisterPlayer(chat, 3, "Carol")

	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	// Finish order: Alice, Bob, Carol -> 3, 1, 0
	for _, p := range []*Player{a, b, cp} {
		if err := st.AddPick(gid, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	if pending, _ := st.ActivePendingGame(chat); pending != nil {
		t.Errorf("game should no longer be pending")
	}

	standings, err := st.Standings(chat, se.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(standings) != 3 {
		t.Fatalf("want 3 standings, got %d", len(standings))
	}
	if standings[0].Name != "Alice" || standings[0].Points != 3 || standings[0].Wins != 1 {
		t.Errorf("leader wrong: %+v", standings[0])
	}
	if standings[1].Points != 1 || standings[2].Points != 0 {
		t.Errorf("points wrong: %+v", standings)
	}
}

func TestTwoPlayerScoringAndDNF(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-500)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")

	// Game 1: both finish, A then B -> 3, 1.
	g1, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.AddPick(g1, a.ID)
	st.AddPick(g1, b.ID)
	st.FinalizeGame(g1, se.PointsTable)

	// Game 2: only A finishes (B did not finish) -> A +3, B +0.
	g2, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.AddPick(g2, a.ID)
	st.FinalizeGame(g2, se.PointsTable)

	got := map[string]int{}
	for _, s := range mustStandings(t, st, chat, se.ID) {
		got[s.Name] = s.Points
	}
	if got["A"] != 6 {
		t.Errorf("A want 6 (3+3), got %d", got["A"])
	}
	if got["B"] != 1 {
		t.Errorf("B want 1 (1+0), got %d", got["B"])
	}
}

func TestNickMapping(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-300)
	st.EnsureChat(chat, 1)
	p, _, _ := st.RegisterPlayer(chat, 1, "Zhoomart")

	// Unknown nick before set.
	if got, _ := st.PlayerByNick(chat, "zhoo"); got != nil {
		t.Errorf("expected no match before SetNick")
	}
	if err := st.SetNick(p.ID, "Zhoo"); err != nil {
		t.Fatal(err)
	}
	// Case-insensitive match.
	got, err := st.PlayerByNick(chat, "zhoo")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != p.ID {
		t.Errorf("expected to match player by nick (case-insensitive), got %+v", got)
	}
	// Different chat must not match.
	if other, _ := st.PlayerByNick(-999, "Zhoo"); other != nil {
		t.Errorf("nick must be scoped to chat")
	}
}

func TestSoftDeleteRestore(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-400)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")

	gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.AddPick(gid, a.ID)
	st.AddPick(gid, b.ID)
	st.FinalizeGame(gid, se.PointsTable)

	if before, _ := st.Standings(chat, se.ID); before[0].Points != 3 {
		t.Fatalf("expected leader 3 before delete, got %d", before[0].Points)
	}

	// Soft delete removes it from standings...
	if err := st.SoftDeleteGame(gid); err != nil {
		t.Fatal(err)
	}
	for _, s := range mustStandings(t, st, chat, se.ID) {
		if s.Points != 0 {
			t.Errorf("deleted game should not count, %s has %d", s.Name, s.Points)
		}
	}

	// ...and restore brings it back.
	if err := st.RestoreGame(gid); err != nil {
		t.Fatal(err)
	}
	if after := mustStandings(t, st, chat, se.ID); after[0].Points != 3 {
		t.Errorf("restore should bring points back, got %d", after[0].Points)
	}
}

// TestDNFRecording: 1 finisher (rank 1, timed) + 1 DNF (AddDNF) → correct ordering,
// points, standings Games count, and finishOrder exclusion.
func TestDNFRecording(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-600)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	// Alice finishes first with a solve time.
	if err := st.AddPickTimed(gid, a.ID, 120); err != nil {
		t.Fatal(err)
	}
	// Bob joined but did not finish.
	if err := st.AddDNF(gid, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	// GameResults: finisher (rank 1) first, then DNF (rank 0).
	rows, err := st.GameResults(gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].Rank != 1 || rows[0].Name != "Alice" {
		t.Errorf("rows[0]: want Alice rank 1, got %+v", rows[0])
	}
	if rows[0].Points != 3 {
		t.Errorf("Alice points: want 3 (default table rank-1), got %d", rows[0].Points)
	}
	if rows[0].Duration != 120 {
		t.Errorf("Alice duration: want 120, got %d", rows[0].Duration)
	}
	if rows[1].Rank != 0 || rows[1].Name != "Bob" {
		t.Errorf("rows[1]: want Bob rank 0, got %+v", rows[1])
	}
	if rows[1].Points != 0 {
		t.Errorf("Bob points: want 0, got %d", rows[1].Points)
	}

	// Standings: Bob counts as a played game but has no wins.
	standings, err := st.Standings(chat, se.ID)
	if err != nil {
		t.Fatal(err)
	}
	idx := map[string]Standing{}
	for _, s := range standings {
		idx[s.Name] = s
	}
	aliceSt := idx["Alice"]
	bobSt := idx["Bob"]
	if aliceSt.Wins != 1 || aliceSt.Games != 1 {
		t.Errorf("Alice standings: want Wins=1 Games=1, got %+v", aliceSt)
	}
	if bobSt.Wins != 0 || bobSt.Games != 1 {
		t.Errorf("Bob standings: want Wins=0 Games=1, got %+v", bobSt)
	}
	if bobSt.Points != 0 {
		t.Errorf("Bob points: want 0, got %d", bobSt.Points)
	}

	// RecentGames finishOrder excludes DNF row.
	recent, err := st.RecentGames(chat, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("want 1 recent game, got %d", len(recent))
	}
	if len(recent[0].Order) != 1 || recent[0].Order[0] != "Alice" {
		t.Errorf("finishOrder: want [Alice], got %v", recent[0].Order)
	}
}

// TestSetPickDuration: fills NULL duration; is a no-op when already set; GameResults reports it.
func TestSetPickDuration(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-700)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	// Alice picked without a time (duration_secs NULL).
	if err := st.AddPick(gid, a.ID); err != nil {
		t.Fatal(err)
	}
	// Bob picked with an explicit time.
	if err := st.AddPickTimed(gid, b.ID, 200); err != nil {
		t.Fatal(err)
	}

	// Fill Alice's NULL duration.
	if err := st.SetPickDuration(gid, a.ID, 150); err != nil {
		t.Fatalf("SetPickDuration: %v", err)
	}
	// Attempt to overwrite Bob's already-set duration — must be a no-op.
	if err := st.SetPickDuration(gid, b.ID, 999); err != nil {
		t.Fatalf("SetPickDuration no-op: %v", err)
	}

	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	rows, err := st.GameResults(gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	idx := map[string]ResultRow{}
	for _, r := range rows {
		idx[r.Name] = r
	}
	if idx["Alice"].Duration != 150 {
		t.Errorf("Alice duration: want 150, got %d", idx["Alice"].Duration)
	}
	if idx["Bob"].Duration != 200 {
		t.Errorf("Bob duration: want 200 (unchanged), got %d", idx["Bob"].Duration)
	}
}

func TestWeeklyDigestRoundTrip(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-800)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}

	if err := st.SetWeeklyDigest(chat, false); err != nil {
		t.Fatal(err)
	}
	c, err := st.GetChat(chat)
	if err != nil {
		t.Fatal(err)
	}
	if c.WeeklyDigest {
		t.Errorf("after SetWeeklyDigest(false): want false, got true")
	}

	if err := st.SetWeeklyDigest(chat, true); err != nil {
		t.Fatal(err)
	}
	c, err = st.GetChat(chat)
	if err != nil {
		t.Fatal(err)
	}
	if !c.WeeklyDigest {
		t.Errorf("after SetWeeklyDigest(true): want true, got false")
	}
}

func TestCompletedTodayCountsDuels(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3050)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// One normal game and one duel game, both completed "now".
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	today := time.Now().UTC().Format("2006-01-02")
	n, err := st.CompletedToday(chat, today)
	if err != nil {
		t.Fatalf("CompletedToday: %v", err)
	}
	if n != 2 {
		t.Errorf("CompletedToday: want 2 (duel counts as played today), got %d", n)
	}
}

func mustStandings(t *testing.T, st *Store, chat, season int64) []Standing {
	t.Helper()
	s, err := st.Standings(chat, season)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSeasonRollover(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-200)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	// Low target so a couple of wins close the season.
	if err := st.UpdateSeasonTarget(se.ID, 5); err != nil {
		t.Fatal(err)
	}
	se, _ = st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Two games, Alice wins both -> 6 points >= 5.
	for i := 0; i < 2; i++ {
		gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		st.AddPick(gid, a.ID)
		st.AddPick(gid, b.ID)
		st.FinalizeGame(gid, se.PointsTable)
	}

	standings, _ := st.Standings(chat, se.ID)
	if standings[0].Points < se.Target {
		t.Fatalf("expected leader to reach target, got %d/%d", standings[0].Points, se.Target)
	}

	newSeason, err := st.CloseSeason(se, standings[0].PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if newSeason.Number != 2 {
		t.Errorf("want season 2, got %d", newSeason.Number)
	}
	if newSeason.Target != 5 {
		t.Errorf("new season should inherit target 5, got %d", newSeason.Target)
	}

	// Active season is now the fresh one, with zero points so far.
	active, _ := st.ActiveSeason(chat)
	if active.ID != newSeason.ID {
		t.Errorf("active season should be the new one")
	}
	fresh, _ := st.Standings(chat, active.ID)
	for _, s := range fresh {
		if s.Points != 0 {
			t.Errorf("new season should start at 0, %s has %d", s.Name, s.Points)
		}
	}
}

func TestActivePendingGameMetadata(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-200)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := st.CreatePendingGame(chat, se.ID, 77, "hard", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	g, err := st.ActivePendingGame(chat)
	if err != nil {
		t.Fatal(err)
	}
	if g == nil || g.ID != gid {
		t.Fatalf("pending game not found: %+v", g)
	}
	if !g.CreatedBy.Valid || g.CreatedBy.Int64 != 77 {
		t.Errorf("CreatedBy = %+v, want 77", g.CreatedBy)
	}
	if g.CreatedAt == "" {
		t.Fatal("CreatedAt empty")
	}
	if _, err := time.Parse("2006-01-02 15:04:05", g.CreatedAt); err != nil {
		t.Errorf("CreatedAt %q not parseable: %v", g.CreatedAt, err)
	}
	byID, err := st.GameByID(gid)
	if err != nil {
		t.Fatal(err)
	}
	if byID.CreatedAt != g.CreatedAt || byID.CreatedBy != g.CreatedBy {
		t.Errorf("GameByID metadata mismatch: %+v vs %+v", byID, g)
	}
}
