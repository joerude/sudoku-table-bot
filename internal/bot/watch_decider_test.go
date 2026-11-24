package bot

import (
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// t0 is an arbitrary fixed base time for deterministic decider tests.
var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestDecider_AllFinishedRecordsImmediately(t *testing.T) {
	var s watchState
	if !s.shouldRecord(t0, 3, 3, false, 0) {
		t.Fatal("all joined players finished → should record at once")
	}
}

func TestDecider_NobodyFinishedNotOverKeepsPolling(t *testing.T) {
	var s watchState
	for i := 0; i < 5; i++ {
		if s.shouldRecord(t0.Add(time.Duration(i)*watchInterval), 0, 3, false, 0) {
			t.Fatalf("poll %d: nobody finished and round not over → must not record", i)
		}
	}
}

func TestDecider_DrainsUntilQuietThenRecords(t *testing.T) {
	var s watchState
	// First finisher (1 of 3) starts the drain; do not record yet.
	if s.shouldRecord(t0, 1, 3, true, 0) {
		t.Fatal("first finisher should start drain, not record")
	}
	// Quiet but below settleQuiet → keep waiting.
	if s.shouldRecord(t0.Add(settleQuiet-time.Minute), 1, 3, true, 0) {
		t.Fatal("still within settle window → must not record")
	}
	// Quiet for the full settle window → record.
	if !s.shouldRecord(t0.Add(settleQuiet), 1, 3, true, 0) {
		t.Fatal("no new finisher for settleQuiet → should record")
	}
}

func TestDecider_NewFinisherResetsSettleTimer(t *testing.T) {
	var s watchState
	// First finisher starts drain.
	s.shouldRecord(t0, 1, 3, true, 0)
	// 5 min later (under settleQuiet=6m) a 2nd finisher arrives — resets the timer.
	if s.shouldRecord(t0.Add(5*time.Minute), 2, 3, true, 0) {
		t.Fatal("new finisher arrived → must not record yet")
	}
	// 5 min after the 2nd finisher (10m since first) — still within the reset window.
	if s.shouldRecord(t0.Add(10*time.Minute), 2, 3, true, 0) {
		t.Fatal("timer reset on 2nd finisher → still waiting at +5m")
	}
	// 6 min of quiet after the 2nd finisher → record.
	if !s.shouldRecord(t0.Add(5*time.Minute+settleQuiet), 2, 3, true, 0) {
		t.Fatal("quiet for settleQuiet after last finisher → should record")
	}
}

func TestDecider_HardCapStopsEndlessTrickle(t *testing.T) {
	var s watchState
	// A finisher arrives on every poll so the quiet timer never elapses; the
	// hard cap must still force a record.
	recorded := false
	for i := 0; ; i++ {
		now := t0.Add(time.Duration(i) * watchInterval)
		if now.Sub(t0) > drainMax+watchInterval {
			break
		}
		if s.shouldRecord(now, i+1, 99, true, 0) {
			recorded = true
			if now.Sub(t0) < drainMax {
				t.Fatalf("recorded too early at %s (< drainMax)", now.Sub(t0))
			}
			break
		}
	}
	if !recorded {
		t.Fatal("hard cap drainMax should force a record despite endless trickle")
	}
}

func TestDecider_RoundOverNoFinishersStillSettles(t *testing.T) {
	var s watchState
	// Superseded/terminal with zero finishers (all DNF or unknown nicks).
	if s.shouldRecord(t0, 0, 3, true, 0) {
		t.Fatal("over with 0 finishers should start drain, not record")
	}
	if !s.shouldRecord(t0.Add(settleQuiet), 0, 3, true, 0) {
		t.Fatal("over with 0 finishers → record after settle so caller can fall back to manual")
	}
}

func TestRoundStarted(t *testing.T) {
	cases := []struct {
		prev, cur string
		want      bool
	}{
		{"pending", "running", true},
		{"", "running", false}, // first poll after a restart: already live
		{"running", "running", false},
		{"pending", "pending", false},
		{"running", "finished", false},
	}
	for _, c := range cases {
		if got := roundStarted(c.prev, c.cur); got != c.want {
			t.Errorf("roundStarted(%q, %q) = %v, want %v", c.prev, c.cur, got, c.want)
		}
	}
}

func TestRacing(t *testing.T) {
	done := int64(5)
	lobby := &usdoku.GameInfo{Players: []usdoku.Player{{Name: "a"}}}
	live := &usdoku.GameInfo{Info: usdoku.Info{StartedAt: 1}, Players: []usdoku.Player{{Name: "a", CompletedAt: &done}, {Name: "b"}}}
	over := &usdoku.GameInfo{Info: usdoku.Info{StartedAt: 1}, Players: []usdoku.Player{{Name: "a", CompletedAt: &done}}}
	if racing(lobby) || !racing(live) || racing(over) {
		t.Errorf("racing: lobby %v, live %v, over %v; want false, true, false", racing(lobby), racing(live), racing(over))
	}
}

func TestDuelistFinished(t *testing.T) {
	ids := map[string]int64{"ann": 1, "bob": 2, "guest": 9}
	idOf := func(n string) int64 { return ids[n] }
	pair := []int64{1, 2}
	if !duelistFinished([]string{"bob"}, pair, idOf) {
		t.Fatal("a duelist finished → the duel is decided")
	}
	if duelistFinished([]string{"guest", "nobody"}, pair, idOf) {
		t.Fatal("only a guest and an unknown nick finished → keep waiting")
	}
	if duelistFinished([]string{"ann"}, nil, idOf) {
		t.Fatal("not a duel → never decided here")
	}
}

// A player still filling the board keeps the drain open past settleQuiet: a
// new right cell counts as activity, a cell lost to a wrong digit and won back
// does not. drainMax still caps the wait.
func TestDecider_CellsKeepDrainOpen(t *testing.T) {
	var s watchState
	s.shouldRecord(t0, 1, 3, true, 70) // the winner is done, two still solving
	if s.shouldRecord(t0.Add(5*time.Minute), 1, 3, true, 74) {
		t.Fatal("cells grew → must not record")
	}
	if s.shouldRecord(t0.Add(5*time.Minute+settleQuiet-time.Minute), 1, 3, true, 74) {
		t.Fatal("within settleQuiet of the last new cell → must not record")
	}
	// 74 → 72 (a wrong digit) → 74 again is no progress.
	s.shouldRecord(t0.Add(6*time.Minute), 1, 3, true, 72)
	if !s.shouldRecord(t0.Add(5*time.Minute+settleQuiet), 1, 3, true, 74) {
		t.Fatal("no new right cell for settleQuiet → should record")
	}

	var slow watchState
	slow.shouldRecord(t0, 1, 3, true, 70)
	for m := 1; m < 25; m++ {
		slow.shouldRecord(t0.Add(time.Duration(m)*time.Minute), 1, 3, true, 70+m)
	}
	if !slow.shouldRecord(t0.Add(drainMax), 1, 3, true, 100) {
		t.Fatal("drainMax caps the wait even while cells keep coming")
	}
}

// A room whose round was restarted reports "over" before anybody plays. While
// nobody has finished, play pushes the drain cap along, so it cannot run out
// right after the first real finish.
func TestDecider_StaleOverDoesNotStartTheCap(t *testing.T) {
	var s watchState
	s.shouldRecord(t0, 0, 3, true, 0) // stale "over", the round just began
	for m := 1; m <= 23; m++ {
		if s.shouldRecord(t0.Add(time.Duration(m)*time.Minute), 0, 3, true, m*3) {
			t.Fatalf("minute %d: players are filling the board, nobody finished → must not record", m)
		}
	}
	// The first finish at minute 24, others still filling: well inside drainMax.
	if s.shouldRecord(t0.Add(24*time.Minute), 1, 3, true, 120) {
		t.Fatal("first finish must not hit a cap counted from the stale flag")
	}
	if s.shouldRecord(t0.Add(26*time.Minute), 1, 3, true, 126) {
		t.Fatal("others still filling → must not record")
	}
}
