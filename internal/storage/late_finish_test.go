package storage

import "testing"

// TestCreditLateFinish: a non-finisher who solved the board after the record
// becomes the next place with points and time; a finisher, a second try and a
// duel stay as they are.
func TestCreditLateFinish(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4401)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol")

	g, err := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.AddPickTimed(g, a.ID, 400)
	_ = st.AddDNF(g, b.ID)
	_ = st.AddDNF(g, c.ID)
	if err := st.FinalizeGame(g, se.PointsTable); err != nil {
		t.Fatal(err)
	}
	var doneAt string
	_ = st.db.QueryRow(`SELECT completed_at FROM games WHERE id=?`, g).Scan(&doneAt)

	if ok, err := st.CreditLateFinish(g, b.ID, 684, se.PointsTable); err != nil || !ok {
		t.Fatalf("credit Bob: %v, %v", ok, err)
	}
	if ok, _ := st.CreditLateFinish(g, b.ID, 700, se.PointsTable); ok {
		t.Error("Bob is a finisher now, a second credit must do nothing")
	}
	if ok, _ := st.CreditLateFinish(g, a.ID, 700, se.PointsTable); ok {
		t.Error("the winner is no non-finisher")
	}
	rows, _ := st.GameResults(g)
	want := []struct {
		id         int64
		rank, secs int
	}{{a.ID, 1, 400}, {b.ID, 2, 684}, {c.ID, 0, 0}}
	for i, w := range want {
		r := rows[i]
		if r.PlayerID != w.id || r.Rank != w.rank || r.Duration != w.secs {
			t.Errorf("row %d = %+v, want player %d rank %d time %d", i, r, w.id, w.rank, w.secs)
		}
		if pts := se.PointsTable; w.rank > 0 && w.rank <= len(pts) && r.Points != pts[w.rank-1] {
			t.Errorf("row %d points %d, want %d", i, r.Points, pts[w.rank-1])
		}
	}
	var after string
	_ = st.db.QueryRow(`SELECT completed_at FROM games WHERE id=?`, g).Scan(&after)
	if after != doneAt {
		t.Errorf("completed_at moved: %s → %s", doneAt, after)
	}

	duel := makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	if ok, _ := st.CreditLateFinish(duel, b.ID, 500, se.PointsTable); ok {
		t.Error("a duel is not credited")
	}
}
