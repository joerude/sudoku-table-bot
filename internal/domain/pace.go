package domain

import (
	"math"
	"sort"
)

// Personal pace compares how far a finisher trails the game's fastest solve
// with how far they usually trail. Everyone solves the same board, so its
// difficulty cancels out; an absolute time norm would mostly reward easy
// boards, and with them the winner.
const (
	PaceWindow     = 15   // the usual lag is taken from this many newest lags
	PaceMinSamples = 5    // fewer lags: no usual lag yet
	PaceMinDrop    = 0.08 // today's lag must beat the usual one by 8% of the winner's time
	PaceMinSecs    = 20   // and by at least this many seconds
)

// Lag is a finisher's delay behind the fastest solve, as a fraction of it.
func Lag(secs, best int) (float64, bool) {
	if best <= 0 || secs < best {
		return 0, false
	}
	return float64(secs-best) / float64(best), true
}

// UsualLag is the median of the newest PaceWindow lags (lags newest first),
// capped at the career median: a run of slow games played on purpose cannot
// loosen it, while real improvement tightens it.
func UsualLag(lags []float64) (float64, bool) {
	if len(lags) < PaceMinSamples {
		return 0, false
	}
	recent := lags
	if len(recent) > PaceWindow {
		recent = recent[:PaceWindow]
	}
	return math.Min(median(recent), median(lags)), true
}

// PaceWin reports a finisher who trailed the fastest solve clearly less than
// usual: lagSecs is today's delay, usualSecs the usual one on today's board.
func PaceWin(secs, best int, lags []float64) (lagSecs, usualSecs int, ok bool) {
	lag, ok := Lag(secs, best)
	if !ok {
		return 0, 0, false
	}
	usual, ok := UsualLag(lags)
	if !ok {
		return 0, 0, false
	}
	if usual-lag < PaceMinDrop || (usual-lag)*float64(best) < PaceMinSecs {
		return 0, 0, false
	}
	return secs - best, int(math.Round(usual * float64(best))), true
}

func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
