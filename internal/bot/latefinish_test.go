package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func TestLateFinishes(t *testing.T) {
	done := func(at int64) *int64 { return &at }
	info := &usdoku.GameInfo{
		Info: usdoku.Info{StartedAt: 1000},
		Players: []usdoku.Player{
			{Name: "winner", CompletedAt: done(1400)}, // recorded as a finisher: not watched
			{Name: "slow", CompletedAt: done(1860)},   // the board got solved after the record
			{Name: "gone"},                            // still not finished
		},
	}
	got := lateFinishes(info, map[string]int64{"slow": 7, "gone": 8})
	if len(got) != 1 || got[0].Nick != "slow" || got[0].PlayerID != 7 || got[0].Secs != 860 {
		t.Fatalf("lateFinishes = %+v, want only slow (player 7) at 860 s", got)
	}

	text := lateFinishText("Bob <b>", 860)
	if !strings.Contains(text, "<b>Bob &lt;b&gt;</b>: доска решена за 14:20, уже после записи") {
		t.Errorf("text = %q", text)
	}
	if text := lateFinishText("Bob", 0); !strings.Contains(text, "доска решена, уже после записи") {
		t.Errorf("no solve time: %q", text)
	}
	if strings.ContainsAny(text, "—–!") {
		t.Errorf("no long dashes or exclamation marks in bot texts: %q", text)
	}
}
