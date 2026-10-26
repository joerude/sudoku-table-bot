package storage

import (
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// makeDNFGame creates a completed season game where the winner finishes and
// dnfID joins but does not.
func makeDNFGame(t *testing.T, st *Store, chatID, seasonID, winnerID, dnfID int64) {
	t.Helper()
	gid, err := st.CreatePendingGame(chatID, seasonID, winnerID, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.AddPick(gid, winnerID); err != nil {
		t.Fatalf("AddPick: %v", err)
	}
	if err := st.AddDNF(gid, dnfID); err != nil {
		t.Fatalf("AddDNF: %v", err)
	}
	se, err := st.ActiveSeason(chatID)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
		t.Fatalf("FinalizeGame: %v", err)
	}
}

// TestSeasonRatingInputs: counts split season games from duels, count a DNF as
// played but not finished, and ignore other seasons, deleted games, pending
// games and removed players.
func TestSeasonRatingInputs(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-5201)
	st.EnsureChat(chat, 1)
	old, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 3, "Carol") // plays nothing this season
	d, _, _ := st.RegisterPlayer(chat, 4, "Dan")   // removed below

	// Old season: must not leak into the current one.
	makeNormalGame(t, st, chat, old.ID, a.ID, b.ID)
	makeDuelGame(t, st, chat, old.ID, a.ID, a.ID, b.ID, b.ID)
	cur, err := st.CloseSeason(old, a.ID)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}

	// Current season games: Alice wins two, Bob wins one, Bob DNFs one.
	makeNormalGame(t, st, chat, cur.ID, a.ID, b.ID)
	makeNormalGame(t, st, chat, cur.ID, a.ID, b.ID)
	makeNormalGame(t, st, chat, cur.ID, b.ID, a.ID)
	makeDNFGame(t, st, chat, cur.ID, a.ID, b.ID)
	// Duels: Alice beats Bob, then Bob beats Alice who DNFs.
	makeDuelGame(t, st, chat, cur.ID, a.ID, a.ID, b.ID, b.ID)
	makeDNFDuelGame(t, st, chat, cur.ID, b.ID, b.ID, a.ID, a.ID)
	// Not counted: a deleted game, a pending game, a removed player's game.
	gone := makeNormalGame(t, st, chat, cur.ID, b.ID, a.ID)
	if err := st.SoftDeleteGame(gone); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}
	if _, err := st.CreatePendingGame(chat, cur.ID, a.ID, "medium", "hardcore"); err != nil {
		t.Fatalf("pending: %v", err)
	}
	makeNormalGame(t, st, chat, cur.ID, d.ID, b.ID)
	if _, err := st.RemovePlayer(chat, "Dan"); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}

	got, err := st.SeasonRatingInputs(chat, cur.ID)
	if err != nil {
		t.Fatalf("SeasonRatingInputs: %v", err)
	}
	by := map[int64]domain.SeasonRatingInput{}
	for _, in := range got {
		by[in.PlayerID] = in
	}
	if len(by) != 3 {
		t.Fatalf("want Alice, Bob and Carol, got %+v", got)
	}
	// Alice: 4 season games (3 wins), 2 duels (1 win, 1 DNF), 5 finishes.
	wantA := domain.SeasonRatingInput{PlayerID: a.ID, SeasonGames: 4, SeasonWins: 3, Played: 6, Finished: 5, Duels: 2, DuelWins: 1}
	// Bob: 5 season games (1 win, 1 DNF, the last one against Dan), 2 duels (1 win).
	wantB := domain.SeasonRatingInput{PlayerID: b.ID, SeasonGames: 5, SeasonWins: 1, Played: 7, Finished: 6, Duels: 2, DuelWins: 1}
	if by[a.ID] != wantA {
		t.Errorf("Alice = %+v, want %+v", by[a.ID], wantA)
	}
	if by[b.ID] != wantB {
		t.Errorf("Bob = %+v, want %+v", by[b.ID], wantB)
	}
	if by[c.ID] != (domain.SeasonRatingInput{PlayerID: c.ID}) {
		t.Errorf("Carol = %+v, want zeros", by[c.ID])
	}
}
