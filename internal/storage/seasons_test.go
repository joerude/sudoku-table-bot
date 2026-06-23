package storage

import (
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
