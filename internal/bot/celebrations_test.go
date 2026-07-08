package bot

import (
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestUniqueTopWins(t *testing.T) {
	st := func(name string, wins int) storage.Standing {
		return storage.Standing{Name: name, Wins: wins}
	}
	cases := []struct {
		name      string
		standings []storage.Standing
		wantName  string
		wantWins  int
		wantOK    bool
	}{
		{"unique leader", []storage.Standing{st("Nur", 28), st("Joe", 26), st("mis", 15)}, "Nur", 28, true},
		{"tie at top", []storage.Standing{st("Nur", 28), st("Joe", 28), st("mis", 15)}, "", 0, false},
		{"all equal", []storage.Standing{st("Nur", 23), st("Joe", 23), st("mis", 23)}, "", 0, false},
		{"all zero", []storage.Standing{st("Nur", 0), st("Joe", 0)}, "", 0, false},
		{"single player", []storage.Standing{st("Nur", 5)}, "Nur", 5, true},
		{"empty", nil, "", 0, false},
		{"unique max after earlier tie", []storage.Standing{st("A", 5), st("B", 5), st("C", 7)}, "C", 7, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, wins, ok := uniqueTopWins(c.standings)
			if ok != c.wantOK || (ok && (name != c.wantName || wins != c.wantWins)) {
				t.Errorf("uniqueTopWins = (%q, %d, %v); want (%q, %d, %v)",
					name, wins, ok, c.wantName, c.wantWins, c.wantOK)
			}
		})
	}
}
