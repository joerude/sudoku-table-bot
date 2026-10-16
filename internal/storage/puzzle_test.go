package storage

import (
	"strings"
	"testing"
)

func TestPuzzleIsStoredOnce(t *testing.T) {
	st := openTemp(t)
	_, gid := newCardGame(t, st)

	if p, err := st.Puzzle(gid); err != nil || p != "" {
		t.Fatalf("fresh game: puzzle %q, err %v", p, err)
	}
	first := strings.Repeat("1", 81)
	if err := st.SetPuzzle(gid, first); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPuzzle(gid, strings.Repeat("2", 81)); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.Puzzle(gid); p != first {
		t.Errorf("puzzle = %q, want the first one kept", p)
	}
	if p, err := st.Puzzle(99999); err != nil || p != "" {
		t.Errorf("missing game: %q, %v", p, err)
	}
}

func TestBoardGameFindsLatestWithPuzzle(t *testing.T) {
	st := openTemp(t)
	chat, old := newCardGame(t, st)
	g, _ := st.GameByID(old)
	newer, err := st.CreatePendingGame(chat, g.SeasonID, 1, "easy", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	noGrid, err := st.CreatePendingGame(chat, g.SeasonID, 1, "easy", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	grid := strings.Repeat("0", 81)
	for id, code := range map[int64]string{old: "QWER", newer: "ZXCV", noGrid: "QWER"} {
		if err := st.SetUsdokuCode(id, code); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{old, newer} {
		if err := st.SetPuzzle(id, grid); err != nil {
			t.Fatal(err)
		}
	}

	if got, _ := st.BoardGame(chat, ""); got == nil || got.ID != newer {
		t.Errorf("no code: got %+v, want the latest game with a grid (%d)", got, newer)
	}
	if got, _ := st.BoardGame(chat, "qwer"); got == nil || got.ID != old {
		t.Errorf("code qwer: got %+v, want %d (the newer QWER has no grid)", got, old)
	}
	if got, _ := st.BoardGame(chat, "NONE"); got != nil {
		t.Errorf("unknown code: got %+v", got)
	}
	if err := st.SoftDeleteGame(old); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.BoardGame(chat, "QWER"); got != nil {
		t.Errorf("deleted game must not be found: %+v", got)
	}
}
