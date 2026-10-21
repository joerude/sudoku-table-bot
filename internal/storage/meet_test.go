package storage

import "testing"

func TestMeetLifecycle(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7401)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	_, _, _ = st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")
	gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.SetUsdokuCode(gid, "LOVI")
	st.SetPostMsg(gid, 77)

	ok, err := st.SetMeet(gid, "2026-10-21 10:30:00", b.ID)
	if err != nil || !ok {
		t.Fatalf("first SetMeet = %v, %v", ok, err)
	}
	if ok, _ := st.SetMeet(gid, "2026-10-21 11:00:00", b.ID); ok {
		t.Errorf("the first agreement stands: a second SetMeet must not overwrite it")
	}
	g, _ := st.GameByID(gid)
	if g.MeetAt.String != "2026-10-21 10:30:00" || g.MeetBy.Int64 != b.ID {
		t.Errorf("game meet = %v / %v", g.MeetAt, g.MeetBy)
	}

	if due, _ := st.DueMeets("2026-10-21 10:29:59"); len(due) != 0 {
		t.Errorf("not due before its time: %+v", due)
	}
	due, err := st.DueMeets("2026-10-21 10:30:00")
	if err != nil || len(due) != 1 {
		t.Fatalf("DueMeets = %+v, %v", due, err)
	}
	want := DueMeet{GameID: gid, ChatID: chat, Code: "LOVI", CreatedBy: 1, MeetBy: b.ID,
		MeetAt: "2026-10-21 10:30:00", PostMsgID: 77}
	if due[0] != want {
		t.Errorf("DueMeet = %+v, want %+v", due[0], want)
	}

	if ok, _ := st.MarkMeetDone(gid); !ok {
		t.Errorf("first MarkMeetDone must win")
	}
	if ok, _ := st.MarkMeetDone(gid); ok {
		t.Errorf("the ping goes out once: second MarkMeetDone must lose")
	}
	if due, _ := st.DueMeets("2026-10-21 12:00:00"); len(due) != 0 {
		t.Errorf("a done meet is not due again: %+v", due)
	}
}

func TestDueMeetsSkipsStaleOnes(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7403)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	b, _, _ := st.RegisterPlayer(chat, 2, "B")
	gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.SetMeet(gid, "2026-10-21 10:30:00", b.ID)

	if due, _ := st.DueMeets("2026-10-21 10:45:00"); len(due) != 1 {
		t.Errorf("a quarter of an hour late still pings: %+v", due)
	}
	// A bot outage over the meet, or a game reopened by an edit days later:
	// a ping about a long-gone «16:30» is noise.
	if due, _ := st.DueMeets("2026-10-21 10:45:01"); len(due) != 0 {
		t.Errorf("a stale meet must not ping: %+v", due)
	}
}

func TestMeetOnlyForLivePendingGames(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7402)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")

	deleted, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.SetMeet(deleted, "2026-10-21 10:00:00", b.ID)
	st.SoftDeleteGame(deleted)

	done, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.SetMeet(done, "2026-10-21 10:00:00", b.ID)
	st.AddPick(done, a.ID)
	st.AddPick(done, b.ID)
	st.FinalizeGame(done, se.PointsTable)

	if due, _ := st.DueMeets("2026-10-21 10:05:00"); len(due) != 0 {
		t.Errorf("deleted and recorded games are never pinged: %+v", due)
	}
	if ok, _ := st.SetMeet(done, "2026-10-21 12:00:00", b.ID); ok {
		t.Errorf("no meet on a recorded game")
	}
}
