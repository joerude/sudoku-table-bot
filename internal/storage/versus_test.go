package storage

import "testing"

func TestVersus(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-9300)
	st.EnsureChat(chat, 1)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol")
	se, _ := st.ActiveSeason(chat)

	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	makeNormalGame(t, st, chat, se.ID, b.ID, a.ID)
	makeDNFGame(t, st, chat, se.ID, b.ID, a.ID)               // Alice's DNF loses to Bob
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)  // duel: Alice wins
	deleted := makeNormalGame(t, st, chat, se.ID, a.ID, b.ID) // deleted: not counted
	st.SoftDeleteGame(deleted)
	makeNormalGame(t, st, chat, se.ID, a.ID, c.ID)
	if _, err := st.RemovePlayer(chat, "Carol"); err != nil { // inactive: not listed
		t.Fatal(err)
	}

	got, err := st.Versus(chat, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := VersusRow{OpponentID: b.ID, OpponentName: "Bob", GameWins: 2, GameLosses: 2, DuelWins: 1}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Versus = %+v, want [%+v]", got, want)
	}

	if _, err := st.CloseSeason(se, a.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.SeasonsWon(chat, a.ID); n != 1 {
		t.Errorf("SeasonsWon(Alice) = %d, want 1", n)
	}
	if n, _ := st.SeasonsWon(chat, b.ID); n != 0 {
		t.Errorf("SeasonsWon(Bob) = %d, want 0", n)
	}
}
