package storage

import "testing"

func TestUsernameAndPlayerByID(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-600)
	st.EnsureChat(chat, 1)

	p, _, _ := st.RegisterPlayer(chat, 10, "Vasya")

	if err := st.SetUsername(p.ID, "vasya"); err != nil {
		t.Fatal(err)
	}

	got, err := st.PlayerByID(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("PlayerByID returned nil")
	}
	if !got.Username.Valid || got.Username.String != "vasya" {
		t.Errorf("Username: want {vasya true}, got %+v", got.Username)
	}

	// ListPlayers should return that username too.
	players, err := st.ListPlayers(chat)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 {
		t.Fatalf("want 1 player, got %d", len(players))
	}
	if !players[0].Username.Valid || players[0].Username.String != "vasya" {
		t.Errorf("ListPlayers username: want vasya, got %+v", players[0].Username)
	}

	// SetNick and assert UsdokuNick via ListPlayers and PlayerByID.
	if err := st.SetNick(p.ID, "VasyaNick"); err != nil {
		t.Fatal(err)
	}
	got2, err := st.PlayerByID(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got2.UsdokuNick.Valid || got2.UsdokuNick.String != "VasyaNick" {
		t.Errorf("UsdokuNick via PlayerByID: want VasyaNick, got %+v", got2.UsdokuNick)
	}
	players2, err := st.ListPlayers(chat)
	if err != nil {
		t.Fatal(err)
	}
	if !players2[0].UsdokuNick.Valid || players2[0].UsdokuNick.String != "VasyaNick" {
		t.Errorf("UsdokuNick via ListPlayers: want VasyaNick, got %+v", players2[0].UsdokuNick)
	}
}

func TestSetUsernameEmptyClears(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-601)
	st.EnsureChat(chat, 1)

	p, _, _ := st.RegisterPlayer(chat, 20, "Petya")
	st.SetUsername(p.ID, "petya")
	st.SetUsername(p.ID, "")

	got, err := st.PlayerByID(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Username.Valid {
		t.Errorf("expected username to be cleared, got %+v", got.Username)
	}
}

func TestDuelTarget(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-602)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 30, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 31, "Bob")

	gid, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "")

	// No duel target initially.
	tid, ok, err := st.DuelTargetID(gid)
	if err != nil {
		t.Fatal(err)
	}
	if ok || tid != 0 {
		t.Errorf("expected no duel target, got (%d, %v)", tid, ok)
	}

	// Set duel target.
	if err := st.SetDuelTarget(gid, b.ID); err != nil {
		t.Fatal(err)
	}
	tid, ok, err = st.DuelTargetID(gid)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || tid != b.ID {
		t.Errorf("expected duel target (%d, true), got (%d, %v)", b.ID, tid, ok)
	}
}

func TestRsvpToggleAndRoster(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-603)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 40, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 41, "Bob")

	gid, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "")

	// Both toggle in.
	inA, err := st.ToggleRsvp(gid, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !inA {
		t.Error("ToggleRsvp(a): want true")
	}
	inB, err := st.ToggleRsvp(gid, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !inB {
		t.Error("ToggleRsvp(b): want true")
	}

	roster, err := st.RsvpPlayers(gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 2 {
		t.Fatalf("want 2 in roster, got %d", len(roster))
	}
	// Ordered by name: Alice, Bob.
	if roster[0].Name != "Alice" || roster[1].Name != "Bob" {
		t.Errorf("name order wrong: %v, %v", roster[0].Name, roster[1].Name)
	}

	// Toggle a out.
	inA2, err := st.ToggleRsvp(gid, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inA2 {
		t.Error("ToggleRsvp(a) second: want false")
	}

	roster2, err := st.RsvpPlayers(gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster2) != 1 || roster2[0].Name != "Bob" {
		t.Errorf("after toggle-out: want [Bob], got %v", roster2)
	}
}

func TestRsvpExcludesDeactivated(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-604)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)

	a, _, _ := st.RegisterPlayer(chat, 50, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 51, "Bob")

	gid, _ := st.CreatePendingGame(chat, se.ID, a.ID, "medium", "")

	st.ToggleRsvp(gid, a.ID)
	st.ToggleRsvp(gid, b.ID)

	// Deactivate Alice.
	if _, err := st.RemovePlayer(chat, "Alice"); err != nil {
		t.Fatal(err)
	}

	roster, err := st.RsvpPlayers(gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 1 || roster[0].Name != "Bob" {
		t.Errorf("deactivated player should be excluded, got %v", roster)
	}
}
