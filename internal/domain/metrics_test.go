package domain

import (
	"math"
	"testing"
	"time"
)

// mt is a local time on 13-26 October 2026 (UTC+6).
func mt(day, hh, mm int) time.Time { return time.Date(2026, 10, day, hh, mm, 0, 0, tz6) }

// mg builds a game created at start and finished ten minutes later.
func mg(id int64, start time.Time, duel bool, res ...[2]int) MetricGame {
	g := MetricGame{ID: id, Created: start, Completed: start.Add(10 * time.Minute), Duel: duel}
	for _, r := range res {
		g.Results = append(g.Results, MetricResult{PlayerID: int64(r[0]), Rank: r[1]})
	}
	return g
}

func TestComputeMetrics(t *testing.T) {
	in := MetricsInput{
		From: mt(13, 0, 0), To: mt(27, 0, 0), Loc: tz6,
		Active: []int64{1, 2, 3},
		Games: []MetricGame{
			mg(1, mt(13, 14, 0), false, [2]int{1, 1}, [2]int{2, 2}, [2]int{3, 3}),  // all finished
			mg(2, mt(13, 14, 15), false, [2]int{2, 1}, [2]int{1, 0}, [2]int{3, 2}), // 5 min after game 1 ended
			mg(3, mt(15, 20, 0), false, [2]int{3, 1}, [2]int{1, 2}),                // Bob missing
			mg(4, mt(15, 20, 40), true, [2]int{1, 1}, [2]int{2, 2}),                // duel, 30 min later
			mg(5, mt(20, 9, 0), false, [2]int{1, 1}, [2]int{2, 0}, [2]int{3, 0}),
		},
		HookGames: map[int64]bool{1: true, 3: true},
		Abandoned: 4,
		Nudges:    []time.Time{mt(15, 19, 0), mt(18, 15, 0)},
	}
	m := ComputeMetrics(in)

	if m.Days != 14 || m.Games != 5 || math.Abs(m.PerWeek-2.5) > 1e-9 {
		t.Errorf("days/games/per week = %d/%d/%v, want 14/5/2.5", m.Days, m.Games, m.PerWeek)
	}
	if m.DaysWithGame != 3 {
		t.Errorf("days with a game = %d, want 3 (13th, 15th, 20th)", m.DaysWithGame)
	}
	if m.Chained != 1 || m.ChainBase != 5 {
		t.Errorf("chained = %d of %d, want 1 of 5 (only game 2 came within 10 min)", m.Chained, m.ChainBase)
	}
	if m.ChainHook != 1 || m.ChainHookBase != 2 || m.ChainPlain != 0 || m.ChainPlainBase != 2 {
		t.Errorf("hook split = %d/%d with, %d/%d without; want 1/2 and 0/2 (season games only)",
			m.ChainHook, m.ChainHookBase, m.ChainPlain, m.ChainPlainBase)
	}
	if m.SeasonGames != 4 || m.AllFinished != 1 {
		t.Errorf("all finished = %d of %d season games, want 1 of 4", m.AllFinished, m.SeasonGames)
	}
	if got := m.DNF[1]; got != [2]int{1, 4} {
		t.Errorf("player 1 DNF = %v, want 1 of 4 season games", got)
	}
	if got := m.DNF[2]; got != [2]int{1, 3} {
		t.Errorf("player 2 DNF = %v, want 1 of 3", got)
	}
	if math.Abs(m.AbandonedPerWeek-2) > 1e-9 {
		t.Errorf("abandoned per week = %v, want 2", m.AbandonedPerWeek)
	}
	if m.Nudges != 2 || m.NudgesPlayed != 1 {
		t.Errorf("nudges = %d, played after = %d; want 2 and 1 (game 3 started 1 h after the first)", m.Nudges, m.NudgesPlayed)
	}
}

func TestComputeMetricsSeesStartsBeyondTheWindow(t *testing.T) {
	last := mg(1, mt(26, 23, 45), false, [2]int{1, 1}, [2]int{2, 2}) // ends 23:55, the window ends at midnight
	in := MetricsInput{
		From: mt(13, 0, 0), To: mt(27, 0, 0), Loc: tz6, Active: []int64{1, 2},
		Games:  []MetricGame{last},
		Starts: []time.Time{last.Created, mt(27, 0, 2)}, // the next game started after midnight
		Nudges: []time.Time{mt(26, 23, 0)},
	}
	m := ComputeMetrics(in)
	if m.Chained != 1 || m.NudgesPlayed != 1 {
		t.Errorf("a game started past the window's end still answers: chained %d, nudges %d", m.Chained, m.NudgesPlayed)
	}
}

func TestComputeMetricsEmpty(t *testing.T) {
	m := ComputeMetrics(MetricsInput{From: mt(13, 0, 0), To: mt(27, 0, 0), Loc: tz6})
	if m.Games != 0 || m.PerWeek != 0 || m.ChainBase != 0 || m.DaysWithGame != 0 {
		t.Errorf("empty window: %+v", m)
	}
}
