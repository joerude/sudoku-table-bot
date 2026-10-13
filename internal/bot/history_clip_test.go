package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// TestHistoryTextClipsLongLists: a list that would blow Telegram's 4096 cap is
// cut from the oldest end with an "…и ещё N" tail instead of failing to send.
func TestHistoryTextClipsLongLists(t *testing.T) {
	games := make([]storage.HistoryGame, 120)
	for i := range games {
		games[i] = storage.HistoryGame{
			ID: int64(i + 1), SeasonNumber: 3, CompletedAt: "2026-07-20 12:00:00",
			Difficulty: "medium", Order: []string{"Alice", "Bob", "Carol"},
		}
	}
	got := historyText(games, "UTC")
	if tgLen(got) > tgMaxText {
		t.Fatalf("history text is %d units, over the %d cap", tgLen(got), tgMaxText)
	}
	if !strings.Contains(got, "…и ещё ") {
		t.Errorf("clipped history must end with a remainder note:\n%s", got[len(got)-80:])
	}
	if !strings.HasPrefix(got, "📜 <b>Последние игры</b>") || !strings.Contains(got, "Сезон 3") {
		t.Errorf("head and season header must survive: %q", got[:60])
	}

	short := historyText(games[:3], "UTC")
	if strings.Contains(short, "…и ещё") {
		t.Errorf("short list must not be clipped: %q", short)
	}
}
