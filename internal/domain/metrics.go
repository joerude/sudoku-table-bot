package domain

import (
	"sort"
	"time"
)

// Habit metrics: the numbers the habit mechanics are judged by, for one
// window. Definitions match the 2026-10-15 baseline (see the project wiki),
// so a window before the mechanics and one after compare like for like.

// chainGap is how soon the next game must start after a result to count as
// played "right after" (the post-game hook's target).
const chainGap = 10 * time.Minute

// nudgeAnswer is how soon after a nudge a started game counts as its answer.
const nudgeAnswer = 2 * time.Hour

// MetricResult is one player's line: rank 0 = did not finish.
type MetricResult struct {
	PlayerID int64
	Rank     int
}

// MetricGame is one completed game.
type MetricGame struct {
	ID        int64
	Created   time.Time
	Completed time.Time
	Duel      bool
	Results   []MetricResult
}

// MetricsInput is one window's data.
type MetricsInput struct {
	From, To  time.Time
	Loc       *time.Location
	Games     []MetricGame   // completed in [From, To)
	Starts    []time.Time    // starts of completed games from From to past To (nil: Games' own); answers may cross the window's end
	Active    []int64        // active players: "all finished" means all of them
	HookGames map[int64]bool // games whose result carried a hook line
	Abandoned int            // rooms created in the window and deleted unplayed
	Nudges    []time.Time    // daily nudges sent in the window
}

// Metrics is one window's numbers. Pairs are (hits, base).
type Metrics struct {
	Days             int
	Games            int
	PerWeek          float64
	DaysWithGame     int
	Chained          int // games followed by the next one within chainGap
	ChainBase        int
	ChainHook        int // the same, season games with a hook line
	ChainHookBase    int
	ChainPlain       int // the same, season games without one
	ChainPlainBase   int
	SeasonGames      int
	AllFinished      int              // season games every active player finished
	DNF              map[int64][2]int // per player: (did not finish, joined), season games
	AbandonedPerWeek float64
	Nudges           int
	NudgesPlayed     int // nudges answered by a game started within nudgeAnswer
}

// ComputeMetrics derives one window's metrics.
func ComputeMetrics(in MetricsInput) Metrics {
	m := Metrics{DNF: map[int64][2]int{}}
	m.Days = int(in.To.Sub(in.From).Round(24*time.Hour) / (24 * time.Hour))
	weeks := float64(m.Days) / 7
	m.Games = len(in.Games)
	if weeks > 0 {
		m.PerWeek = float64(m.Games) / weeks
		m.AbandonedPerWeek = float64(in.Abandoned) / weeks
	}

	days := map[string]bool{}
	starts := append([]time.Time(nil), in.Starts...)
	for _, g := range in.Games {
		days[g.Completed.In(in.Loc).Format("2006-01-02")] = true
		if in.Starts == nil {
			starts = append(starts, g.Created)
		}
	}
	m.DaysWithGame = len(days)
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })

	// followed: the first game started at or after done does so within chainGap.
	followed := func(done time.Time) bool {
		i := sort.Search(len(starts), func(i int) bool { return !starts[i].Before(done) })
		return i < len(starts) && starts[i].Sub(done) <= chainGap
	}
	active := make(map[int64]bool, len(in.Active))
	for _, p := range in.Active {
		active[p] = true
	}
	for _, g := range in.Games {
		next := followed(g.Completed)
		m.ChainBase++
		if next {
			m.Chained++
		}
		if g.Duel {
			continue
		}
		if in.HookGames[g.ID] {
			m.ChainHookBase++
			if next {
				m.ChainHook++
			}
		} else {
			m.ChainPlainBase++
			if next {
				m.ChainPlain++
			}
		}
		m.SeasonGames++
		finished := 0
		for _, r := range g.Results {
			d := m.DNF[r.PlayerID]
			d[1]++
			if r.Rank == 0 {
				d[0]++
			} else if active[r.PlayerID] {
				finished++
			}
			m.DNF[r.PlayerID] = d
		}
		if len(active) > 0 && finished == len(active) {
			m.AllFinished++
		}
	}

	m.Nudges = len(in.Nudges)
	for _, n := range in.Nudges {
		i := sort.Search(len(starts), func(i int) bool { return !starts[i].Before(n) })
		if i < len(starts) && starts[i].Sub(n) <= nudgeAnswer {
			m.NudgesPlayed++
		}
	}
	return m
}
