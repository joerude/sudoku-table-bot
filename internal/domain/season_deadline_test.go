package domain

import (
	"testing"
	"time"
)

// bishkek is a fixed +06:00 zone: the test container has no tzdata, so
// LoadLocation("Asia/Bishkek") would silently fall back to UTC.
var bishkek = time.FixedZone("UTC+6", 6*3600)

func TestSeasonDeadlineIsTwoMonthsFromStart(t *testing.T) {
	start := time.Date(2026, 7, 8, 12, 0, 0, 0, bishkek)
	want := time.Date(2026, 9, 8, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(start, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestSeasonDeadlineCrossesYear(t *testing.T) {
	start := time.Date(2026, 12, 5, 9, 0, 0, 0, bishkek)
	want := time.Date(2027, 2, 5, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(start, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

// A start day that doesn't exist two months later normalises forward, which
// only ever lengthens the season, never truncates it.
func TestSeasonDeadlineDayOverflowsForward(t *testing.T) {
	start := time.Date(2026, 12, 31, 9, 0, 0, 0, bishkek)
	got := SeasonDeadline(start, bishkek)
	if !got.After(start.UTC()) {
		t.Fatalf("SeasonDeadline = %s, want after start %s", got, start)
	}
	if want := time.Date(2027, 3, 3, 0, 0, 0, 0, bishkek).UTC(); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestSeasonDaysLeft(t *testing.T) {
	deadline := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC() // last day: 31 July
	cases := []struct {
		name string
		now  time.Time
		want int
	}{
		{"mid month", time.Date(2026, 7, 20, 12, 0, 0, 0, bishkek), 11},
		{"day before", time.Date(2026, 7, 30, 23, 59, 0, 0, bishkek), 1},
		{"last day morning", time.Date(2026, 7, 31, 0, 1, 0, 0, bishkek), 0},
		{"last day night", time.Date(2026, 7, 31, 23, 59, 0, 0, bishkek), 0},
		{"just after", time.Date(2026, 8, 1, 0, 0, 30, 0, bishkek), -1},
	}
	for _, tc := range cases {
		if got := SeasonDaysLeft(deadline, tc.now, bishkek); got != tc.want {
			t.Errorf("%s: SeasonDaysLeft = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestExtendSeasonDeadlineOneMonth(t *testing.T) {
	deadline := time.Date(2026, 9, 8, 0, 0, 0, 0, bishkek).UTC()
	now := time.Date(2026, 9, 8, 0, 1, 0, 0, bishkek)
	want := time.Date(2026, 10, 8, 0, 0, 0, 0, bishkek).UTC()
	if got := ExtendSeasonDeadline(deadline, now, bishkek); !got.Equal(want) {
		t.Errorf("ExtendSeasonDeadline = %s, want %s", got, want)
	}
}

func TestExtendSeasonDeadlineSkipsLongOutage(t *testing.T) {
	// Bot was down from September to December: the deadline must land ahead of now.
	deadline := time.Date(2026, 9, 8, 0, 0, 0, 0, bishkek).UTC()
	now := time.Date(2026, 12, 10, 0, 0, 0, 0, bishkek)
	want := time.Date(2027, 1, 8, 0, 0, 0, 0, bishkek).UTC()
	if got := ExtendSeasonDeadline(deadline, now, bishkek); !got.Equal(want) {
		t.Errorf("ExtendSeasonDeadline = %s, want %s", got, want)
	}
}
