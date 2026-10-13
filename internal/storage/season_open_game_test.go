package storage

import (
	"testing"
	"time"
)

func dbTime(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") }

// TestHasOpenSeasonGame: only a live, non-duel pending game of the season that
// was started before the cutoff and is younger than the age limit counts.
func TestHasOpenSeasonGame(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4201)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	now := time.Now().UTC()
	cutoff := dbTime(now.Add(time.Hour)) // "deadline" an hour from now
	has := func() bool {
		t.Helper()
		ok, err := st.HasOpenSeasonGame(se.ID, cutoff, 180)
		if err != nil {
			t.Fatalf("HasOpenSeasonGame: %v", err)
		}
		return ok
	}
	if has() {
		t.Fatal("no games: want false")
	}

	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if !has() {
		t.Error("fresh pending game: want true")
	}

	// Older than the age limit: stale, no longer holds the season open.
	if _, err := st.db.Exec(`UPDATE games SET created_at=? WHERE id=?`,
		dbTime(now.Add(-4*time.Hour)), gid); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Error("4h-old pending game: want false")
	}

	// Started after the cutoff: belongs to the next season's time, ignored.
	if _, err := st.db.Exec(`UPDATE games SET created_at=? WHERE id=?`,
		dbTime(now.Add(2*time.Hour)), gid); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Error("game created after cutoff: want false")
	}

	// Back inside the window, then soft-deleted.
	if _, err := st.db.Exec(`UPDATE games SET created_at=? WHERE id=?`,
		dbTime(now.Add(-time.Minute)), gid); err != nil {
		t.Fatal(err)
	}
	if !has() {
		t.Fatal("pending game inside the window: want true")
	}
	if err := st.SoftDeleteGame(gid); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Error("soft-deleted game: want false")
	}

	// A completed game does not hold the season open.
	done := makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	_ = done
	if has() {
		t.Error("completed game: want false")
	}

	// A pending duel never holds the season open.
	duel, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDuelTarget(duel, b.ID); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Error("pending duel: want false")
	}
}
