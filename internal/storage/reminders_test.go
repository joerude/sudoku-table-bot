package storage

import "testing"

// A declined duel is soft-deleted but stays status='pending'; it must not
// trigger the "unrecorded game" nudge.
func TestStalePendingGamesSkipsDeleted(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	live, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame live: %v", err)
	}
	gone, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame gone: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE games SET created_at = datetime('now', '-2 hours')`); err != nil {
		t.Fatalf("age games: %v", err)
	}
	if err := st.SoftDeleteGame(gone); err != nil {
		t.Fatalf("SoftDeleteGame: %v", err)
	}

	got, err := st.StalePendingGames(45)
	if err != nil {
		t.Fatalf("StalePendingGames: %v", err)
	}
	if len(got) != 1 || got[0].GameID != live {
		t.Fatalf("got %+v, want only the live game %d", got, live)
	}
}

// The nudge needs the room code (to skip games the watcher still handles), the
// creator (to ping when there is no card) and the card message (to edit).
func TestStalePendingGamesCarriesCardAndRoom(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := st.CreatePendingGame(chat, se.ID, 42, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetUsdokuCode(gid, "LOVI"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPostMsg(gid, 99); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE games SET created_at = datetime('now', '-2 hours')`); err != nil {
		t.Fatal(err)
	}
	got, err := st.StalePendingGames(45)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	sp := got[0]
	if sp.Code != "LOVI" || sp.CreatedBy.Int64 != 42 || sp.PostMsgID.Int64 != 99 {
		t.Errorf("stale game lost its context: %+v", sp)
	}
}

// A reopened game is marked reminded (its creator is editing it), and a game
// with picks is mid-recording: neither gets the stale nudge.
func TestStalePendingGamesSkipsReopenedAndPicked(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7003)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	reopened := makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	if err := st.ReopenGame(reopened); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE games SET created_at = datetime('now', '-2 hours')`); err != nil {
		t.Fatal(err)
	}
	if got, err := st.StalePendingGames(45); err != nil || len(got) != 0 {
		t.Fatalf("reopened game must not be nudged: %+v err=%v", got, err)
	}

	picked, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.AddPick(picked, a.ID)
	if _, err := st.db.Exec(`UPDATE games SET created_at = datetime('now', '-2 hours'), reminded=0 WHERE id=?`, picked); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.StalePendingGames(45); len(got) != 0 {
		t.Fatalf("game with picks must not be nudged: %+v", got)
	}
}

// Private chats (positive ids) have no league: the ticker must not walk them.
func TestAllChatsSkipsPrivate(t *testing.T) {
	st := openTemp(t)
	st.EnsureChat(-7004, 1)
	st.EnsureChat(555, 555)
	got, err := st.AllChats()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ChatID != -7004 {
		t.Fatalf("AllChats = %+v, want only the group", got)
	}
}

func TestGameStartsSince(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-7106)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "A")
	b, _, _ := st.RegisterPlayer(chat, 2, "B")
	game := func(created string, finish, deleted bool) {
		t.Helper()
		gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		if finish {
			st.AddPick(gid, a.ID)
			st.AddPick(gid, b.ID)
			st.FinalizeGame(gid, se.PointsTable)
		}
		if deleted {
			st.SoftDeleteGame(gid)
		}
		if _, err := st.db.Exec(`UPDATE games SET created_at=? WHERE id=?`, created, gid); err != nil {
			t.Fatal(err)
		}
	}
	game("2026-09-01 08:00:00", true, false)  // before the window
	game("2026-10-20 09:30:00", true, false)  // counts
	game("2026-10-19 07:15:00", true, false)  // counts, comes first
	game("2026-10-20 10:00:00", false, false) // never played
	game("2026-10-20 11:00:00", true, true)   // deleted

	got, err := st.GameStartsSince(chat, "2026-10-01 00:00:00")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-10-19 07:15:00", "2026-10-20 09:30:00"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("GameStartsSince = %v, want %v", got, want)
	}
}
