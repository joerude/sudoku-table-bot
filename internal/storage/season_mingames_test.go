package storage

import (
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// SeasonGameCount is the number the calendar exit is guarded on, so it must
// count exactly what Standings scores: completed, not deleted, not duels.
func TestSeasonGameCountExcludesDuelsAndDeleted(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	alice, _, err := st.RegisterPlayer(chat, 1, "Alice")
	if err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}
	bob, _, err := st.RegisterPlayer(chat, 2, "Bob")
	if err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}

	finish := func(gid int64) {
		t.Helper()
		if err := st.AddPick(gid, alice.ID); err != nil {
			t.Fatalf("AddPick: %v", err)
		}
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatalf("FinalizeGame: %v", err)
		}
	}

	// Two seasonal games — the only ones that should count.
	for i := 0; i < 2; i++ {
		gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		if err != nil {
			t.Fatalf("CreatePendingGame: %v", err)
		}
		finish(gid)
	}
	// A duel: excluded from the season, so excluded from the floor.
	duel, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame duel: %v", err)
	}
	if err := st.SetDuelTarget(duel, bob.ID); err != nil {
		t.Fatalf("SetDuelTarget: %v", err)
	}
	finish(duel)
	// A soft-deleted seasonal game: doesn't count either.
	gone, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	finish(gone)
	if err := st.SoftDeleteGame(gone); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}

	got, err := st.SeasonGameCount(chat, se.ID)
	if err != nil {
		t.Fatalf("SeasonGameCount: %v", err)
	}
	if got != 2 {
		t.Errorf("SeasonGameCount = %d, want 2 (duel and deleted game excluded)", got)
	}
}

// A tuned games floor must survive the season roll, exactly as target does —
// otherwise every new season silently reverts to the default.
func TestCloseSeasonInheritsMinGames(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if se.MinGames != domain.DefaultMinGames {
		t.Fatalf("fresh season MinGames = %d, want %d", se.MinGames, domain.DefaultMinGames)
	}
	if err := st.UpdateSeasonMinGames(se.ID, 40); err != nil {
		t.Fatalf("UpdateSeasonMinGames: %v", err)
	}
	se, err = st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason reread: %v", err)
	}
	alice, _, err := st.RegisterPlayer(chat, 1, "Alice")
	if err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}

	next, err := st.CloseSeason(se, alice.ID)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}
	if next.MinGames != 40 {
		t.Errorf("next season MinGames = %d, want 40 inherited", next.MinGames)
	}
}
