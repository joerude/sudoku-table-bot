package domain

import (
	"testing"
	"time"
)

// bishkek is a fixed +06:00 zone: the test container has no tzdata, so
// LoadLocation("Asia/Bishkek") would silently fall back to UTC.
var bishkek = time.FixedZone("UTC+6", 6*3600)

func TestSeasonDeadlineIsNextMonthStart(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, bishkek)
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(now, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestSeasonDeadlineSkipsStubSeason(t *testing.T) {
	// 28 July: less than 7 days left, so the deadline jumps to 1 September.
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, bishkek)
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(now, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestSeasonDeadlineCrossesYear(t *testing.T) {
	now := time.Date(2026, 12, 5, 9, 0, 0, 0, bishkek)
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := SeasonDeadline(now, bishkek); !got.Equal(want) {
		t.Errorf("SeasonDeadline = %s, want %s", got, want)
	}
}

func TestExtendSeasonDeadlineOneMonth(t *testing.T) {
	deadline := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC()
	now := time.Date(2026, 8, 1, 0, 1, 0, 0, bishkek)
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := ExtendSeasonDeadline(deadline, now, bishkek); !got.Equal(want) {
		t.Errorf("ExtendSeasonDeadline = %s, want %s", got, want)
	}
}

func TestExtendSeasonDeadlineSkipsLongOutage(t *testing.T) {
	// Bot was down from August to November: the deadline must land ahead of now.
	deadline := time.Date(2026, 8, 1, 0, 0, 0, 0, bishkek).UTC()
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, bishkek)
	want := time.Date(2026, 12, 1, 0, 0, 0, 0, bishkek).UTC()
	if got := ExtendSeasonDeadline(deadline, now, bishkek); !got.Equal(want) {
		t.Errorf("ExtendSeasonDeadline = %s, want %s", got, want)
	}
}
