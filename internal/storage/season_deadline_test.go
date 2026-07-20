package storage

import (
	"path/filepath"
	"testing"
)

// TestSeasonDeadlineRoundTrip: a fresh season has no deadline; once set it is
// read back by every season getter and survives reopening the store.
func TestSeasonDeadlineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const chat = int64(-4001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if se.Deadline.Valid {
		t.Errorf("fresh season: Deadline = %q, want unset", se.Deadline.String)
	}

	const dl = "2026-07-31 18:00:00"
	if err := st.SetSeasonDeadline(se.ID, dl); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}

	got, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after set: %v", err)
	}
	if !got.Deadline.Valid || got.Deadline.String != dl {
		t.Errorf("ActiveSeason: Deadline = %v/%q, want %q", got.Deadline.Valid, got.Deadline.String, dl)
	}
	byID, err := st.SeasonByID(se.ID)
	if err != nil {
		t.Fatalf("SeasonByID: %v", err)
	}
	if byID.Deadline.String != dl {
		t.Errorf("SeasonByID: Deadline = %q, want %q", byID.Deadline.String, dl)
	}
	byNum, err := st.SeasonByNumber(chat, se.Number)
	if err != nil {
		t.Fatalf("SeasonByNumber: %v", err)
	}
	if byNum.Deadline.String != dl {
		t.Errorf("SeasonByNumber: Deadline = %q, want %q", byNum.Deadline.String, dl)
	}

	// Reopening runs schema.sql + migrate() again: the column must survive.
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { st2.Close() })
	again, err := st2.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after reopen: %v", err)
	}
	if again.Deadline.String != dl {
		t.Errorf("after reopen: Deadline = %q, want %q", again.Deadline.String, dl)
	}
}

// TestCloseSeasonLeavesDeadlineUnset: the next season starts without a
// deadline, so the bot's lazy fill assigns a fresh one.
func TestCloseSeasonLeavesDeadlineUnset(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-4002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if err := st.SetSeasonDeadline(se.ID, "2026-07-31 18:00:00"); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}
	next, err := st.CloseSeason(se, 0)
	if err != nil {
		t.Fatalf("CloseSeason: %v", err)
	}
	if next.Deadline.Valid {
		t.Errorf("next season: Deadline = %q, want unset", next.Deadline.String)
	}
}
