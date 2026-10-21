package domain

import (
	"math"
	"testing"
)

func TestLag(t *testing.T) {
	if l, ok := Lag(330, 300); !ok || math.Abs(l-0.1) > 1e-9 {
		t.Errorf("Lag(330, 300) = %v, %v; want 0.1", l, ok)
	}
	if l, ok := Lag(300, 300); !ok || l != 0 {
		t.Errorf("a tie with the fastest is a zero lag, got %v, %v", l, ok)
	}
	for _, c := range [][2]int{{300, 0}, {290, 300}, {0, 300}} {
		if _, ok := Lag(c[0], c[1]); ok {
			t.Errorf("Lag(%d, %d) must be invalid", c[0], c[1])
		}
	}
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestUsualLag(t *testing.T) {
	if _, ok := UsualLag(repeat(0.2, PaceMinSamples-1)); ok {
		t.Errorf("too few lags must give no usual lag")
	}
	if u, ok := UsualLag([]float64{0.1, 0.3, 0.2, 0.5, 0.4}); !ok || math.Abs(u-0.3) > 1e-9 {
		t.Errorf("median of five = %v, %v; want 0.3", u, ok)
	}
	// Fifteen slow games on purpose after a long career: the usual lag stays
	// at the career median, so the slow streak buys no easy line later.
	sandbag := append(repeat(0.5, PaceWindow), repeat(0.2, 30)...)
	if u, _ := UsualLag(sandbag); math.Abs(u-0.2) > 1e-9 {
		t.Errorf("sandbagging must not loosen the usual lag: %v", u)
	}
	// A player who got better is measured against the recent, stricter lag.
	better := append(repeat(0.2, PaceWindow), repeat(0.4, 30)...)
	if u, _ := UsualLag(better); math.Abs(u-0.2) > 1e-9 {
		t.Errorf("improvement must tighten the usual lag: %v", u)
	}
}

func TestPaceWin(t *testing.T) {
	usual := repeat(0.3, 10)
	lag, us, ok := PaceWin(315, 300, usual)
	if !ok || lag != 15 || us != 90 {
		t.Errorf("PaceWin(315, 300) = %d, %d, %v; want 15, 90, true", lag, us, ok)
	}
	if _, _, ok := PaceWin(370, 300, usual); ok {
		t.Errorf("a lag 6.7 points under the usual one is not notable")
	}
	if _, _, ok := PaceWin(112, 100, usual); ok {
		t.Errorf("18 seconds saved is under the bar")
	}
	if _, _, ok := PaceWin(315, 300, repeat(0.3, 3)); ok {
		t.Errorf("no usual lag yet: no pace win")
	}
	if _, _, ok := PaceWin(290, 300, usual); ok {
		t.Errorf("faster than the fastest is bad data")
	}
}
