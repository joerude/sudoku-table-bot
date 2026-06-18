package storage

import (
	"path/filepath"
	"testing"

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
