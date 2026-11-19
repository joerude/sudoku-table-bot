package bot

import (
	"slices"
	"testing"
)

func TestLobbyWaitLine(t *testing.T) {
	expected := []string{"Вова", "Дима", "Саша"}
	cases := []struct {
		joined, expected []string
		want             string
	}{
		{[]string{"Вова", "Дима"}, expected, "⏳ Ждём: Саша"},
		{[]string{"Дима", "Саша", "Вова"}, expected, "✅ Все в комнате, можно стартовать"},
		{[]string{"Вова", "guest42"}, expected, "⏳ Ждём: Дима, Саша"}, // an unknown nick waits for nobody
		{nil, expected, ""}, // empty room: the card says nothing
		{[]string{"Вова"}, []string{"Вова"}, ""}, // a solo chat has no one to wait for
		{[]string{"Вова"}, []string{"Вова", "<b>"}, "⏳ Ждём: &lt;b&gt;"},
	}
	for _, tc := range cases {
		if got := lobbyWaitLine(tc.joined, tc.expected); got != tc.want {
			t.Errorf("lobbyWaitLine(%v) = %q, want %q", tc.joined, got, tc.want)
		}
	}
}

func TestLiveSectionShowsWaiting(t *testing.T) {
	s := liveSnapshot{Joined: []string{"Вова"}, Waiting: "⏳ Ждём: Дима"}
	if got, want := liveSectionText(s), "👀 В комнате: Вова\n⏳ Ждём: Дима"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLobbyExpected(t *testing.T) {
	const chat = int64(-7801)
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	vova, _, _ := st.RegisterPlayer(chat, 1, "Вова")
	dima, _, _ := st.RegisterPlayer(chat, 2, "Дима")
	st.RegisterPlayer(chat, 3, "Саша")
	b := &Bot{st: st}
	expect := func(gid int64, want ...string) {
		t.Helper()
		g, err := st.GameByID(gid)
		if err != nil {
			t.Fatal(err)
		}
		got := b.lobbyExpected(g)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("game %d: expected %v, want %v", gid, got, want)
		}
	}

	plain, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	expect(plain, "Вова", "Дима", "Саша")

	duel, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.SetDuelTarget(duel, dima.ID)
	expect(duel, "Вова", "Дима")

	invite, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	st.SetPostCard(invite, 100, "card", cardKindInvite)
	expect(invite, "Вова", "Дима", "Саша") // nobody pressed «Я в деле» yet
	st.ToggleRsvp(invite, vova.ID)
	st.ToggleRsvp(invite, dima.ID)
	expect(invite, "Вова", "Дима")
}
