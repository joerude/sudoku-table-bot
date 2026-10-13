package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func newCardGame(t *testing.T, st *Store) (chat, gid int64) {
	t.Helper()
	chat = -300
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	gid, err = st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	return chat, gid
}

func TestPostCardRoundTrip(t *testing.T) {
	st := openTemp(t)
	chat, gid := newCardGame(t, st)

	g, err := st.GameByID(gid)
	if err != nil || g == nil {
		t.Fatalf("GameByID: %v %v", g, err)
	}
	if g.PostMsgID.Valid || g.PostText.Valid || g.PostKind.Valid {
		t.Fatalf("fresh game must have no card: %+v", g)
	}

	if err := st.SetPostMsg(gid, 77); err != nil {
		t.Fatal(err)
	}
	g, _ = st.GameByID(gid)
	if !g.PostMsgID.Valid || g.PostMsgID.Int64 != 77 || g.PostText.Valid {
		t.Fatalf("after SetPostMsg: %+v", g)
	}

	if err := st.SetPostCard(gid, 78, "<b>card</b>", "invite"); err != nil {
		t.Fatal(err)
	}
	pending, err := st.ActivePendingGame(chat)
	if err != nil || pending == nil {
		t.Fatalf("ActivePendingGame: %v %v", pending, err)
	}
	if pending.PostMsgID.Int64 != 78 || pending.PostText.String != "<b>card</b>" || pending.PostKind.String != "invite" {
		t.Fatalf("card not stored: %+v", pending)
	}

	if err := st.SetPostText(gid, ""); err != nil {
		t.Fatal(err)
	}
	g, _ = st.GameByID(gid)
	if g.PostText.Valid {
		t.Fatalf("empty text must clear the base: %+v", g.PostText)
	}
	if g.PostMsgID.Int64 != 78 {
		t.Fatalf("clearing the text must keep the message id: %+v", g.PostMsgID)
	}
}

// A database from before the card columns must open and gain them, repeatedly.
func TestOpenAddsCardColumnsToOldDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE games (
		id INTEGER PRIMARY KEY AUTOINCREMENT, chat_id INTEGER NOT NULL, season_id INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending', difficulty TEXT, mode TEXT, usdoku_code TEXT,
		reminded INTEGER NOT NULL DEFAULT 0, created_by INTEGER,
		created_at TEXT NOT NULL DEFAULT (datetime('now')), completed_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	if _, err := st.db.Exec(`UPDATE games SET post_msg_id=5, post_text='x', post_kind='y'`); err != nil {
		t.Fatalf("card columns missing after migrate: %v", err)
	}
	st.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
}
