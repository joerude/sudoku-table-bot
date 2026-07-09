package storage

import (
	"testing"
)

// helper: seed a completed game with timed picks and return the game ID.
func seedTimedGame(t *testing.T, st *Store, chatID, seasonID int64, difficulty string, picks []struct {
	playerID    int64
	durationSec int64
	timed       bool // true → AddPickTimed; false → AddPick (NULL duration)
}) int64 {
	t.Helper()
	gid, err := st.CreatePendingGame(chatID, seasonID, 1, difficulty, "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	for _, p := range picks {
		if p.timed {
			if err := st.AddPickTimed(gid, p.playerID, p.durationSec); err != nil {
				t.Fatalf("AddPickTimed: %v", err)
			}
		} else {
			if err := st.AddPick(gid, p.playerID); err != nil {
				t.Fatalf("AddPick: %v", err)
			}
		}
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

// TestSpeedForHappyPath: player with several medium timed games → correct avg/best/count.
// Durations: 100, 130 → avg = 115.0 → rounds to 115 (exact, no ambiguity).
func TestSpeedForHappyPath(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")

	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 100, timed: true}})
	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 130, timed: true}})

	got, err := st.SpeedFor(chat, se.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor: %v", err)
	}
	if got.Games != 2 {
		t.Errorf("Games: want 2, got %d", got.Games)
	}
	if got.BestSecs != 100 {
		t.Errorf("BestSecs: want 100, got %d", got.BestSecs)
	}
	if got.AvgSecs != 115 {
		t.Errorf("AvgSecs: want 115, got %d", got.AvgSecs)
	}
}

// TestSpeedForExcludesManualRows: AddPick (NULL duration) must not affect avg/count.
func TestSpeedForExcludesManualRows(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1002)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")

	// One timed game.
	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 120, timed: true}})
	// One manual (NULL) game — same player, same difficulty.
	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID}})

	got, err := st.SpeedFor(chat, se.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor: %v", err)
	}
	if got.Games != 1 {
		t.Errorf("Games: want 1 (NULL excluded), got %d", got.Games)
	}
	if got.AvgSecs != 120 {
		t.Errorf("AvgSecs: want 120, got %d", got.AvgSecs)
	}
}

// TestSpeedForExcludesDNF: a game where player has no row does not count.
func TestSpeedForExcludesDNF(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1003)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Game where only Bob finishes (Alice is DNF — no row for her).
	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: b.ID, durationSec: 90, timed: true}})

	got, err := st.SpeedFor(chat, se.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor: %v", err)
	}
	if got.Games != 0 {
		t.Errorf("Games: want 0 (Alice DNF), got %d", got.Games)
	}
	if got.AvgSecs != 0 || got.BestSecs != 0 {
		t.Errorf("want zero SpeedStat for DNF player, got %+v", got)
	}
}

// TestSpeedForDifficultyFilter: a hard timed game does not affect the medium query.
func TestSpeedForDifficultyFilter(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1004)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")

	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 200, timed: true}})
	seedTimedGame(t, st, chat, se.ID, "hard", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 50, timed: true}})

	// medium query should only see the medium game.
	med, err := st.SpeedFor(chat, se.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor medium: %v", err)
	}
	if med.Games != 1 || med.AvgSecs != 200 {
		t.Errorf("medium: want Games=1,Avg=200 got %+v", med)
	}

	// hard query should only see the hard game.
	hard, err := st.SpeedFor(chat, se.ID, a.ID, "hard")
	if err != nil {
		t.Fatalf("SpeedFor hard: %v", err)
	}
	if hard.Games != 1 || hard.AvgSecs != 50 {
		t.Errorf("hard: want Games=1,Avg=50 got %+v", hard)
	}
}

// TestSpeedForSeasonFilter: a timed game in another season is excluded.
func TestSpeedForSeasonFilter(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1005)
	st.EnsureChat(chat, 1)
	se1, _ := st.ActiveSeason(chat)
	// Set a low target so we can trigger season rollover.
	st.UpdateSeasonTarget(se1.ID, 3)
	se1, _ = st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")

	// Game in season 1 with Alice timed.
	seedTimedGame(t, st, chat, se1.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 80, timed: true}})

	// Roll over to season 2.
	standings, _ := st.Standings(chat, se1.ID)
	se2, err := st.CloseSeason(se1, standings[0].PlayerID)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}

	// Game in season 2 with Alice timed.
	seedTimedGame(t, st, chat, se2.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: a.ID, durationSec: 200, timed: true}})

	// Season 1 query must only see the 80s game.
	s1, err := st.SpeedFor(chat, se1.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor se1: %v", err)
	}
	if s1.Games != 1 || s1.AvgSecs != 80 {
		t.Errorf("season1: want Games=1,Avg=80 got %+v", s1)
	}

	// Season 2 query must only see the 200s game.
	s2, err := st.SpeedFor(chat, se2.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor se2: %v", err)
	}
	if s2.Games != 1 || s2.AvgSecs != 200 {
		t.Errorf("season2: want Games=1,Avg=200 got %+v", s2)
	}
}

// TestSpeedForEmpty: player with zero timed games → all-zero SpeedStat, nil error.
func TestSpeedForEmpty(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1006)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")

	got, err := st.SpeedFor(chat, se.ID, a.ID, "medium")
	if err != nil {
		t.Fatalf("SpeedFor: %v", err)
	}
	want := &SpeedStat{}
	if *got != *want {
		t.Errorf("want zero SpeedStat, got %+v", got)
	}
}

// TestSpeedboardOrderingAndThreshold: ranked/fewer split + ordering.
// 3 players at medium, minGames=3:
//   - Alice: 3 games [60,90,90] → avg=80 → ranked
//   - Bob:   3 games [70,80,90] → avg=80 → ranked (tie-break: same avg, same games → name asc → Alice before Bob)
//   - Carol: 2 games [50,60]    → avg=55 → fewer (below minGames=3)
//   - Dave:  0 timed medium games → absent
//
// Ordering within ranked: avg asc. Alice and Bob both avg=80 → tie → games desc (same 3) → name asc → Alice before Bob.
func TestSpeedboardOrderingAndThreshold(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1007)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	alice, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	bob, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	carol, _, _ := st.RegisterPlayer(chat, 3, "Carol")
	dave, _, _ := st.RegisterPlayer(chat, 4, "Dave")
	_ = dave

	// Alice: 3 timed games with durations 60, 90, 90 → avg=80.
	for _, d := range []int64{60, 90, 90} {
		seedTimedGame(t, st, chat, se.ID, "medium", []struct {
			playerID    int64
			durationSec int64
			timed       bool
		}{{playerID: alice.ID, durationSec: d, timed: true}})
	}
	// Bob: 3 timed games with durations 70, 80, 90 → avg=80.
	for _, d := range []int64{70, 80, 90} {
		seedTimedGame(t, st, chat, se.ID, "medium", []struct {
			playerID    int64
			durationSec int64
			timed       bool
		}{{playerID: bob.ID, durationSec: d, timed: true}})
	}
	// Carol: 2 timed games with durations 50, 60 → avg=55, below minGames=3.
	for _, d := range []int64{50, 60} {
		seedTimedGame(t, st, chat, se.ID, "medium", []struct {
			playerID    int64
			durationSec int64
			timed       bool
		}{{playerID: carol.ID, durationSec: d, timed: true}})
	}
	// Dave: only a manual (NULL) medium game → no timed medium games.
	seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: dave.ID}})

	ranked, fewer, err := st.Speedboard(chat, se.ID, "medium", 3)
	if err != nil {
		t.Fatalf("Speedboard: %v", err)
	}

	if len(ranked) != 2 {
		t.Fatalf("ranked: want 2, got %d: %+v", len(ranked), ranked)
	}
	if len(fewer) != 1 {
		t.Fatalf("fewer: want 1, got %d: %+v", len(fewer), fewer)
	}

	// Both Alice and Bob have avg=80; tie-break by name asc → Alice before Bob.
	if ranked[0].Name != "Alice" {
		t.Errorf("ranked[0]: want Alice (name tie-break asc), got %s", ranked[0].Name)
	}
	if ranked[0].AvgSecs != 80 {
		t.Errorf("ranked[0].AvgSecs: want 80, got %d", ranked[0].AvgSecs)
	}
	if ranked[1].Name != "Bob" {
		t.Errorf("ranked[1]: want Bob, got %s", ranked[1].Name)
	}

	if fewer[0].Name != "Carol" {
		t.Errorf("fewer[0]: want Carol, got %s", fewer[0].Name)
	}
	if fewer[0].AvgSecs != 55 {
		t.Errorf("fewer[0].AvgSecs: want 55, got %d", fewer[0].AvgSecs)
	}
}

// TestSpeedboardIgnoresDeletedGames: soft-deleted game drops its contribution.
func TestSpeedboardIgnoresDeletedGames(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1008)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	alice, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	bob, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// Alice: 3 timed games → ranked (minGames=3).
	for _, d := range []int64{60, 70, 80} {
		seedTimedGame(t, st, chat, se.ID, "medium", []struct {
			playerID    int64
			durationSec int64
			timed       bool
		}{{playerID: alice.ID, durationSec: d, timed: true}})
	}
	// Bob: 3 timed games; we'll delete one → drops to 2 → goes to fewer.
	gToDelete := seedTimedGame(t, st, chat, se.ID, "medium", []struct {
		playerID    int64
		durationSec int64
		timed       bool
	}{{playerID: bob.ID, durationSec: 50, timed: true}})
	for _, d := range []int64{60, 70} {
		seedTimedGame(t, st, chat, se.ID, "medium", []struct {
			playerID    int64
			durationSec int64
			timed       bool
		}{{playerID: bob.ID, durationSec: d, timed: true}})
	}

	// Before delete: both ranked.
	ranked, fewer, err := st.Speedboard(chat, se.ID, "medium", 3)
	if err != nil {
		t.Fatalf("Speedboard before delete: %v", err)
	}
	if len(ranked) != 2 || len(fewer) != 0 {
		t.Errorf("before delete: want 2 ranked 0 fewer, got ranked=%d fewer=%d", len(ranked), len(fewer))
	}

	// Soft-delete Bob's first game.
	if err := st.SoftDeleteGame(gToDelete); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}

	// After delete: Bob drops to 2 games → fewer.
	ranked, fewer, err = st.Speedboard(chat, se.ID, "medium", 3)
	if err != nil {
		t.Fatalf("Speedboard after delete: %v", err)
	}
	if len(ranked) != 1 {
		t.Errorf("after delete: want 1 ranked, got %d: %+v", len(ranked), ranked)
	}
	if ranked[0].Name != "Alice" {
		t.Errorf("after delete: want Alice ranked, got %s", ranked[0].Name)
	}
	if len(fewer) != 1 || fewer[0].Name != "Bob" {
		t.Errorf("after delete: want Bob in fewer, got %+v", fewer)
	}
	if fewer[0].Games != 2 {
		t.Errorf("Bob.Games after delete: want 2, got %d", fewer[0].Games)
	}
}

func TestTitlesBoard(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-9100)
	st.EnsureChat(chat, 1)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// No archived seasons yet → empty board.
	if tb, err := st.TitlesBoard(chat); err != nil || len(tb) != 0 {
		t.Fatalf("empty TitlesBoard: got %v err %v", tb, err)
	}

	win := func(w int64) {
		se, err := st.ActiveSeason(chat)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.CloseSeason(se, w); err != nil {
			t.Fatal(err)
		}
	}
	win(b.ID) // Bob
	win(a.ID) // Alice
	win(b.ID) // Bob → 2 titles, leads

	tb, err := st.TitlesBoard(chat)
	if err != nil {
		t.Fatalf("TitlesBoard: %v", err)
	}
	if len(tb) != 2 {
		t.Fatalf("want 2 rows, got %+v", tb)
	}
	if tb[0].Name != "Bob" || tb[0].Count != 2 {
		t.Errorf("row0: want Bob x2, got %+v", tb[0])
	}
	if tb[1].Name != "Alice" || tb[1].Count != 1 {
		t.Errorf("row1: want Alice x1, got %+v", tb[1])
	}
}
