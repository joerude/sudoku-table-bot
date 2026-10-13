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

func TestWelcomeTextTeachesPlayFlow(t *testing.T) {
	for _, want := range []string{"/setnick", "/play", "/help", "удалять сообщения"} {
		if !strings.Contains(welcomeText, want) {
			t.Errorf("welcomeText missing %q", want)
		}
	}
	if strings.Contains(welcomeText, "/newgame") || strings.Contains(welcomeText, "/join") {
		t.Errorf("welcomeText must teach /setnick + /play, not /join or /newgame")
	}
}

func TestHelpTextCoversMenuCommands(t *testing.T) {
	all := append(groupCommands(), adminCommands()...)
	for _, c := range all {
		if c.Text != "help" && !strings.Contains(helpText, "/"+c.Text) {
			t.Errorf("helpText missing menu command /%s", c.Text)
		}
	}
	for _, want := range []string{"/players", "/backup", "/learn", "/rating"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("helpText missing %q", want)
		}
	}
}

func TestQuickMenuKeyboard(t *testing.T) {
	row := quickMenuKeyboard().InlineKeyboard[0]
	want := []struct{ text, unique string }{
		{"🎮 Играть", cbQGame},
		{"📊 Таблица", cbQStatus},
		{"❓ Помощь", cbQHelp},
	}
	if len(row) != len(want) {
		t.Fatalf("want %d buttons, got %d", len(want), len(row))
	}
	for i, w := range want {
		if row[i].Text != w.text || row[i].Unique != w.unique {
			t.Errorf("button %d = (%q, %q), want (%q, %q)", i, row[i].Text, row[i].Unique, w.text, w.unique)
		}
	}
}

func TestStartText(t *testing.T) {
	if got := startText(0); got != welcomeText {
		t.Errorf("first /start in an empty chat must show the welcome")
	}
	short := startText(3)
	if short == welcomeText || len(short) >= len(welcomeText) {
		t.Errorf("repeat /start must be a short pointer, got %q", short)
	}
	for _, want := range []string{"/play", "/stats", "/help"} {
		if !strings.Contains(short, want) {
			t.Errorf("repeat /start missing %q", want)
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
