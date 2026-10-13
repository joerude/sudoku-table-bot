package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestLastPlayedPhraseUsesLocalCalendarDay(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	// 21 July 09:00 local.
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, loc)
	cases := []struct {
		name, last, want string
	}{
		// 18:00 UTC on the 20th is 00:00 local on the 21st: today, though under 24h either way.
		{"after local midnight", "2026-07-20 18:00:00", "играл сегодня"},
		// 17:00 UTC on the 20th is 23:00 local on the 20th: yesterday, only 10h ago.
		{"before local midnight", "2026-07-20 17:00:00", "играл вчера"},
		{"three days", "2026-07-18 10:00:00", "не играл 3 дня"},
		{"eleven days", "2026-07-10 10:00:00", "не играл 11 дней"},
		{"never", "", "ещё не играл"},
	}
	for _, tc := range cases {
		if got := lastPlayedPhrase(tc.last, now, loc); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestActivityTextLeadsWithRecentWindow(t *testing.T) {
	rows := []storage.InitiativeRow{
		{Name: "Bob", Games30: 1, Duels30: 0, GamesAll: 1, LastPlayed: "2026-07-20 08:00:00"},
		{Name: "Alice", GamesAll: 21, DuelsAll: 2, LastPlayed: "2026-07-10 08:00:00"},
	}
	got := activityText(rows, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC), time.UTC)
	for _, want := range []string{
		"📣", "<b>1</b> игра · <b>0</b> дуэлей <i>за 30 дн</i>",
		"всего: 21 · 2", "/result",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Считаются только") {
		t.Errorf("the old footnote must be gone:\n%s", got)
	}
}
