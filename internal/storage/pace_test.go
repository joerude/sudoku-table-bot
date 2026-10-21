package storage

import "testing"

func TestLagsBefore(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7301)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")

	type pick struct {
		id   int64
		secs int64 // 0 = DNF
	}
	untimed := int64(-1) // finished, no solve time (manual record)
	play := func(diff string, picks ...pick) int64 {
		t.Helper()
		gid, err := st.CreatePendingGame(chat, se.ID, 1, diff, "hardcore")
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range picks {
			switch p.secs {
			case 0:
				st.AddDNF(gid, p.id)
			case untimed:
				st.AddPick(gid, p.id)
			default:
				st.AddPickTimed(gid, p.id, p.secs)
			}
		}
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatal(err)
		}
		return gid
	}
	play("medium", pick{a.ID, 300}, pick{b.ID, 360})     // lag sample (360, 300)
	play("easy", pick{a.ID, 100}, pick{b.ID, 130})       // other difficulty
	play("medium", pick{b.ID, 280}, pick{a.ID, 290})     // B won: no lag
	play("medium", pick{a.ID, 300}, pick{b.ID, 0})       // B did not finish
	play("medium", pick{a.ID, untimed}, pick{b.ID, 340}) // the winner has no time
	play("medium", pick{a.ID, 300}, pick{b.ID, 290})     // hand order against the times
	duel := play("medium", pick{a.ID, 310}, pick{b.ID, 330})
	if err := st.SetDuelTarget(duel, b.ID); err != nil {
		t.Fatal(err)
	}
	cur := play("medium", pick{a.ID, 300}, pick{b.ID, 320}) // the game being judged
	play("medium", pick{a.ID, 300}, pick{b.ID, 900})        // later game

	got, err := st.LagsBefore(chat, b.ID, cur, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	// Best is the winner's time, not the fastest one; Lag() later drops the
	// contradicting sample.
	want := []LagSample{{Secs: 330, Best: 310}, {Secs: 290, Best: 300}, {Secs: 360, Best: 300}}
	if len(got) != len(want) {
		t.Fatalf("LagsBefore = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample %d = %+v, want %+v (newest first, duels count)", i, got[i], want[i])
		}
	}
}
