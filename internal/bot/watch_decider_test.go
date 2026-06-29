package bot

import (
	"testing"
	"time"
)

// t0 is an arbitrary fixed base time for deterministic decider tests.
var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestDecider_AllFinishedRecordsImmediately(t *testing.T) {
	var s watchState
	if !s.shouldRecord(t0, 3, 3, false) {
		t.Fatal("all joined players finished → should record at once")
	}
}

func TestDecider_NobodyFinishedNotOverKeepsPolling(t *testing.T) {
	var s watchState
	for i := 0; i < 5; i++ {
		if s.shouldRecord(t0.Add(time.Duration(i)*watchInterval), 0, 3, false) {
			t.Fatalf("poll %d: nobody finished and round not over → must not record", i)
		}
	}
}

func TestDecider_DrainsUntilQuietThenRecords(t *testing.T) {
	var s watchState
	// First finisher (1 of 3) starts the drain; do not record yet.
	if s.shouldRecord(t0, 1, 3, true) {
		t.Fatal("first finisher should start drain, not record")
	}
	// Quiet but below settleQuiet → keep waiting.
	if s.shouldRecord(t0.Add(settleQuiet-time.Minute), 1, 3, true) {
		t.Fatal("still within settle window → must not record")
	}
	// Quiet for the full settle window → record.
	if !s.shouldRecord(t0.Add(settleQuiet), 1, 3, true) {
		t.Fatal("no new finisher for settleQuiet → should record")
	}
}

func TestDecider_NewFinisherResetsSettleTimer(t *testing.T) {
	var s watchState
	// First finisher starts drain.
	s.shouldRecord(t0, 1, 3, true)
	// 5 min later (under settleQuiet=6m) a 2nd finisher arrives — resets the timer.
	if s.shouldRecord(t0.Add(5*time.Minute), 2, 3, true) {
		t.Fatal("new finisher arrived → must not record yet")
	}
	// 5 min after the 2nd finisher (10m since first) — still within the reset window.
	if s.shouldRecord(t0.Add(10*time.Minute), 2, 3, true) {
		t.Fatal("timer reset on 2nd finisher → still waiting at +5m")
	}
	// 6 min of quiet after the 2nd finisher → record.
	if !s.shouldRecord(t0.Add(5*time.Minute+settleQuiet), 2, 3, true) {
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
		if s.shouldRecord(now, i+1, 99, true) {
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
	if s.shouldRecord(t0, 0, 3, true) {
		t.Fatal("over with 0 finishers should start drain, not record")
	}
	if !s.shouldRecord(t0.Add(settleQuiet), 0, 3, true) {
		t.Fatal("over with 0 finishers → record after settle so caller can fall back to manual")
	}
}
