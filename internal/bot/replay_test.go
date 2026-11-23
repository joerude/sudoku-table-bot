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

// A player with no cell for minutes came late: their line starts at 0 from
// their own start, so the round's idle stretch does not squeeze the others.
func TestReplayDataLateStart(t *testing.T) {
	start := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	var samples []progSample
	for s := 20; s <= 1680; s += 20 {
		joe := 0
		if s > 1320 {
			joe = (s - 1320) / 20
		}
		samples = append(samples, progSample{at: start.Add(time.Duration(s) * time.Second), open: 51,
			solved: map[string]int{"nur": min(s/7, 51), "joe": joe}})
	}
	players := []replayPlayer{
		{ID: 1, Name: "Joe", Nick: "joe"},
		{ID: 2, Name: "Nur", Nick: "nur", Rank: 1, Finish: 352},
	}
	r, ok := replayData(start, time.FixedZone("BISH", 6*3600), samples, players)
	if !ok {
		t.Fatal("no replay")
	}
	joe := r.Series[0]
	// Joe's line runs from his last empty fetch (22:00) to the last one (28:00).
	if len(joe.Points) != 36 || joe.Points[1] != 1 || joe.Points[35] != 18 {
		t.Errorf("late line = %d points, %v", len(joe.Points), joe.Points)
	}
	want := []chart.ReplayRow{
		{Slot: 1, Place: 1, Name: "Nur", Value: "5:52"},
		{Slot: 0, Name: "Joe", Value: "18/51", Note: "с 22-й минуты"},
	}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("rows = %+v, want %+v", r.Rows, want)
	}
	if r.Meta != "Пн, 23 ноября · 09:00 · 28 мин" {
		t.Errorf("meta = %q", r.Meta)
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
