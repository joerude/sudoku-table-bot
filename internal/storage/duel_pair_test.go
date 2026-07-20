package storage

import (
	"path/filepath"
	"testing"
)

// TestDuelPlayerIDs: resolves the two duelists (creator via tg id + target),
// nil for non-duels and for duels whose creator can't be mapped to a player.
func TestDuelPlayerIDs(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3001)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")

	duel, err := st.CreatePendingGame(chat, se.ID, 11, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame: %v", err)
	}
	if err := st.SetDuelTarget(duel, b.ID); err != nil {
		t.Fatalf("SetDuelTarget: %v", err)
	}
	pair, err := st.DuelPlayerIDs(duel)
	if err != nil {
		t.Fatalf("DuelPlayerIDs: %v", err)
	}
	if len(pair) != 2 || pair[0] != a.ID || pair[1] != b.ID {
		t.Errorf("pair = %v, want [%d %d]", pair, a.ID, b.ID)
	}

	normal, err := st.CreatePendingGame(chat, se.ID, 11, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame normal: %v", err)
	}
	if pair, err := st.DuelPlayerIDs(normal); err != nil || pair != nil {
		t.Errorf("non-duel: pair = %v, err = %v, want nil, nil", pair, err)
	}

	orphan, err := st.CreatePendingGame(chat, se.ID, 999, "medium", "hardcore")
	if err != nil {
		t.Fatalf("CreatePendingGame orphan: %v", err)
	}
	if err := st.SetDuelTarget(orphan, b.ID); err != nil {
		t.Fatalf("SetDuelTarget orphan: %v", err)
	}
	if pair, err := st.DuelPlayerIDs(orphan); err != nil || pair != nil {
		t.Errorf("unresolvable creator: pair = %v, err = %v, want nil, nil", pair, err)
	}

	if pair, err := st.DuelPlayerIDs(99999); err != nil || pair != nil {
		t.Errorf("missing game: pair = %v, err = %v, want nil, nil", pair, err)
	}
}

// TestMigrateDropsDuelGuestRows: reopening the store deletes result rows of
// duel games that belong to neither duelist (room guests recorded by old
// auto-record), fixing RecentDuels' winner/loser. Normal games and duels with
// an unresolvable creator are left untouched.
func TestMigrateDropsDuelGuestRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	const chat = int64(-3002)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 11, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 12, "Bob")
	c, _, _ := st.RegisterPlayer(chat, 13, "Zed") // sorts after Bob → MAX(name) trap

	seed := func(createdBy int64, duel bool) int64 {
		t.Helper()
		gid, err := st.CreatePendingGame(chat, se.ID, createdBy, "medium", "hardcore")
		if err != nil {
			t.Fatalf("CreatePendingGame: %v", err)
		}
		if duel {
			if err := st.SetDuelTarget(gid, b.ID); err != nil {
				t.Fatalf("SetDuelTarget: %v", err)
			}
		}
		if err := st.AddPick(gid, a.ID); err != nil {
			t.Fatalf("AddPick a: %v", err)
		}
		if err := st.AddPick(gid, b.ID); err != nil {
			t.Fatalf("AddPick b: %v", err)
		}
		if err := st.AddDNF(gid, c.ID); err != nil {
			t.Fatalf("AddDNF c: %v", err)
		}
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatalf("FinalizeGame: %v", err)
		}
		return gid
	}
	duel := seed(11, true)     // Alice challenged Bob; Zed crashed the room
	normal := seed(11, false)  // 3-player season game, must keep all rows
	orphan := seed(999, true)  // creator not a player — cleanup must not guess

	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st, err = Open(path) // migrate runs again on open
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	wantRows := func(gid int64, want int) {
		t.Helper()
		rows, err := st.GameResults(gid)
		if err != nil {
			t.Fatalf("GameResults(%d): %v", gid, err)
		}
		if len(rows) != want {
			t.Errorf("game %d: %d result rows, want %d (%v)", gid, len(rows), want, rows)
		}
	}
	wantRows(duel, 2)
	wantRows(normal, 3)
	wantRows(orphan, 3)

	// The prod symptom: loser must be the actual duelist, not MAX(name) guest.
	duels, err := st.RecentDuels(chat, 10)
	if err != nil {
		t.Fatalf("RecentDuels: %v", err)
	}
	if len(duels) == 0 {
		t.Fatal("RecentDuels: empty")
	}
	last := duels[len(duels)-1] // seeded duel is the oldest
	if last.Winner != "Alice" || last.Loser != "Bob" {
		t.Errorf("RecentDuels: %s beat %s, want Alice beat Bob", last.Winner, last.Loser)
	}
}
