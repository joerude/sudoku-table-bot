package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// paceFixture plays five games where Bob trails Alice by 30%, then a pending
// game where Alice solves in aliceSecs and Bob in bobSecs.
func paceFixture(t *testing.T, chat, aliceSecs, bobSecs int64) (*Bot, *storage.Store, *storage.Game) {
	t.Helper()
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	bob, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	game := func(secsA, secsB int64) int64 {
		gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		if secsA == 0 {
			st.AddPick(gid, a.ID) // manual record, no time for the winner
		} else {
			st.AddPickTimed(gid, a.ID, secsA)
		}
		st.AddPickTimed(gid, bob.ID, secsB)
		return gid
	}
	for i := 0; i < 5; i++ {
		if err := st.FinalizeGame(game(200, 260), se.PointsTable); err != nil {
			t.Fatal(err)
		}
	}
	g, _ := st.GameByID(game(aliceSecs, bobSecs))
	return &Bot{st: st}, st, g
}

func TestPaceLineInResult(t *testing.T) {
	b, st, g := paceFixture(t, -7302, 300, 320)
	result, _, err := b.scoreAndCheck(g)
	if err != nil {
		t.Fatal(err)
	}
	want := "🏃 <b>Bob</b>: 20 с от победителя, обычно около 1:30"
	if !strings.Contains(result, want) {
		t.Errorf("result misses the pace line %q:\n%s", want, result)
	}
	if strings.Contains(result, "🏃 <b>Alice</b>") {
		t.Errorf("the winner has the win spotlight, no pace line")
	}
	if n, _ := st.CountEvents(g.ChatID, evPaceShown); n != 1 {
		t.Errorf("pace_shown events = %d, want 1", n)
	}
}

func TestPaceLineNotNotable(t *testing.T) {
	b, _, g := paceFixture(t, -7303, 300, 385) // 28% behind: the usual lag
	result, _, _ := b.scoreAndCheck(g)
	if strings.Contains(result, "🏃") {
		t.Errorf("an ordinary lag must stay quiet:\n%s", result)
	}
}

func TestPaceLineOff(t *testing.T) {
	b, st, g := paceFixture(t, -7304, 300, 320)
	if err := st.SetFeature(g.ChatID, featPace, false); err != nil {
		t.Fatal(err)
	}
	result, _, _ := b.scoreAndCheck(g)
	if strings.Contains(result, "🏃") {
		t.Errorf("switched off, still shown:\n%s", result)
	}
}

func TestPaceNeedsTheWinnersTime(t *testing.T) {
	b, _, g := paceFixture(t, -7306, 0, 320)
	result, _, _ := b.scoreAndCheck(g)
	if strings.Contains(result, "🏃") {
		t.Errorf("without the winner's time there is no lag to show:\n%s", result)
	}
}

func TestPaceYieldsToRecord(t *testing.T) {
	// Bob beats his best (260) while still well under his usual lag: the
	// record line is enough, no pace line on top of it.
	b, _, g := paceFixture(t, -7305, 230, 250)
	result, _, _ := b.scoreAndCheck(g)
	if !strings.Contains(result, "🚀 <b>Bob</b>") || strings.Contains(result, "🏃 <b>Bob</b>") {
		t.Errorf("want the record line instead of the pace line:\n%s", result)
	}
}

func TestPaceText(t *testing.T) {
	if got := paceText("<i>Bo</i>", 0, 75); got != "🏃 <b>&lt;i&gt;Bo&lt;/i&gt;</b>: вровень с победителем, обычно около 1:15" {
		t.Errorf("paceText = %q", got)
	}
	if got := paceText("Bob", 61, 130); strings.ContainsAny(got, "—–") || !strings.Contains(got, "1:01 от победителя") {
		t.Errorf("paceText = %q", got)
	}
}
