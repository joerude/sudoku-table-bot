package bot

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"
)

func TestJoinName(t *testing.T) {
	cases := []struct {
		u        *tele.User
		explicit string
		want     string
	}{
		{&tele.User{FirstName: "Ann", Username: "ann"}, "", "Ann"},
		{&tele.User{FirstName: "Ann"}, "  Annie ", "Annie"},
		{&tele.User{Username: "ann"}, "", "ann"},
		{&tele.User{}, "", "Игрок"},
		{nil, "", "Игрок"},
	}
	for _, tc := range cases {
		if got := joinName(tc.u, tc.explicit); got != tc.want {
			t.Errorf("joinName(%+v, %q) = %q, want %q", tc.u, tc.explicit, got, tc.want)
		}
	}
}

func TestNickTakenText(t *testing.T) {
	got := nickTakenText("Ace", "A<b>")
	if !strings.Contains(got, "Ace") || !strings.Contains(got, "уже у") {
		t.Errorf("missing parts: %q", got)
	}
	if strings.Contains(got, "A<b>") {
		t.Errorf("holder name not escaped: %q", got)
	}
}

func TestSetNickDoneText(t *testing.T) {
	fresh := setNickDoneText("Ace", true)
	if !strings.Contains(fresh, "Записал тебя") || !strings.Contains(fresh, "Ace") {
		t.Errorf("created text: %q", fresh)
	}
	if !strings.Contains(fresh, "комнате usdoku") {
		t.Errorf("must hint that the nick matches the usdoku room name: %q", fresh)
	}
	if again := setNickDoneText("Ace", false); strings.Contains(again, "Записал тебя") {
		t.Errorf("existing player must not see the registration note: %q", again)
	}
	if esc := setNickDoneText("<x>", false); strings.Contains(esc, "<x>") {
		t.Errorf("nick not escaped: %q", esc)
	}
}
