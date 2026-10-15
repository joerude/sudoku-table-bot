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

// TestPointsSince: only points of games completed inside the window count,
// per player, with the game count.
func TestPointsSince(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4302)
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

	gains, games, err := st.PointsSince(chat, se.ID, "1970-01-01 00:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if games != 1 || gains[a.ID] <= gains[b.ID] || gains[a.ID] == 0 {
		t.Errorf("PointsSince = %v, %d games; want Alice > Bob > =0 over 1 game", gains, games)
	}
	gains, games, err = st.PointsSince(chat, se.ID, "2999-01-01 00:00:00")
	if err != nil || games != 0 || len(gains) != 0 {
		t.Errorf("future window: gains=%v games=%d err=%v; want empty", gains, games, err)
	}
}
