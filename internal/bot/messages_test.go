package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func season1() *storage.Season {
	return &storage.Season{ID: 1, Number: 1, Target: 30}
}

func TestMeTextWithTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 10, Wins: 3, Points: 12, Rank: 2}
	sp := &storage.SpeedStat{AvgSecs: 270, BestSecs: 192, Games: 7}
	out := meText("Alice", st, sp, season1())
	for _, want := range []string{"4:30", "3:12", "по 7", "⚡ Лучшее"} {
		if !strings.Contains(out, want) {
			t.Errorf("meText with timed games: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestMeTextNoTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 1, Points: 6, Rank: 3}
	sp := &storage.SpeedStat{} // Games == 0
	out := meText("Bob", st, sp, season1())
	if !strings.Contains(out, "Ср. время: —") {
		t.Errorf("meText no timed games: want 'Ср. время: —' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Лучшее") {
		t.Errorf("meText no timed games: should NOT contain 'Лучшее'\ngot: %s", out)
	}
}

func TestMeTextZeroGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 0}
	out := meText("Carol", st, nil, season1())
	if !strings.Contains(out, "пока нет игр") {
		t.Errorf("meText zero games: want 'пока нет игр' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Ср. время") {
		t.Errorf("meText zero games: should NOT contain 'Ср. время'\ngot: %s", out)
	}
}

func TestSpeedTextRankedAndFewer(t *testing.T) {
	ranked := []storage.SpeedRow{
		{Name: "Bob", AvgSecs: 228, BestSecs: 181, Games: 5},
		{Name: "Alice", AvgSecs: 270, BestSecs: 192, Games: 7},
	}
	fewer := []storage.SpeedRow{
		{Name: "Carol", AvgSecs: 200, BestSecs: 200, Games: 2},
	}
	out := speedText(season1(), "medium", ranked, fewer, 3)
	checks := []string{"🥇", "3:48", "4:30", "Мало игр (&lt;3): Carol"}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("speedText ranked+fewer: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestSpeedTextEmptyRanked(t *testing.T) {
	out := speedText(season1(), "hard", nil, nil, 3)
	for _, want := range []string{"мало данных", "Hard"} {
		if !strings.Contains(out, want) {
			t.Errorf("speedText empty: want %q in output\ngot: %s", want, out)
		}
	}
}
