package bot

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func row(name string, rank, secs int) storage.ResultRow {
	return storage.ResultRow{Name: name, Rank: rank, Duration: secs}
}

func TestNearMiss(t *testing.T) {
	cases := []struct {
		name      string
		rows      []storage.ResultRow
		ok        bool
		place     int
		loser     string
		gap       int
		lineParts []string
	}{
		{"seven seconds for the win", []storage.ResultRow{row("A", 1, 360), row("B", 2, 367)},
			true, 1, "B", 7, []string{"До победы не хватило 7 с", "<b>B</b>", "Реванш?"}},
		{"twenty seconds is not close", []storage.ResultRow{row("A", 1, 360), row("B", 2, 380)},
			false, 0, "", 0, nil},
		{"a long game scales the bar", []storage.ResultRow{row("A", 1, 1200), row("B", 2, 1230)},
			true, 1, "B", 30, nil},
		{"closest pair wins: 2nd and 3rd", []storage.ResultRow{row("A", 1, 300), row("B", 2, 312), row("C", 3, 316)},
			true, 2, "C", 4, []string{"До 2-го места не хватило 4 с", "<b>C</b>", "Ещё одну?"}},
		{"equal gaps prefer the win", []storage.ResultRow{row("A", 1, 300), row("B", 2, 305), row("C", 3, 310)},
			true, 1, "B", 5, nil},
		{"same second", []storage.ResultRow{row("A", 1, 300), row("B", 2, 300)},
			true, 1, "B", 0, []string{"меньше секунды"}},
		{"DNF and unknown times ignored", []storage.ResultRow{row("A", 1, 300), row("B", 2, 0), row("C", 0, 0)},
			false, 0, "", 0, nil},
		{"times against the finish order ignored", []storage.ResultRow{row("A", 1, 310), row("B", 2, 300)},
			false, 0, "", 0, nil},
		{"winner alone", []storage.ResultRow{row("A", 1, 300)}, false, 0, "", 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nm, ok := nearMiss(c.rows)
			if ok != c.ok {
				t.Fatalf("nearMiss ok = %v, want %v (%+v)", ok, c.ok, nm)
			}
			if !ok {
				return
			}
			if nm.Place != c.place || nm.Name != c.loser || nm.Gap != c.gap {
				t.Errorf("nearMiss = %+v, want place %d %s gap %d", nm, c.place, c.loser, c.gap)
			}
			line := nearMissLine(nm)
			for _, p := range c.lineParts {
				if !strings.Contains(line, p) {
					t.Errorf("line %q missing %q", line, p)
				}
			}
			if strings.ContainsAny(line, "—–") {
				t.Errorf("no long dashes in bot texts: %q", line)
			}
		})
	}
}

func TestNearMissLineEscapes(t *testing.T) {
	line := nearMissLine(nearMissPair{Place: 1, Name: "<b>x</b>", Gap: 3})
	if strings.Contains(line, "<b><b>") || !strings.Contains(line, "&lt;b&gt;x") {
		t.Errorf("name must be escaped: %q", line)
	}
}

func stand(name string, pts int) storage.Standing {
	return storage.Standing{Name: name, Points: pts}
}

func TestStakeLine(t *testing.T) {
	table := []int{3, 1, 0}
	cases := []struct {
		name      string
		standings []storage.Standing
		target    int
		want      string
	}{
		{"one win closes the season",
			[]storage.Standing{stand("A", 98), stand("B", 80), stand("C", 60)}, 100,
			"⏭ Ещё одна победа, и <b>A</b> закрывает сезон (98/100)"},
		{"two wins close the season",
			[]storage.Standing{stand("A", 95), stand("B", 80), stand("C", 60)}, 100,
			"⏭ Две победы, и <b>A</b> закрывает сезон (95/100)"},
		{"nothing within reach",
			[]storage.Standing{stand("A", 70), stand("B", 49), stand("C", 40)}, 100, ""},
		{"bottom player first",
			[]storage.Standing{stand("A", 10), stand("B", 8), stand("C", 7)}, 100,
			"⏭ <b>C</b> в 1 очке от 2-го места: хватит одной победы"},
		{"two wins to overtake is not a stake",
			[]storage.Standing{stand("A", 40), stand("B", 30), stand("C", 27)}, 100, ""},
		{"level on points",
			[]storage.Standing{stand("A", 10), stand("B", 6), stand("C", 6)}, 100,
			"⏭ <b>B</b> и <b>C</b> идут вровень за 2-е место: решит следующая игра"},
		{"closing the season beats an overtake",
			[]storage.Standing{stand("A", 97), stand("B", 95), stand("C", 94)}, 100,
			"⏭ Ещё одна победа, и <b>A</b> закрывает сезон (97/100)"},
		{"one win beats two",
			[]storage.Standing{stand("A", 95), stand("B", 94), stand("C", 50)}, 100,
			"⏭ <b>B</b> в 1 очке от 1-го места: хватит одной победы"},
		{"a draw is not an overtake",
			[]storage.Standing{stand("A", 95), stand("B", 93), stand("C", 50)}, 100,
			"⏭ Две победы, и <b>A</b> закрывает сезон (95/100)"},
		{"no target", []storage.Standing{stand("A", 9), stand("B", 9)}, 0,
			"⏭ <b>A</b> и <b>B</b> идут вровень за 1-е место: решит следующая игра"},
		{"single player", []storage.Standing{stand("A", 3)}, 100, ""},
		{"empty", nil, 100, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stakeLine(c.standings, table, c.target); got != c.want {
				t.Errorf("stakeLine = %q\nwant        %q", got, c.want)
			}
		})
	}
	if got := stakeLine([]storage.Standing{stand("A", 5), stand("B", 4)}, []int{1, 1}, 100); got != "" {
		t.Errorf("a table where a win is worth no more than 2nd has no stakes, got %q", got)
	}
}

// hookFixture records a seasonal game with solve times, one player per time
// (Alice, Bob, Carol), in finish order.
func hookFixture(t *testing.T, chat int64, secs ...int64) (*Bot, *storage.Store, *storage.Game) {
	t.Helper()
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range secs {
		p, _, _ := st.RegisterPlayer(chat, int64(i+1), []string{"Alice", "Bob", "Carol"}[i])
		st.AddPickTimed(gid, p.ID, s)
	}
	g, _ := st.GameByID(gid)
	b := &Bot{st: st}
	if _, _, err := b.scoreAndCheck(g); err != nil {
		t.Fatalf("scoreAndCheck: %v", err)
	}
	return b, st, g
}

func TestHookFooterNearMiss(t *testing.T) {
	b, st, g := hookFixture(t, -7201, 360, 366)
	got := b.hookFooter(g)
	if !strings.HasPrefix(got, "\n\n😤 До победы не хватило 6 с: <b>Bob</b>") {
		t.Errorf("hookFooter = %q", got)
	}
	if n, _ := st.CountEvents(g.ChatID, evHookShown); n != 1 {
		t.Errorf("hook_shown events = %d, want 1", n)
	}
	kb := b.resultKBBase(g.ID)
	if !hasButton(kb, "🔁 Реванш") {
		t.Errorf("a near miss turns «Ещё одну» into «Реванш»")
	}
}

func TestHookFooterStake(t *testing.T) {
	// Alice 3, Bob 1, Carol (sat this one out) 0: one win lifts Carol past
	// Bob even if Bob takes 2nd; Bob's single win would only draw level.
	b, st, g := hookFixture(t, -7202, 300, 400)
	st.RegisterPlayer(g.ChatID, 3, "Carol")
	got := b.hookFooter(g)
	if got != "\n\n⏭ <b>Carol</b> в 1 очке от 2-го места: хватит одной победы" {
		t.Errorf("hookFooter = %q", got)
	}
	if hasButton(b.resultKBBase(g.ID), "🔁 Реванш") {
		t.Errorf("no near miss, the button stays «Ещё одну»")
	}
}

func TestRematchLabelOnlyForTheWin(t *testing.T) {
	b, _, g := hookFixture(t, -7205, 300, 340, 344) // close for 2nd, not for the win
	if !strings.Contains(b.hookFooter(g), "До 2-го места") {
		t.Fatalf("want the 2nd-place near miss")
	}
	if hasButton(b.resultKBBase(g.ID), "🔁 Реванш") {
		t.Errorf("«Реванш» is for a lost win; a near miss for 2nd keeps «Ещё одну»")
	}
}

func TestHookFooterOff(t *testing.T) {
	b, st, g := hookFixture(t, -7203, 360, 366)
	if err := st.SetFeature(g.ChatID, featHook, false); err != nil {
		t.Fatal(err)
	}
	if got := b.hookFooter(g); got != "" {
		t.Errorf("switched off: hookFooter = %q", got)
	}
	if hasButton(b.resultKBBase(g.ID), "🔁 Реванш") {
		t.Errorf("switched off: the button stays «Ещё одну»")
	}
	if n, _ := st.CountEvents(g.ChatID, evHookShown); n != 0 {
		t.Errorf("switched off: nothing is logged, got %d", n)
	}
}

func TestHookFooterSkipsDuels(t *testing.T) {
	b, st, g := hookFixture(t, -7204, 360, 366)
	players, _ := st.ListPlayers(g.ChatID)
	if err := st.SetDuelTarget(g.ID, players[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := b.hookFooter(g); got != "" {
		t.Errorf("duels have their own rematch, hookFooter = %q", got)
	}
}

func hasButton(kb *tele.ReplyMarkup, text string) bool {
	for _, btn := range flatButtons(kb) {
		if btn.Text == text {
			return true
		}
	}
	return false
}
