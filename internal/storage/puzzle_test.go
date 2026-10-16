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
