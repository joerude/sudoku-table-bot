package storage

import (
	"errors"
	"testing"
)

func TestNormalizeNick(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"Zhoo", "Zhoo", nil},
		{"  Zhoo  ", "Zhoo", nil},
		{"@Zhoo", "Zhoo", nil},
		{" @Zhoo ", "Zhoo", nil},
		{"", "", ErrNickEmpty},
		{"   ", "", ErrNickEmpty},
		{"@", "", ErrNickEmpty},
		{"Zhoo Mart", "", ErrNickSpaces},
		{"Zhoo\tMart", "", ErrNickSpaces},
	}
	for _, tc := range cases {
		got, err := NormalizeNick(tc.in)
		if got != tc.want || !errors.Is(err, tc.wantErr) {
			t.Errorf("NormalizeNick(%q) = (%q, %v), want (%q, %v)", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestSetNickStoresNormalized(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-310)
	st.EnsureChat(chat, 1)
	p, _, _ := st.RegisterPlayer(chat, 1, "Zhoomart")
	if err := st.SetNick(p.ID, "  @Zhoo "); err != nil {
		t.Fatal(err)
	}
	got, _ := st.PlayerByID(p.ID)
	if got == nil || got.UsdokuNick.String != "Zhoo" {
		t.Errorf("nick = %+v, want Zhoo", got)
	}
}

func TestSetNickIsUniquePerChat(t *testing.T) {
	st := openTemp(t)
	const chat, other = int64(-311), int64(-312)
	st.EnsureChat(chat, 1)
	st.EnsureChat(other, 1)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	c, _, _ := st.RegisterPlayer(other, 3, "Carol")
	if err := st.SetNick(a.ID, "Ace"); err != nil {
		t.Fatal(err)
	}

	// Same nick, different case, other player in the same chat: refused.
	err := st.SetNick(b.ID, "ACE")
	var taken *NickTakenError
	if !errors.As(err, &taken) {
		t.Fatalf("want NickTakenError, got %v", err)
	}
	if taken.Holder != "Alice" || taken.Nick != "ACE" {
		t.Errorf("taken = %+v, want holder Alice, nick ACE", taken)
	}
	if got, _ := st.PlayerByID(b.ID); got.UsdokuNick.Valid {
		t.Errorf("refused nick must not be stored, got %+v", got.UsdokuNick)
	}

	// The owner may re-set their own nick (even changing the case).
	if err := st.SetNick(a.ID, "ace"); err != nil {
		t.Errorf("re-setting own nick: %v", err)
	}
	// Another chat is independent.
	if err := st.SetNick(c.ID, "Ace"); err != nil {
		t.Errorf("same nick in another chat: %v", err)
	}
	// A removed player no longer holds the nick.
	if _, err := st.RemovePlayer(chat, "Alice"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNick(b.ID, "Ace"); err != nil {
		t.Errorf("nick of a removed player should be free: %v", err)
	}
}

func TestSetNickRejectsEmpty(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-313)
	st.EnsureChat(chat, 1)
	p, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	if err := st.SetNick(p.ID, " @ "); !errors.Is(err, ErrNickEmpty) {
		t.Errorf("want ErrNickEmpty, got %v", err)
	}
}
