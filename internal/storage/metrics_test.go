package storage

import "testing"

func TestMetricsQueries(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7701)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")
	at := func(gid int64, created, completed string) {
		t.Helper()
		if _, err := st.db.Exec(`UPDATE games SET created_at=?, completed_at=? WHERE id=?`, created, completed, gid); err != nil {
			t.Fatal(err)
		}
	}
	played := func(created, completed string) int64 {
		gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		st.AddPick(gid, a.ID)
		st.AddPick(gid, b.ID)
		st.FinalizeGame(gid, se.PointsTable)
		at(gid, created, completed)
		return gid
	}
	inWin := played("2026-10-14 09:50:00", "2026-10-14 10:00:00")
	played("2026-10-01 09:50:00", "2026-10-01 10:00:00") // before the window
	played("2026-10-28 09:50:00", "2026-10-28 10:00:00") // after it

	abandon := func(created string, duel bool) int64 {
		gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		if duel {
			st.SetDuelTarget(gid, b.ID)
		}
		st.SoftDeleteGame(gid)
		at(gid, created, "")
		return gid
	}
	abandon("2026-10-15 12:00:00", false)
	abandon("2026-10-16 12:00:00", false)
	abandon("2026-10-16 13:00:00", true)  // a withdrawn duel is not an empty room
	abandon("2026-10-02 12:00:00", false) // before the window
	open, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	at(open, "2026-10-17 12:00:00", "") // still pending: not abandoned (yet)

	const from, to = "2026-10-13 00:00:00", "2026-10-27 00:00:00"
	rows, err := st.ResultsBetween(chat, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].GameID != inWin || rows[0].CreatedAt != "2026-10-14 09:50:00" {
		t.Errorf("ResultsBetween = %+v", rows)
	}
	if since, _ := st.ResultsSince(chat, from); len(since) != 4 {
		t.Errorf("ResultsSince keeps no upper bound: %d rows, want 4", len(since))
	}

	if n, err := st.AbandonedRooms(chat, from, to); err != nil || n != 2 {
		t.Errorf("AbandonedRooms = %d, %v; want 2", n, err)
	}

	meet := abandon("2026-10-18 12:00:00", false)
	for _, e := range []Event{
		{GameID: inWin, Kind: "meet_set", At: "2026-10-14 09:55:00"},
		{GameID: meet, Kind: "meet_set", At: "2026-10-18 12:10:00"},
		{GameID: inWin, Kind: "meet_set", At: "2026-10-14 09:56:00"}, // the same game twice
		{Kind: "nudge_sent", Detail: "smart", At: "2026-10-15 14:00:00"},
		{Kind: "nudge_sent", Detail: "smart", At: "2026-10-30 14:00:00"},
	} {
		e.ChatID = chat
		st.LogEvent(e)
	}
	if set, won, err := st.MeetOutcomes(chat, from, to); err != nil || set != 2 || won != 1 {
		t.Errorf("MeetOutcomes = %d set, %d played (%v); want 2 and 1", set, won, err)
	}
	if ev, _ := st.EventsBetween(chat, "nudge_sent", from, to); len(ev) != 1 || ev[0].At != "2026-10-15 14:00:00" {
		t.Errorf("EventsBetween = %+v", ev)
	}

	if off, _ := st.DisabledFeatures(chat); len(off) != 0 {
		t.Errorf("nothing switched off yet: %v", off)
	}
	st.SetFeature(chat, "pace", false)
	st.SetFeature(chat, "hook", false)
	st.SetFeature(chat, "hook", true)
	if off, _ := st.DisabledFeatures(chat); len(off) != 1 || off[0] != "pace" {
		t.Errorf("DisabledFeatures = %v, want [pace]", off)
	}
}
