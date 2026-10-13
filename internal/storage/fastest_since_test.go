package storage

import "testing"

// TestFastestSinceNullDifficulty: a timed game without a difficulty (NULL)
// must not break the scan, which used to take the weekly digest down.
func TestFastestSinceNullDifficulty(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4301)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	gid, err := st.CreatePendingGame(chat, se.ID, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPickTimed(gid, a.ID, 95); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPickTimed(gid, b.ID, 120); err != nil {
		t.Fatal(err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	rec, err := st.FastestSince(chat, "1970-01-01 00:00:00")
	if err != nil {
		t.Fatalf("FastestSince with NULL difficulty: %v", err)
	}
	if rec == nil || rec.Secs != 95 || rec.Name != "Alice" || rec.Difficulty != "" {
		t.Errorf("FastestSince = %+v, want Alice 95s with empty difficulty", rec)
	}
}
