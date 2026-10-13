package storage

import "testing"

func TestClaimNickRefusesATakenNick(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-501)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Anna")
	b, _, _ := st.RegisterPlayer(chat, 2, "Boris")

	if ok, err := st.ClaimNick(a.ID, "Joe"); err != nil || !ok {
		t.Fatalf("free nick: ok=%v err=%v", ok, err)
	}
	if ok, err := st.ClaimNick(b.ID, "joe"); err != nil || ok {
		t.Fatalf("taken nick (other case) must be refused: ok=%v err=%v", ok, err)
	}
	got, _ := st.PlayerByNick(chat, "joe")
	if got == nil || got.ID != a.ID {
		t.Fatalf("the nick must stay with its owner: %+v", got)
	}
	// Re-claiming your own nick is fine (idempotent tap).
	if ok, err := st.ClaimNick(a.ID, "Joe"); err != nil || !ok {
		t.Fatalf("own nick: ok=%v err=%v", ok, err)
	}
}

func TestClaimNickIgnoresInactiveOwnersAndOtherChats(t *testing.T) {
	st := openTemp(t)
	const chat, other = int64(-502), int64(-503)
	for _, c := range []int64{chat, other} {
		if err := st.EnsureChat(c, 1); err != nil {
			t.Fatal(err)
		}
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Anna")
	b, _, _ := st.RegisterPlayer(chat, 2, "Boris")
	x, _, _ := st.RegisterPlayer(other, 3, "Xena")

	if ok, _ := st.ClaimNick(x.ID, "joe"); !ok {
		t.Fatal("other chat claim must work")
	}
	if ok, err := st.ClaimNick(a.ID, "joe"); err != nil || !ok {
		t.Fatalf("a nick used in another chat is free here: ok=%v err=%v", ok, err)
	}
	if _, err := st.RemovePlayer(chat, "Anna"); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.ClaimNick(b.ID, "joe"); err != nil || !ok {
		t.Fatalf("a removed player's nick is free: ok=%v err=%v", ok, err)
	}
}
