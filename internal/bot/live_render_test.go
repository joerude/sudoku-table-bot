package bot

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func i64p(v int64) *int64 { return &v }

func TestBuildLiveSnapshotResolvesNamesInFinishOrder(t *testing.T) {
	info := &usdoku.GameInfo{
		Info: usdoku.Info{StartedAt: 1000},
		Players: []usdoku.Player{
			{Name: "bob", JoinedAt: 1000, CompletedAt: i64p(1300)},
			{Name: "ann", JoinedAt: 1000, CompletedAt: i64p(1200)},
			{Name: "cat", JoinedAt: 1000},
		},
	}
	snap := buildLiveSnapshot(info, func(n string) string { return strings.ToUpper(n) }, nil)
	if len(snap.Finishers) != 2 || snap.Finishers[0].Name != "ANN" || snap.Finishers[1].Name != "BOB" {
		t.Fatalf("finish order wrong: %+v", snap.Finishers)
	}
	if snap.Finishers[0].Secs != 200 {
		t.Errorf("secs = %d, want 200", snap.Finishers[0].Secs)
	}
}

func TestLiveSectionTextEmptyUntilSomeoneFinishes(t *testing.T) {
	if got := liveSectionText(liveSnapshot{}); got != "" {
		t.Errorf("no finishers must render nothing, got %q", got)
	}
}

func TestLiveKeyboardKeepsRSVPOnInviteCard(t *testing.T) {
	inv := liveKeyboard(7, cardKindInvite, false, 2)
	if len(inv.InlineKeyboard) != 1 || !strings.Contains(inv.InlineKeyboard[0][0].Text, "(2)") {
		t.Errorf("invite card must keep the RSVP button with the roster size: %+v", inv.InlineKeyboard)
	}
	plain := liveKeyboard(7, "", false, 0)
	if len(plain.InlineKeyboard) != 1 || plain.InlineKeyboard[0][0].Unique != cbRec {
		t.Errorf("plain card must offer the record button: %+v", plain.InlineKeyboard)
	}
}

func TestBuildLiveSnapshotJoinedAndSolving(t *testing.T) {
	info := &usdoku.GameInfo{
		Info: usdoku.Info{StartedAt: 1000},
		Players: []usdoku.Player{
			{Name: "ann", JoinedAt: 1000, CompletedAt: i64p(1200)},
			{Name: "bob", JoinedAt: 1000},
		},
	}
	snap := buildLiveSnapshot(info, func(n string) string { return n }, nil)
	if got := strings.Join(snap.Joined, ","); got != "ann,bob" {
		t.Errorf("joined = %q", got)
	}
	if got := strings.Join(snap.Solving, ","); got != "bob" {
		t.Errorf("solving = %q, want only the one without completedAt", got)
	}
}

func TestLiveSectionTextBeforeStartListsRoom(t *testing.T) {
	got := liveSectionText(liveSnapshot{Joined: []string{"Аня", "Боб"}})
	if got != "👀 В комнате: Аня, Боб" {
		t.Errorf("got %q", got)
	}
}

func TestLiveSectionTextDrainExplainsWhoAndWhen(t *testing.T) {
	got := liveSectionText(liveSnapshot{
		Joined:    []string{"Аня", "Боб"},
		Finishers: []liveFinisher{{Name: "Аня", Secs: 125}},
		Solving:   []string{"Боб"},
	})
	for _, want := range []string{"🥇 Аня", "⏱ 2:05", "⏳ Ещё решают: Боб", "~6 мин"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestLiveSectionTextEscapesNames(t *testing.T) {
	got := liveSectionText(liveSnapshot{Joined: []string{"<b>x</b>"}})
	if strings.Contains(got, "<b>x</b>") {
		t.Errorf("nick must be HTML-escaped: %q", got)
	}
}

func TestLiveSectionTextEveryoneDone(t *testing.T) {
	got := liveSectionText(liveSnapshot{
		Joined:    []string{"Аня"},
		Finishers: []liveFinisher{{Name: "Аня"}},
	})
	if strings.Contains(got, "Ещё решают") {
		t.Errorf("nobody is solving, got %q", got)
	}
}

func TestLiveKeyboardOffersRecordNowOnlyAfterAFinish(t *testing.T) {
	has := func(m *tele.ReplyMarkup, unique string) bool {
		for _, row := range m.InlineKeyboard {
			for _, b := range row {
				if b.Unique == unique {
					return true
				}
			}
		}
		return false
	}
	if has(liveKeyboard(7, "", false, 0), cbRecNow) {
		t.Error("no finisher yet: no «Записать сейчас»")
	}
	after := liveKeyboard(7, "", true, 0)
	if !has(after, cbRecNow) || !has(after, cbRec) {
		t.Errorf("after a finish both record buttons are expected: %+v", after.InlineKeyboard)
	}
	inv := liveKeyboard(7, cardKindInvite, true, 1)
	if !has(inv, cbJoinIn) || !has(inv, cbRecNow) {
		t.Errorf("invite card keeps RSVP and gains record-now: %+v", inv.InlineKeyboard)
	}
}

func TestNotCountedKeyboardOffersManualRecordAndCancel(t *testing.T) {
	m := notCountedKeyboard(9)
	if len(m.InlineKeyboard) != 1 || len(m.InlineKeyboard[0]) != 2 {
		t.Fatalf("want one row with two buttons: %+v", m.InlineKeyboard)
	}
	rec, del := m.InlineKeyboard[0][0], m.InlineKeyboard[0][1]
	if rec.Unique != cbRec || rec.Data != "9" || del.Unique != cbDel || del.Data != "9" {
		t.Errorf("buttons must reuse the record and delete callbacks: %+v %+v", rec, del)
	}
}

func TestNotCountedTextNamesTheMinimum(t *testing.T) {
	got := notCountedText(3)
	if !strings.Contains(got, "<b>3</b>") || !strings.Contains(got, "не засчитана") {
		t.Errorf("got %q", got)
	}
}

func TestAutoMappingTextDoesNotBlameMissingPlayers(t *testing.T) {
	got := autoMappingText(nil, nil)
	if strings.Contains(got, "минимум 2") {
		t.Errorf("all nicks are known, the old minimum-players line is a lie: %q", got)
	}
	if !strings.Contains(got, "не финишировал") {
		t.Errorf("must say nobody finished: %q", got)
	}
}

func TestAutoMappingTextListsUnknownNicks(t *testing.T) {
	got := autoMappingText([]string{"joe"}, []string{"max"})
	if !strings.Contains(got, "Не узнал ники: <b>max</b>") || !strings.Contains(got, "🥇 joe") {
		t.Errorf("got %q", got)
	}
}

func TestStaleNudgeText(t *testing.T) {
	plain := staleNudgeText("", "")
	if !strings.Contains(plain, "Результата нет, запишем?") || strings.Contains(plain, "usdoku.com") {
		t.Errorf("no room, no ping: %q", plain)
	}
	full := staleNudgeText("LOVI", `<a href="tg://user?id=1">Аня</a>`)
	if !strings.Contains(full, "https://www.usdoku.com/LOVI") || !strings.Contains(full, "Аня") {
		t.Errorf("room link and ping expected: %q", full)
	}
}

func TestWatchingRegistryCountsOverlappingWatchers(t *testing.T) {
	const id = int64(987654)
	if isWatching(id) {
		t.Fatal("unknown game must not be watched")
	}
	markWatching(id)
	markWatching(id)
	unmarkWatching(id)
	if !isWatching(id) {
		t.Fatal("one watcher is still running")
	}
	unmarkWatching(id)
	if isWatching(id) {
		t.Fatal("all watchers are gone")
	}
}

func TestBuildLiveSnapshotBarsForSolvingOnly(t *testing.T) {
	info := &usdoku.GameInfo{Players: []usdoku.Player{
		{Name: "ann", CompletedAt: i64p(1200)},
		{Name: "bob"},
		{Name: "cat"},
	}}
	prog := &usdoku.Progress{Open: 50, Solved: map[string]int{"ann": 50, "bob": 12, "cat": 30}}
	snap := buildLiveSnapshot(info, func(n string) string { return n }, prog)
	if len(snap.Bars) != 2 || snap.Bars[0] != (liveBar{"cat", 30}) || snap.Bars[1] != (liveBar{"bob", 12}) {
		t.Errorf("bars = %+v, want cat 30 then bob 12 (finisher ann left out)", snap.Bars)
	}
	if snap.Open != 50 {
		t.Errorf("open = %d", snap.Open)
	}
	if none := buildLiveSnapshot(info, func(n string) string { return n }, nil); none.Bars != nil {
		t.Errorf("no progress must give no bars: %+v", none.Bars)
	}
}

func TestLiveSectionTextBarsBeforeAndAfterFirstFinish(t *testing.T) {
	bars := []liveBar{{"Боб", 27}, {"<i>x</i>", 0}}
	got := liveSectionText(liveSnapshot{Joined: []string{"Боб", "<i>x</i>"}, Solving: []string{"Боб", "<i>x</i>"}, Bars: bars, Open: 54})
	for _, want := range []string{"🧩 <b>Решают:</b>", "[▰▰▰▰▰▱▱▱▱▱] 27/54 · Боб", "[▱▱▱▱▱▱▱▱▱▱] 0/54 · &lt;i&gt;x&lt;/i&gt;"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	got = liveSectionText(liveSnapshot{
		Joined: []string{"Аня", "Боб"}, Finishers: []liveFinisher{{Name: "Аня"}},
		Solving: []string{"Боб"}, Bars: []liveBar{{"Боб", 41}}, Open: 54,
	})
	for _, want := range []string{"🥇 Аня", "⏳ <b>Ещё решают:</b>\n[▰▰▰▰▰▰▰▱▱▱] 41/54 · Боб", "~6 мин"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}
