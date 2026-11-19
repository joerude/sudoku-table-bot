package storage

import "testing"

// Duels are numbered all-time and per season; seasonal games, deleted duels and
// other chats do not take a number.
func TestDuelNumber(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7101)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	a, _, err := st.RegisterPlayer(chat, 1, "Alice")
	if err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}
	b, _, err := st.RegisterPlayer(chat, 2, "Bob")
	if err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}

	d1 := makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	gone := makeDuelGame(t, st, chat, se.ID, a.ID, b.ID, a.ID, b.ID)
	if err := st.SoftDeleteGame(gone); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}
	next, err := st.CloseSeason(se, a.ID)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}
	d2 := makeDuelGame(t, st, chat, next.ID, a.ID, b.ID, a.ID, b.ID)

	for _, tc := range []struct {
		name         string
		season, game int64
		all, inSe    int
	}{
		{"first duel", se.ID, d1, 1, 1},
		{"next season starts at 1", next.ID, d2, 2, 1},
		{"deleted duel has no number", se.ID, gone, 0, 0},
	} {
		all, inSe, err := st.DuelNumber(chat, tc.season, tc.game)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if all != tc.all || inSe != tc.inSe {
			t.Errorf("%s: got %d/%d, want %d/%d", tc.name, all, inSe, tc.all, tc.inSe)
		}
	}
}
