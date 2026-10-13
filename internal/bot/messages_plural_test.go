package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestSeasonDeadlineLineTomorrowAndPlural(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		Number:   5,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true}, // last day 31 July
	}
	cases := []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 7, 30, 23, 0, 0, 0, loc), "(завтра)"},
		{time.Date(2026, 7, 29, 9, 0, 0, 0, loc), "(через 2 дня)"},
		{time.Date(2026, 7, 13, 9, 0, 0, 0, loc), "(через 18 дней)"},
		{time.Date(2026, 7, 10, 9, 0, 0, 0, loc), "(через 21 день)"},
	}
	for _, tc := range cases {
		if got := seasonDeadlineLine(se, tc.now, loc); !strings.Contains(got, tc.want) {
			t.Errorf("now=%s: line = %q, want %q", tc.now, got, tc.want)
		}
	}
}

func TestStandingsTextPlurals(t *testing.T) {
	se := &storage.Season{Number: 3, Target: 101}
	rows := []storage.Standing{
		{Name: "A", Points: 5, Wins: 1, Games: 1},
		{Name: "B", Points: 3, Wins: 0, Games: 22},
	}
	got := standingsText(se, rows)
	for _, want := range []string{"до 101 очко", "1 игра)", "22 игры)"} {
		if !strings.Contains(got, want) {
			t.Errorf("standingsText missing %q in:\n%s", want, got)
		}
	}
}
