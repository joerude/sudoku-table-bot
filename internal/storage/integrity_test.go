package storage

import (
	"errors"
	"testing"
)

// integrityFixture is a chat with one season, two players and a pending game.
func integrityFixture(t *testing.T) (st *Store, se *Season, a, b *Player, gid int64) {
	t.Helper()
	st = openTemp(t)
	const chat = int64(-7001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	var err error
	if se, err = st.ActiveSeason(chat); err != nil {
		t.Fatal(err)
	}
	a, _, _ = st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ = st.RegisterPlayer(chat, 2, "Bob")
	if gid, err = st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore"); err != nil {
		t.Fatal(err)
	}
	return
}

func TestAddPickAfterFinalizeIsNoop(t *testing.T) {
	st, se, a, b, gid := integrityFixture(t)
	st.AddPick(gid, a.ID)
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPick(gid, b.ID); err != nil {
		t.Fatalf("AddPick after finalize: %v", err)
	}
	if err := st.AddDNF(gid, b.ID); err != nil {
		t.Fatalf("AddDNF after finalize: %v", err)
	}
	if ids, _ := st.PickedPlayerIDs(gid); len(ids) != 1 {
		t.Errorf("closed game must not gain results, got %v", ids)
	}
}

func TestAddPickOnDeletedGameIsNoop(t *testing.T) {
	st, _, a, _, gid := integrityFixture(t)
	if err := st.SoftDeleteGame(gid); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPick(gid, a.ID); err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.PickedPlayerIDs(gid); len(ids) != 0 {
		t.Errorf("deleted game must not gain results, got %v", ids)
	}
}

func TestAddPickRanksSequentialWithDNF(t *testing.T) {
	st, _, a, b, gid := integrityFixture(t)
	cp, _, _ := st.RegisterPlayer(-7001, 3, "Carol")
	if err := st.AddPickTimed(gid, a.ID, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPick(gid, b.ID); err != nil {
		t.Fatal(err)
	}
	// Same player twice keeps the first rank.
	if err := st.AddPick(gid, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDNF(gid, cp.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := st.GameResults(gid)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		rank int
	}{{"Alice", 1}, {"Bob", 2}, {"Carol", 0}}
	if len(rows) != len(want) {
		t.Fatalf("want %d rows, got %+v", len(want), rows)
	}
	for i, w := range want {
		if rows[i].Name != w.name || rows[i].Rank != w.rank {
			t.Errorf("row %d = %s/%d, want %s/%d", i, rows[i].Name, rows[i].Rank, w.name, w.rank)
		}
	}
	if rows[0].Duration != 100 || rows[1].Duration != 0 {
		t.Errorf("durations not stored: %+v", rows)
	}
}

func pointsOf(t *testing.T, st *Store, gid int64) map[string]int {
	t.Helper()
	rows, err := st.GameResults(gid)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.Name] = r.Points
	}
	return out
}

func TestClearAndRemoveLastPickSkipClosedGames(t *testing.T) {
	st, se, a, b, gid := integrityFixture(t)
	st.AddPick(gid, a.ID)
	st.AddPick(gid, b.ID)
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveLastPick(gid); err != nil {
		t.Fatal(err)
	}
	if err := st.ClearResults(gid); err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.PickedPlayerIDs(gid); len(ids) != 2 {
		t.Fatalf("completed game lost results: %v", ids)
	}
	if p := pointsOf(t, st, gid); p["Alice"] != 3 {
		t.Errorf("points changed: %v", p)
	}
}

func TestClearAndRemoveLastPickWorkOnPending(t *testing.T) {
	st, _, a, b, gid := integrityFixture(t)
	st.AddPick(gid, a.ID)
	st.AddPick(gid, b.ID)
	if err := st.RemoveLastPick(gid); err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.PickedPlayerIDs(gid); len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("undo should drop Bob only, got %v", ids)
	}
	if err := st.ClearResults(gid); err != nil {
		t.Fatal(err)
	}
	if ids, _ := st.PickedPlayerIDs(gid); len(ids) != 0 {
		t.Fatalf("reset should clear picks, got %v", ids)
	}
}

func TestReopenGameThenRefinalize(t *testing.T) {
	st, se, a, b, gid := integrityFixture(t)
	st.AddPick(gid, a.ID)
	st.AddPick(gid, b.ID)
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	if err := st.ReopenGame(gid); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	g, _ := st.GameByID(gid)
	if g.Status != "pending" {
		t.Fatalf("reopened status = %s", g.Status)
	}
	st.AddPick(gid, b.ID)
	st.AddPick(gid, a.ID)
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("refinalize after reopen: %v", err)
	}
	if p := pointsOf(t, st, gid); p["Bob"] != 3 || p["Alice"] != 1 {
		t.Errorf("points after edit: %v", p)
	}
}

func TestReopenGameSkipsPendingAndDeleted(t *testing.T) {
	st, se, a, _, gid := integrityFixture(t)
	st.AddPick(gid, a.ID)
	// Pending: a stale "edit" tap must not wipe in-progress picks.
	if err := st.ReopenGame(gid); !errors.Is(err, ErrGameNotCompleted) {
		t.Fatalf("pending reopen: want ErrGameNotCompleted, got %v", err)
	}
	if ids, _ := st.PickedPlayerIDs(gid); len(ids) != 1 {
		t.Fatalf("pending picks wiped: %v", ids)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	if err := st.SoftDeleteGame(gid); err != nil {
		t.Fatal(err)
	}
	if err := st.ReopenGame(gid); !errors.Is(err, ErrGameNotCompleted) {
		t.Fatalf("deleted reopen: want ErrGameNotCompleted, got %v", err)
	}
	if g, _ := st.GameByID(gid); g.Status != "completed" {
		t.Errorf("deleted game status changed to %s", g.Status)
	}
}
