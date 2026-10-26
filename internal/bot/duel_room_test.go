package bot

import (
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func roomOf(nicks ...string) *usdoku.GameInfo {
	info := &usdoku.GameInfo{}
	for _, n := range nicks {
		info.Players = append(info.Players, usdoku.Player{Name: n})
	}
	return info
}

// duelRoomFixture: Alice (tg 201) challenges Bob; both have usdoku nicks.
func duelRoomFixture(t *testing.T) (*Bot, *storage.Game, int64) {
	t.Helper()
	st := openTempStore(t)
	const chat = int64(-9501)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 201, "Alice")
	bob, _, _ := st.RegisterPlayer(chat, 202, "Bob")
	if err := st.SetNick(a.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNick(bob.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	gid, err := st.CreatePendingGame(chat, se.ID, 201, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDuelTarget(gid, bob.ID); err != nil {
		t.Fatal(err)
	}
	g, _ := st.GameByID(gid)
	t.Cleanup(func() { setRoomBusy(gid, false) })
	return &Bot{st: st}, g, bob.ID
}

// The challenger sitting alone in the room is not an answer.
func TestDuelRoomSeenChallengerAlone(t *testing.T) {
	b, g, _ := duelRoomFixture(t)
	b.duelRoomSeen(g, roomOf("alice"))
	if roomBusy(g.ID) {
		t.Error("challenger alone must not mark the room busy")
	}
	if ok, _ := b.st.AcceptDuel(g.ID); !ok {
		t.Error("challenge must still be open")
	}
}

// A guest keeps the challenge alive (busy) but does not accept it.
func TestDuelRoomSeenGuest(t *testing.T) {
	b, g, _ := duelRoomFixture(t)
	b.duelRoomSeen(g, roomOf("alice", "zed"))
	if !roomBusy(g.ID) {
		t.Error("a guest in the room must mark it busy")
	}
	if ok, _ := b.st.AcceptDuel(g.ID); !ok {
		t.Error("a guest must not accept the challenge")
	}
}

// The target joining by the link accepts the challenge; a second look is a no-op.
func TestDuelRoomSeenTargetAccepts(t *testing.T) {
	b, g, _ := duelRoomFixture(t)
	b.duelRoomSeen(g, roomOf("alice", "bob"))
	if !roomBusy(g.ID) {
		t.Error("room with the target must be busy")
	}
	if ok, _ := b.st.AcceptDuel(g.ID); ok {
		t.Error("the target joining must have accepted the challenge")
	}
	b.duelRoomSeen(g, roomOf("alice", "bob")) // idempotent, must not panic or re-accept
}

// A manual picker whose order differs from usdoku's is never overwritten by
// auto-record: it does nothing (no usdoku order appended, no fallback note)
// and reports it. Picks that are the head of usdoku's order are extended.
func TestAutoRecordSkipsManualRecording(t *testing.T) {
	b, g, bobID := duelRoomFixture(t)
	pair, err := b.st.DuelPlayerIDs(g.ID)
	if err != nil || len(pair) != 2 {
		t.Fatalf("pair: %v %v", pair, err)
	}
	aliceID := pair[0]
	if aliceID == bobID {
		aliceID = pair[1]
	}
	if err := b.st.AddPick(g.ID, aliceID); err != nil {
		t.Fatal(err)
	}
	done := int64(100)
	info := roomOf("alice", "bob")
	info.Players[1].CompletedAt = &done // usdoku: bob won
	if handled := b.autoRecord(g, info); handled {
		t.Fatal("autoRecord must report it did nothing")
	}
	ids, _ := b.st.PickedPlayerIDs(g.ID)
	if len(ids) != 1 || ids[0] != aliceID {
		t.Errorf("manual picks changed: %v", ids)
	}
	if cur, _ := b.st.GameByID(g.ID); cur.Status != "pending" {
		t.Errorf("game status = %q, want pending", cur.Status)
	}
}
