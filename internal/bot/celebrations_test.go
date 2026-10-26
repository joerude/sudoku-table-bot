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

func TestUniqueDuelist(t *testing.T) {
	rec := func(id int64, name string, wins, losses int) storage.DuelStanding {
		return storage.DuelStanding{PlayerID: id, Name: name, Wins: wins, Losses: losses}
	}
	cases := []struct {
		name       string
		records    []storage.DuelStanding
		wantName   string
		wantID     int64
		wantWins   int
		wantLosses int
		wantOK     bool
	}{
		{"unique leader", []storage.DuelStanding{rec(1, "Nur", 4, 1), rec(2, "Joe", 2, 3)}, "Nur", 1, 4, 1, true},
		{"leader listed last", []storage.DuelStanding{rec(2, "Joe", 2, 3), rec(1, "Nur", 4, 1)}, "Nur", 1, 4, 1, true},
		{"exactly the minimum", []storage.DuelStanding{rec(1, "Nur", duelistMinWins, 0)}, "Nur", 1, duelistMinWins, 0, true},
		{"below the minimum", []storage.DuelStanding{rec(1, "Nur", duelistMinWins-1, 0)}, "", 0, 0, 0, false},
		{"tie at top", []storage.DuelStanding{rec(1, "Nur", 3, 1), rec(2, "Joe", 3, 2), rec(3, "mis", 1, 0)}, "", 0, 0, 0, false},
		{"tie below the leader", []storage.DuelStanding{rec(1, "Nur", 5, 0), rec(2, "Joe", 3, 2), rec(3, "mis", 3, 1)}, "Nur", 1, 5, 0, true},
		{"unique max after earlier tie", []storage.DuelStanding{rec(1, "A", 3, 0), rec(2, "B", 3, 0), rec(3, "C", 4, 0)}, "C", 3, 4, 0, true},
		{"nobody won", []storage.DuelStanding{rec(1, "Nur", 0, 2), rec(2, "Joe", 0, 1)}, "", 0, 0, 0, false},
		{"empty", nil, "", 0, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, id, wins, losses, ok := uniqueDuelist(c.records)
			if name != c.wantName || id != c.wantID || wins != c.wantWins || losses != c.wantLosses || ok != c.wantOK {
				t.Errorf("uniqueDuelist = (%q, %d, %d, %d, %v); want (%q, %d, %d, %d, %v)",
					name, id, wins, losses, ok, c.wantName, c.wantID, c.wantWins, c.wantLosses, c.wantOK)
			}
		})
	}
}
