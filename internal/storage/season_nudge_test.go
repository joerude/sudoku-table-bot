package storage

import (
	"path/filepath"
	"testing"
)

// TestSeasonNudgeRoundTrip: a fresh season has no nudge marker; the marker is
// stored per season and survives reopening (schema.sql + migrate() rerun).
func TestSeasonNudgeRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const chat = int64(-4101)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	got, err := st.SeasonNudge(se.ID)
	if err != nil || got != "" {
		t.Fatalf("fresh SeasonNudge = %q, %v; want empty", got, err)
	}
	if err := st.SetSeasonNudge(se.ID, "week@2026-08-01 00:00:00"); err != nil {
		t.Fatalf("SetSeasonNudge: %v", err)
	}
	if got, _ := st.SeasonNudge(se.ID); got != "week@2026-08-01 00:00:00" {
		t.Errorf("SeasonNudge = %q after set", got)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { st2.Close() })
	if got, _ := st2.SeasonNudge(se.ID); got != "week@2026-08-01 00:00:00" {
		t.Errorf("after reopen SeasonNudge = %q", got)
	}
}
