package domain

import "testing"

func TestDuelStreaks(t *testing.T) {
	const (
		A = int64(1)
		B = int64(2)
	)

	t.Run("empty", func(t *testing.T) {
		if got := DuelStreaks(nil); len(got) != 0 {
			t.Errorf("empty: want no entries, got %v", got)
		}
	})

	t.Run("single win tracks both players", func(t *testing.T) {
		got := DuelStreaks([][2]int64{{A, B}})
		if got[A] != (Streak{Current: 1, Best: 1}) {
			t.Errorf("A: want {1,1}, got %+v", got[A])
		}
		if got[B] != (Streak{Current: 0, Best: 0}) {
			t.Errorf("B (only loser): want {0,0}, got %+v", got[B])
		}
	})

	t.Run("loss resets current, best retained", func(t *testing.T) {
		// chronological: A wins, A wins, B wins.
		got := DuelStreaks([][2]int64{{A, B}, {A, B}, {B, A}})
		if got[A] != (Streak{Current: 0, Best: 2}) {
			t.Errorf("A won 2 then lost: want {0,2}, got %+v", got[A])
		}
		if got[B] != (Streak{Current: 1, Best: 1}) {
			t.Errorf("B lost 2 then won: want {1,1}, got %+v", got[B])
		}
	})

	t.Run("best greater than current", func(t *testing.T) {
		// A: win, win, loss, win → current 1, best 2.
		got := DuelStreaks([][2]int64{{A, B}, {A, B}, {B, A}, {A, B}})
		if got[A] != (Streak{Current: 1, Best: 2}) {
			t.Errorf("A: want {1,2}, got %+v", got[A])
		}
		if got[B] != (Streak{Current: 0, Best: 1}) {
			t.Errorf("B: want {0,1}, got %+v", got[B])
		}
	})
}
