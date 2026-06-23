package storage

import (
	"path/filepath"
	"testing"
)

func TestActiveSeasonIdempotent(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}

	se1, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("first ActiveSeason: %v", err)
	}
	se2, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("second ActiveSeason: %v", err)
	}
	if se1.ID != se2.ID {
		t.Errorf("want same season ID, got %d vs %d", se1.ID, se2.ID)
	}

	var count int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM seasons WHERE chat_id=? AND status='active'`, chat,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("want exactly 1 active season, got %d", count)
	}
}

func TestActiveSeasonIndexEnforced(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ActiveSeason(chat); err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}

	// Direct insert of a second active season must be rejected by the partial unique index.
	_, err := st.db.Exec(
		`INSERT INTO seasons(chat_id, number, target, points_table, status)
		 VALUES(?, 2, 100, '[3,1,0]', 'active')`, chat)
	if err == nil {
		t.Error("expected error inserting second active season, got nil")
	}
}

func TestActiveSeasonRolloverStillWorks(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-1003)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}

	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}

	// winnerID=0 is acceptable (winner_id is nullable).
	newSe, err := st.CloseSeason(se, 0)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}
	if newSe.Number != 2 {
		t.Errorf("want season 2, got %d", newSe.Number)
	}

	// Old season must be archived.
	old, err := st.SeasonByID(se.ID)
	if err != nil {
		t.Fatalf("SeasonByID: %v", err)
	}
	if old.Status != "archived" {
		t.Errorf("old season: want archived, got %s", old.Status)
	}

	// Exactly one active season remains.
	var count int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM seasons WHERE chat_id=? AND status='active'`, chat,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("want exactly 1 active season after rollover, got %d", count)
	}
}

// TestMigrateDedupsActiveSeasonsOnReopen: if the DB somehow accumulated multiple
// active seasons per chat (e.g. restored from a backup), a second Open must
// archive the extras, keeping the one with the most completed games (ties: lowest id).
func TestMigrateDedupsActiveSeasonsOnReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "dedup.db")

	// First Open: normal bootstrap — creates schema + index.
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}

	const chat = int64(-9001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}

	// Create season #1 (the "real" one with a completed game).
	se1, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	gid, err := st.CreatePendingGame(chat, se1.ID, a.ID, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	st.AddPick(gid, a.ID)
	st.AddPick(gid, b.ID)
	st.FinalizeGame(gid, se1.PointsTable)

	// Temporarily drop the unique index so we can inject duplicate active seasons.
	if _, err := st.db.Exec(`DROP INDEX IF EXISTS idx_one_active_season`); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	// Insert two more empty active seasons (no completed games).
	for _, num := range []int{2, 3} {
		if _, err := st.db.Exec(
			`INSERT INTO seasons(chat_id, number, target, points_table, status)
			 VALUES(?, ?, 100, '[3,1,0]', 'active')`, chat, num); err != nil {
			t.Fatalf("insert extra active season %d: %v", num, err)
		}
	}

	// Verify we have 3 active seasons before reopen.
	var before int
	st.db.QueryRow(`SELECT COUNT(*) FROM seasons WHERE chat_id=? AND status='active'`, chat).Scan(&before)
	if before != 3 {
		t.Fatalf("precondition: want 3 active seasons, got %d", before)
	}
	st.Close()

	// Second Open: migrate() dedup runs, should archive the two empty extras.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open (reopen): %v", err)
	}
	defer st2.Close()

	var after int
	st2.db.QueryRow(`SELECT COUNT(*) FROM seasons WHERE chat_id=? AND status='active'`, chat).Scan(&after)
	if after != 1 {
		t.Errorf("after reopen: want exactly 1 active season, got %d", after)
	}

	// The surviving active season must be se1 (the one with the completed game).
	surviving, err := st2.activeSeasonRow(chat)
	if err != nil {
		t.Fatalf("activeSeasonRow after reopen: %v", err)
	}
	if surviving.ID != se1.ID {
		t.Errorf("wrong season survived: want id=%d (has completed game), got id=%d", se1.ID, surviving.ID)
	}
}
