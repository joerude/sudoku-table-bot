package bot

import (
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestRaceChartData(t *testing.T) {
	se := &storage.Season{Number: 6, Target: 100}
	// Standings come in rank order; the chart must order lines by player id.
	standings := []storage.Standing{{PlayerID: 2, Name: "Nur"}, {PlayerID: 1, Name: "mister"}, {PlayerID: 3, Name: "Joe"}}
	ranks := []storage.SeasonRank{
		{GameID: 10, PlayerID: 1, Rank: 1, Points: 3},
		{GameID: 10, PlayerID: 2, Rank: 2, Points: 1},
		{GameID: 11, PlayerID: 2, Rank: 1, Points: 3}, // Joe and mister missing: no points
		{GameID: 12, PlayerID: 3, Rank: 1, Points: 3},
		{GameID: 12, PlayerID: 1, Rank: 0, Points: 0},
	}
	race := raceChartData(se, standings, ranks)
	if race.Target != 100 || race.Title != "Сезон 6 · гонка до 100" {
		t.Errorf("title/target: %+v", race)
	}
	want := map[string][]int{"mister": {3, 3, 3}, "Nur": {1, 4, 4}, "Joe": {0, 0, 3}}
	order := []string{"mister", "Nur", "Joe"}
	if len(race.Series) != 3 {
		t.Fatalf("want 3 series, got %d", len(race.Series))
	}
	for i, s := range race.Series {
		if s.Name != order[i] {
			t.Errorf("series %d = %s, want %s (by player id)", i, s.Name, order[i])
		}
		for g, p := range want[s.Name] {
			if s.Points[g] != p {
				t.Errorf("%s after game %d = %d, want %d", s.Name, g+1, s.Points[g], p)
			}
		}
	}
}
