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

	rec, err := st.FastestSince(chat, se.ID, "1970-01-01 00:00:00")
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

// TestSeasonGameTimes: only the season's counting games, oldest first, and the
// season's start time is readable.
func TestSeasonGameTimes(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4303)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	for i := 0; i < 2; i++ {
		gid, err := st.CreatePendingGame(chat, se.ID, 1, "", "")
		if err != nil {
			t.Fatal(err)
		}
		_ = st.AddPickTimed(gid, a.ID, 90)
		_ = st.AddPickTimed(gid, b.ID, 95)
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatal(err)
		}
	}
	// A pending game does not count.
	if _, err := st.CreatePendingGame(chat, se.ID, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	times, err := st.SeasonGameTimes(chat, se.ID)
	if err != nil || len(times) != 2 || times[0] > times[1] {
		t.Errorf("SeasonGameTimes = %v, %v; want 2 ascending", times, err)
	}
	if at, err := st.SeasonStartedAt(se.ID); err != nil || len(at) != len("2006-01-02 15:04:05") {
		t.Errorf("SeasonStartedAt = %q, %v", at, err)
	}
}

// TestWeekStatsStayInSeason: the digest's counters see only the asked season.
func TestWeekStatsStayInSeason(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4302)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	old, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	gid, _ := st.CreatePendingGame(chat, old.ID, 1, "medium", "hardcore")
	st.AddPickTimed(gid, a.ID, 95)
	if err := st.FinalizeGame(gid, old.PointsTable); err != nil {
		t.Fatal(err)
	}
	se, err := st.CloseSeason(old, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	const since = "1970-01-01 00:00:00"
	if n, err := st.GamesSince(chat, se.ID, since); err != nil || n != 0 {
		t.Errorf("GamesSince(new season) = %d %v, want 0", n, err)
	}
	if n, _ := st.GamesSince(chat, old.ID, since); n != 1 {
		t.Errorf("GamesSince(old season) = %d, want 1", n)
	}
	if rec, err := st.FastestSince(chat, se.ID, since); err != nil || rec != nil {
		t.Errorf("FastestSince(new season) = %+v %v, want nil", rec, err)
	}
}
