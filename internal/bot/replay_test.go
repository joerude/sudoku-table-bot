package bot

import (
	"testing"
	"time"
)

func TestReplayData(t *testing.T) {
	start := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	at := func(secs int) time.Time { return start.Add(time.Duration(secs) * time.Second) }
	samples := []progSample{
		{at: at(20), open: 50, solved: map[string]int{"al": 10, "bobby": 4}},
		{at: at(40), open: 50, solved: map[string]int{"al": 30, "bobby": 12}},
		{at: at(60), open: 50, solved: map[string]int{"al": 44, "bobby": 20}},
		// Alice finished at 1:10: what usdoku keeps for her afterwards is ignored.
		{at: at(80), open: 50, solved: map[string]int{"bobby": 30}},
	}
	players := []replayPlayer{
		{ID: 2, Name: "Bob", Nick: "bobby"},
		{ID: 1, Name: "Alice", Nick: "al", Done: true, Finish: 70},
	}
	r, ok := replayData("Ход игры · QWER", "Medium", start, samples, players)
	if !ok || r.Open != 50 || r.Step != replayStep || len(r.Series) != 2 {
		t.Fatalf("replay = %+v, ok = %v", r, ok)
	}
	alice, bob := r.Series[0], r.Series[1] // by player id, like the season charts
	if alice.Name != "Alice" || alice.Label != "Alice 1:10" {
		t.Errorf("finisher label: %+v", alice)
	}
	// 10 s a point up to the finish at 70 s; halfway between samples is linear.
	if len(alice.Points) != 7 || alice.Points[0] != 5 || alice.Points[2] != 20 || alice.Points[6] != 50 {
		t.Errorf("finisher line = %v", alice.Points)
	}
	if bob.Label != "Bob 30/50" || len(bob.Points) != 8 || bob.Points[7] != 30 {
		t.Errorf("unfinished line = %+v", bob)
	}

	if _, ok := replayData("t", "", start, samples[:3], players); ok {
		t.Errorf("three samples are too few to draw")
	}
	if _, ok := replayData("t", "", start, samples, players[:1]); ok {
		t.Errorf("one player is no race")
	}
}

func TestRoundStart(t *testing.T) {
	first := time.Date(2026, 11, 16, 12, 0, 30, 0, time.UTC)
	began := first.Add(-25 * time.Second)
	if got := roundStart(began.Unix(), first); !got.Equal(began) {
		t.Errorf("seconds: %v, want %v", got, began)
	}
	if got := roundStart(began.UnixMilli(), first); !got.Equal(began) {
		t.Errorf("milliseconds: %v, want %v", got, began)
	}
	// No start, or one the bot's clock cannot agree with: one fetch before the first sample.
	want := first.Add(-progressEvery)
	for _, bad := range []int64{0, first.Add(time.Hour).Unix(), first.Add(-48 * time.Hour).Unix()} {
		if got := roundStart(bad, first); !got.Equal(want) {
			t.Errorf("roundStart(%d) = %v, want %v", bad, got, want)
		}
	}
}
