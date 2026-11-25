package bot

import (
	"reflect"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/chart"
)

func TestReplayData(t *testing.T) {
	start := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) // a Monday
	at := func(secs int) time.Time { return start.Add(time.Duration(secs) * time.Second) }
	samples := []progSample{
		{at: at(20), open: 50, solved: map[string]int{"al": 10, "bobby": 4}},
		{at: at(40), open: 50, solved: map[string]int{"al": 30, "bobby": 12}},
		{at: at(60), open: 50, solved: map[string]int{"al": 44, "bobby": 20}},
		// Alice finished at 1:05: what usdoku keeps for her afterwards is ignored.
		{at: at(80), open: 50, solved: map[string]int{"bobby": 30}},
	}
	players := []replayPlayer{
		{ID: 2, Name: "Bob", Nick: "bobby"},
		{ID: 1, Name: "Alice", Nick: "al", Rank: 1, Finish: 65},
	}
	r, ok := replayData(start, time.UTC, samples, players)
	if !ok || r.Open != 50 || r.Step != replayStep || len(r.Series) != 2 {
		t.Fatalf("replay = %+v, ok = %v", r, ok)
	}
	alice, bob := r.Series[0], r.Series[1] // by player id, like the season charts
	// 10 s a point up to the finish; the last point sits on the finish time itself.
	if alice.Name != "Alice" || len(alice.Points) != 7 || alice.Points[0] != 5 || alice.Points[2] != 20 ||
		alice.Points[6] != 50 || alice.Last != 6.5 {
		t.Errorf("finisher line = %+v", alice)
	}
	if len(bob.Points) != 8 || bob.Points[7] != 30 || bob.Last != 0 {
		t.Errorf("unfinished line = %+v", bob)
	}
	want := []chart.ReplayRow{
		{Slot: 0, Place: 1, Name: "Alice", Value: "1:05"},
		{Slot: 1, Name: "Bob", Value: "30/50"},
	}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("rows = %+v, want %+v", r.Rows, want)
	}
	if r.Meta != "Пн, 16 ноября · 12:00 · 1 мин 20 с" {
		t.Errorf("meta = %q", r.Meta)
	}

	if _, ok := replayData(start, time.UTC, samples[:3], players); ok {
		t.Errorf("three samples are too few to draw")
	}
	if _, ok := replayData(start, time.UTC, samples, players[:1]); ok {
		t.Errorf("one player is no race")
	}
}

// OLXR: the room sat with an empty board for 22 minutes (but for one stray
// cell), then both played. The idle stretch is cut for everybody alike, and
// the winner's finish comes from usdoku's completion moment, because the
// recorded time counts from his own join.
func TestReplayDataIdleStart(t *testing.T) {
	start := time.Date(2026, 11, 24, 17, 1, 0, 0, time.UTC) // 23:01 in Bishkek
	var samples []progSample
	for s := 20; s <= 1520; s += 20 {
		nur, joe := 0, 0
		if s >= 40 && s <= 100 {
			nur = 1
		}
		if s > 1360 {
			nur, joe = (s-1360)/7, (s-1360)/4
		}
		samples = append(samples, progSample{at: start.Add(time.Duration(s) * time.Second), open: 52,
			solved: map[string]int{"nur": nur, "joe": joe}})
	}
	players := []replayPlayer{
		{ID: 1, Name: "Joe", Nick: "joe", Rank: 1, Finish: 170, Done: start.Add(1530 * time.Second)},
		{ID: 2, Name: "Nur", Nick: "nur"},
	}
	r, ok := replayData(start, time.FixedZone("BISH", 6*3600), samples, players)
	if !ok {
		t.Fatal("no replay")
	}
	joe, nur := r.Series[0], r.Series[1]
	if len(joe.Points) != 17 || joe.Points[1] != 5 || joe.Points[16] != 52 || joe.Last != 17 {
		t.Errorf("winner line = %d points, last %v: %v", len(joe.Points), joe.Last, joe.Points)
	}
	if len(nur.Points) != 16 || nur.Points[1] != 2 || nur.Points[15] != 22 {
		t.Errorf("other line = %d points: %v", len(nur.Points), nur.Points)
	}
	want := []chart.ReplayRow{
		{Slot: 0, Place: 1, Name: "Joe", Value: "2:50"},
		{Slot: 1, Name: "Nur", Value: "22/52"},
	}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("rows = %+v, want %+v", r.Rows, want)
	}
	if r.Meta != "Вт, 24 ноября · 23:23 · 2 мин 50 с" {
		t.Errorf("meta = %q", r.Meta)
	}
}

func TestRoundWindow(t *testing.T) {
	first := time.Date(2026, 11, 16, 12, 0, 30, 0, time.UTC)
	samples := make([]progSample, 90) // one every 20 s, 30 minutes
	for i := range samples {
		samples[i] = progSample{at: first.Add(time.Duration(i) * progressEvery)}
	}
	began := first.Add(-25 * time.Second)
	for _, v := range []int64{began.Unix(), began.UnixMilli()} {
		if got, s := roundWindow(v, samples); !got.Equal(began) || len(s) != len(samples) {
			t.Errorf("roundWindow(%d) = %v with %d samples, want %v with all", v, got, len(s), began)
		}
	}
	// The round restarted in the same room 23 minutes in: the earlier samples
	// are the old round's and go.
	restart := first.Add(23*time.Minute + 5*time.Second)
	got, s := roundWindow(restart.Unix(), samples)
	if !got.Equal(restart) || len(s) == 0 || s[0].at.Before(restart) || s[0].at.Sub(restart) > progressEvery {
		t.Errorf("restart: start %v, first kept sample %v", got, s[0].at)
	}
	// No start, or one the bot's clock cannot agree with: one fetch before the first sample.
	want := first.Add(-progressEvery)
	for _, bad := range []int64{0, first.Add(time.Hour).Unix(), first.Add(-48 * time.Hour).Unix()} {
		if got, s := roundWindow(bad, samples); !got.Equal(want) || len(s) != len(samples) {
			t.Errorf("roundWindow(%d) = %v, want %v", bad, got, want)
		}
	}
}
