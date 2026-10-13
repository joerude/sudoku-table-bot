package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func seasonAt(minGames int, deadline string) *storage.Season {
	return &storage.Season{
		Number: 5, Target: 100, MinGames: minGames,
		Deadline: sql.NullString{String: deadline, Valid: deadline != ""},
	}
}

// Below the floor the panel has to show both paces: what the league is doing
// and what it needs to do. Season 5's real numbers on 1 August.
func TestSeasonGamesLineShowsBothPaces(t *testing.T) {
	se := seasonAt(66, "2026-09-07 18:00:00") // 8 Sep 00:00 +06
	now := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)

	out := seasonGamesLine(se, 28, "2026-07-08", now)

	for _, want := range []string{"28", "66", "темп", "нужно"} {
		if !strings.Contains(out, want) {
			t.Errorf("seasonGamesLine = %q, want it to contain %q", out, want)
		}
	}
	// 28 games over 24 days ≈ 1.2/day; 38 left over 38 days = 1.0/day.
	if !strings.Contains(out, "1.2") || !strings.Contains(out, "1.0") {
		t.Errorf("seasonGamesLine = %q, want paces 1.2 and 1.0", out)
	}
}

// Once the floor is cleared the pace lines are noise: the deadline can close
// the season, so the only thing worth saying is that it's cleared.
func TestSeasonGamesLineAtFloorDropsPaces(t *testing.T) {
	se := seasonAt(66, "2026-09-07 18:00:00")
	now := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)

	out := seasonGamesLine(se, 70, "2026-07-08", now)

	if !strings.Contains(out, "70") || !strings.Contains(out, "66") {
		t.Errorf("seasonGamesLine = %q, want the 70/66 counter", out)
	}
	if strings.Contains(out, "нужно") || strings.Contains(out, "темп") {
		t.Errorf("seasonGamesLine = %q, want no pace lines once the floor is met", out)
	}
}

// A season with no deadline yet (freshly rolled) still shows the counter; only
// the required-pace half is missing.
func TestSeasonGamesLineWithoutDeadline(t *testing.T) {
	se := seasonAt(66, "")
	now := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)

	out := seasonGamesLine(se, 5, "2026-07-25", now)

	if !strings.Contains(out, "5") || !strings.Contains(out, "66") {
		t.Errorf("seasonGamesLine = %q, want the 5/66 counter", out)
	}
	if strings.Contains(out, "нужно") {
		t.Errorf("seasonGamesLine = %q, want no required pace without a deadline", out)
	}
}

// The extension post is the whole pressure mechanism: it must name the
// shortfall, the leader, and the new date.
func TestSeasonExtendedTextNamesShortfall(t *testing.T) {
	se := seasonAt(66, "2026-09-07 18:00:00")
	now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	ext := time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)
	leader := &storage.Standing{Name: "Joe Rude", Points: 71}

	out := seasonExtendedText(se, 41, leader, ext, now, time.UTC)

	for _, want := range []string{"Сезон 5", "41", "66", "25", "Joe Rude", "71"} {
		if !strings.Contains(out, want) {
			t.Errorf("seasonExtendedText = %q, want it to contain %q", out, want)
		}
	}
}

// Nobody scored at all: the post still goes out, without a leader line.
func TestSeasonExtendedTextWithoutLeader(t *testing.T) {
	se := seasonAt(66, "2026-09-07 18:00:00")
	now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	ext := time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)

	out := seasonExtendedText(se, 0, nil, ext, now, time.UTC)

	if !strings.Contains(out, "0") || !strings.Contains(out, "66") {
		t.Errorf("seasonExtendedText = %q, want the 0/66 counter", out)
	}
	if strings.Contains(out, "Лидер") {
		t.Errorf("seasonExtendedText = %q, want no leader line when nobody scored", out)
	}
}

// The result post carries the counter, that is where the tempo stays visible.
func TestResultTextCarriesGamesCounter(t *testing.T) {
	rows := []storage.ResultRow{{Rank: 1, Name: "A", Points: 3}}

	out := resultText(rows, "medium", "hardcore", nil, 100, 29, 66)

	if !strings.Contains(out, "29") || !strings.Contains(out, "66") {
		t.Errorf("resultText = %q, want the 29/66 counter", out)
	}
}
