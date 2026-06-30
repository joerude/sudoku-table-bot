package storage

import "testing"

func TestGamesForRating(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-100)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	cp, _, _ := st.RegisterPlayer(chat, 3, "Carol")

	// Game 1: Alice 1st, Bob 2nd, Carol DNF.
	g1, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err := st.AddPick(g1, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPick(g1, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDNF(g1, cp.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.FinalizeGame(g1, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	// Game 2: Bob 1st, Alice 2nd.
	g2, _ := st.CreatePendingGame(chat, se.ID, 1, "easy", "hardcore")
	_ = st.AddPick(g2, b.ID)
	_ = st.AddPick(g2, a.ID)
	if err := st.FinalizeGame(g2, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	games, err := st.GamesForRating(chat)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("got %d games want 2", len(games))
	}
	if games[0].ID != g1 || games[1].ID != g2 {
		t.Errorf("chronological order broken: %d,%d want %d,%d",
			games[0].ID, games[1].ID, g1, g2)
	}
	if len(games[0].Participants) != 3 {
		t.Errorf("game1 participants=%d want 3", len(games[0].Participants))
	}
	// Carol must appear as DNF (rank 0) in game1.
	var sawDNF bool
	for _, p := range games[0].Participants {
		if p.PlayerID == cp.ID && p.Rank == 0 {
			sawDNF = true
		}
	}
	if !sawDNF {
		t.Errorf("Carol DNF (rank 0) missing from game1: %+v", games[0].Participants)
	}
}
