package bot

import (
	"strings"
	"testing"
	"time"

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

	text := lateFinishText("Bob <b>", 860, 0, 0)
	if !strings.Contains(text, "<b>Bob &lt;b&gt;</b>: доска решена за 14:20, уже после записи") {
		t.Errorf("text = %q", text)
	}
	if text := lateFinishText("Bob", 0, 0, 0); !strings.Contains(text, "доска решена, уже после записи") {
		t.Errorf("no solve time: %q", text)
	}
	if !strings.Contains(text, "Очки и места не меняются") {
		t.Errorf("not credited: %q", text)
	}
	credited := lateFinishText("Bob", 684, 2, 1)
	if !strings.Contains(credited, "доска решена за 11:24, уже после записи") ||
		!strings.Contains(credited, "Засчитано: 2-е место · +1") {
		t.Errorf("credited = %q", credited)
	}
	if strings.ContainsAny(text+credited, "—–!") {
		t.Errorf("no long dashes or exclamation marks in bot texts: %q", text)
	}
}

func TestBoardOpen(t *testing.T) {
	const recorded = "2026-11-24 15:40:00"
	at := time.Date(2026, 11, 24, 15, 40, 0, 0, time.UTC)
	cases := []struct {
		name      string
		now       time.Time
		hasDNF    bool
		countable bool
		open      bool
		qualifies bool
	}{
		{"inside the window", at.Add(5 * time.Minute), true, true, true, true},
		{"after the window", at.Add(lateFinishWatch), true, true, false, true},
		{"nobody left a non-finisher", at.Add(time.Minute), false, true, false, false},
		{"a duel or an archived season", at.Add(time.Minute), true, false, false, false},
	}
	for _, c := range cases {
		until, open := boardOpen(c.now, recorded, c.hasDNF, c.countable)
		if open != c.open || !until.IsZero() != c.qualifies {
			t.Errorf("%s: open=%v until=%v, want open=%v qualifies=%v", c.name, open, until, c.open, c.qualifies)
		}
		if c.qualifies && !until.Equal(at.Add(lateFinishWatch)) {
			t.Errorf("%s: until = %v, want %v", c.name, until, at.Add(lateFinishWatch))
		}
	}
	if until, open := boardOpen(at, "", true, true); open || !until.IsZero() {
		t.Errorf("no completion stamp: open=%v until=%v", open, until)
	}
}
