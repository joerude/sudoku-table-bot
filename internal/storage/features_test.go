package storage

import "testing"

func TestFeatureDefaultsOnAndToggles(t *testing.T) {
	st := openTemp(t)
	const chat, other = int64(-7101), int64(-7102)

	on, err := st.FeatureEnabled(chat, "hook")
	if err != nil || !on {
		t.Fatalf("a feature with no row must be on: on=%v err=%v", on, err)
	}
	if err := st.SetFeature(chat, "hook", false); err != nil {
		t.Fatal(err)
	}
	if on, _ := st.FeatureEnabled(chat, "hook"); on {
		t.Errorf("hook must be off after SetFeature(false)")
	}
	if on, _ := st.FeatureEnabled(other, "hook"); !on {
		t.Errorf("switching one chat must not touch another")
	}
	if on, _ := st.FeatureEnabled(chat, "pace"); !on {
		t.Errorf("switching one feature must not touch another")
	}
	if err := st.SetFeature(chat, "hook", true); err != nil {
		t.Fatal(err)
	}
	if on, _ := st.FeatureEnabled(chat, "hook"); !on {
		t.Errorf("hook must be back on after SetFeature(true)")
	}
}

func TestLogEventCounts(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7103)
	for _, e := range []Event{
		{ChatID: chat, GameID: 5, Kind: "hook_shown", Detail: "near"},
		{ChatID: chat, GameID: 6, PlayerID: 2, Kind: "hook_shown"},
		{ChatID: chat, Kind: "meet_set"},
		{ChatID: chat - 1, Kind: "hook_shown"},
	} {
		if err := st.LogEvent(e); err != nil {
			t.Fatalf("LogEvent(%+v): %v", e, err)
		}
	}
	if n, err := st.CountEvents(chat, "hook_shown"); err != nil || n != 2 {
		t.Errorf("hook_shown count = %d (err %v), want 2", n, err)
	}
	if n, _ := st.CountEvents(chat, "meet_set"); n != 1 {
		t.Errorf("meet_set count = %d, want 1", n)
	}
	var nulls int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM bot_events
		WHERE kind='meet_set' AND game_id IS NULL AND player_id IS NULL AND detail IS NULL`).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 1 {
		t.Errorf("zero ids and empty detail must be stored as NULL")
	}
}

func TestRecentEventsNewestFirstWithTimes(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7105)
	for _, e := range []Event{
		{ChatID: chat, Kind: "nudge_sent", Detail: "smart", At: "2026-10-19 15:00:00"},
		{ChatID: chat, Kind: "nudge_sent", Detail: "fixed", At: "2026-10-20 15:00:00"},
		{ChatID: chat, Kind: "hook_shown", At: "2026-10-21 15:00:00"},
		{ChatID: chat - 1, Kind: "nudge_sent", At: "2026-10-21 15:00:00"},
	} {
		if err := st.LogEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.RecentEvents(chat, "nudge_sent", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].At != "2026-10-20 15:00:00" || got[0].Detail != "fixed" ||
		got[1].At != "2026-10-19 15:00:00" {
		t.Errorf("RecentEvents = %+v", got)
	}
	if one, _ := st.RecentEvents(chat, "nudge_sent", 1); len(one) != 1 || one[0].Detail != "fixed" {
		t.Errorf("limit 1 = %+v", one)
	}
	if err := st.LogEvent(Event{ChatID: chat, Kind: "now"}); err != nil {
		t.Fatal(err)
	}
	if now, _ := st.RecentEvents(chat, "now", 1); len(now) != 1 || len(now[0].At) != len("2006-01-02 15:04:05") {
		t.Errorf("an event without At gets the current time: %+v", now)
	}
}
