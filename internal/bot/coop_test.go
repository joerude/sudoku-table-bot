package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

var coopTiers66 = []int{13, 23, 33}

func TestCoopLines(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"result: plain", coopResultLine(13-1, coopTiers66), "🌟 Общая цель: 12/13"},
		{"result: first star", coopResultLine(13, coopTiers66), "🌟 Общая цель: первая звезда ⭐ · 13 досок без брошенных"},
		{"result: second star", coopResultLine(23, coopTiers66), "🌟 Общая цель: вторая звезда ⭐⭐ · 23 доски без брошенных"},
		{"result: between", coopResultLine(14, coopTiers66), "🌟 Общая цель: 14/23 ⭐"},
		{"result: all stars", coopResultLine(35, coopTiers66), "🌟 Общая цель: 35 досок ⭐⭐⭐"},
		{"live", coopLiveLine(12, coopTiers66), "🌟 Доиграют все: доска пойдёт в общую цель (12/13)"},
		{"live: all stars", coopLiveLine(35, coopTiers66), "🌟 Доиграют все: доска пойдёт в общую цель (35 досок)"},
		{"panel: none", coopPanelLine(5, coopTiers66), "🌟 Общая цель: 5 досок без брошенных · первая звезда на 13"},
		{"panel: some", coopPanelLine(13, coopTiers66), "🌟 Общая цель: 13 досок без брошенных ⭐ · следующая звезда на 23"},
		{"panel: all", coopPanelLine(35, coopTiers66), "🌟 Общая цель: 35 досок без брошенных ⭐⭐⭐"},
		{"nudge: none", coopNudgeLine(5, coopTiers66), "🌟 Общая цель: 5/13 · до первой звезды 8 досок"},
		{"nudge: one", coopNudgeLine(13, coopTiers66), "🌟 Общая цель: 13/23 ⭐ · до второй звезды 10 досок"},
		{"nudge: all", coopNudgeLine(35, coopTiers66), "🌟 Общая цель: 35 досок ⭐⭐⭐"},
		{"season: stars", coopSeasonResultLine(8, 27, coopTiers66), "🌟 Общая цель сезона 8: 27 досок ⭐⭐"},
		{"season: none", coopSeasonResultLine(8, 9, coopTiers66), "🌟 Общая цель сезона 8: 9 досок"},
		{"next season", coopNextSeasonLine(9, coopTiers66), "🌟 В сезоне 9 звёзды на 13, 23 и 33 доски без брошенных"},
		{"next season: one board", coopNextSeasonLine(2, []int{1, 2, 21}), "🌟 В сезоне 2 звёзды на 1, 2 и 21 доску без брошенных"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, c.got, c.want)
		}
		if strings.ContainsAny(c.got, "—–!") {
			t.Errorf("%s: no dashes or exclamation marks in bot texts: %q", c.name, c.got)
		}
	}
	for name, got := range map[string]string{
		"result": coopResultLine(3, nil), "live": coopLiveLine(3, nil), "panel": coopPanelLine(3, nil),
		"nudge": coopNudgeLine(3, nil), "season": coopSeasonResultLine(1, 3, nil), "next": coopNextSeasonLine(2, nil),
		"end": coopSeasonEnd(1, 3, nil, 2, nil),
	} {
		if got != "" {
			t.Errorf("%s without tiers must be empty, got %q", name, got)
		}
	}
	if got := coopSeasonEnd(8, 27, coopTiers66, 9, coopTiers66); strings.Count(got, "\n") != 1 {
		t.Errorf("season end block is two lines, got %q", got)
	}
}

func TestLiveSectionTextShowsCoopLine(t *testing.T) {
	snap := liveSnapshot{
		Joined:    []string{"Аня", "Боб"},
		Finishers: []liveFinisher{{Name: "Аня", Secs: 125}},
		Solving:   []string{"Боб"},
	}
	if got := liveSectionText(snap); strings.Contains(got, "🌟") {
		t.Errorf("no coop line set, got %q", got)
	}
	snap.Coop = coopLiveLine(12, coopTiers66)
	got := liveSectionText(snap)
	want := snap.Coop + "\n<i>Запишу сам через"
	if !strings.Contains(got, want) {
		t.Errorf("the coop line goes right above the auto-record note, got %q", got)
	}
}

func TestSeasonStatusBlockCoopLine(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		MinGames: 66,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true},
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, loc)
	coop := coopPanelLine(13, coopTiers66)
	got := seasonStatusBlock(se, 20, "2026-06-01", now, loc, coop)
	if !strings.HasSuffix(got, "\n"+coop) || strings.Index(got, "🎲") > strings.Index(got, "🌟") {
		t.Errorf("the coop line comes after the games counter, got %q", got)
	}
	if without := seasonStatusBlock(se, 20, "2026-06-01", now, loc, ""); strings.Contains(without, "🌟") {
		t.Errorf("no coop line, got %q", without)
	}
}

// coopGame records a finished seasonal game; quitter true leaves the second
// player as a DNF.
func coopGame(t *testing.T, st *storage.Store, chat, seasonID int64, table []int, a, b int64, quitter bool) *storage.Game {
	t.Helper()
	gid, err := st.CreatePendingGame(chat, seasonID, 1, "medium", "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	st.AddPick(gid, a)
	if quitter {
		st.AddDNF(gid, b)
	} else {
		st.AddPick(gid, b)
	}
	if err := st.FinalizeGame(gid, table); err != nil {
		t.Fatal(err)
	}
	g, err := st.GameByID(gid)
	if err != nil || g == nil {
		t.Fatalf("GameByID: %v", err)
	}
	return g
}

func TestCoopFooter(t *testing.T) {
	const chat = int64(-7601)
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	p2, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	b := &Bot{st: st}

	clean := coopGame(t, st, chat, se.ID, se.PointsTable, a.ID, p2.ID, false)
	if got := b.coopFooter(clean); got != "\n\n🌟 Общая цель: 1/13" {
		t.Errorf("a clean board: got %q", got)
	}
	coopGame(t, st, chat, se.ID, se.PointsTable, p2.ID, a.ID, false)
	if got := b.coopFooter(clean); !strings.HasPrefix(got, "\n\n🌟 Общая цель: ") {
		t.Errorf("a second clean board: got %q", got)
	}
	quit := coopGame(t, st, chat, se.ID, se.PointsTable, a.ID, p2.ID, true)
	if got := b.coopFooterAt(quit, time.Now().Add(lateFinishWatch+time.Minute)); got != "" {
		t.Errorf("a game with a quitter gets no line once its window is over, got %q", got)
	}
	st.SetFeature(chat, featCoop, false)
	if got := b.coopFooter(clean); got != "" {
		t.Errorf("switched off: got %q", got)
	}
}

func TestCoopOpenLine(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	until := time.Date(2026, 11, 24, 15, 40, 0, 0, time.UTC) // 21:40 at UTC+6
	const head = "🌟 Доска открыта до 21:40: доиграют все, и она пойдёт в общую цель · "
	cases := []struct {
		n    int
		want string
	}{
		{12, head + "12/13"},
		{14, head + "14/23 ⭐"},
		{35, head + "35 досок ⭐⭐⭐"},
	}
	for _, c := range cases {
		got := coopOpenLine(until, loc, c.n, coopTiers66)
		if got != c.want {
			t.Errorf("n=%d:\n got %q\nwant %q", c.n, got, c.want)
		}
		if strings.ContainsAny(got, "—–!") {
			t.Errorf("no dashes or exclamation marks in bot texts: %q", got)
		}
	}
	if got := coopOpenLine(until, loc, 3, nil); got != "" {
		t.Errorf("without tiers the line is empty, got %q", got)
	}
}

func TestCoopFooterOpenBoard(t *testing.T) {
	const chat = int64(-7602)
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	p2, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	b := &Bot{st: st}

	quit := coopGame(t, st, chat, se.ID, se.PointsTable, a.ID, p2.ID, true)
	done, err := parseDBTime(quit.CompletedAt.String)
	if err != nil {
		t.Fatal(err)
	}
	until := done.Add(lateFinishWatch)
	want := "\n\n" + coopOpenLine(until, loadLoc(b.chatTZ(chat)), 0, coopTiers66)
	if got := b.coopFooterAt(quit, done.Add(5*time.Minute)); got != "" {
		t.Errorf("no late-finish watch (manual record, restart): got %q", got)
	}
	lateWatchArm(quit.ID)
	defer lateWatchDone(quit.ID)
	if got := b.coopFooterAt(quit, done.Add(5*time.Minute)); got != want || !strings.Contains(got, "Доска открыта до ") {
		t.Errorf("inside the window: got %q, want %q", got, want)
	}
	if got := b.coopFooterAt(quit, until); got != "" {
		t.Errorf("window over: a game with a quitter gets no line, got %q", got)
	}
	st.SetFeature(chat, featLateFinish, false)
	if got := b.coopFooterAt(quit, done.Add(5*time.Minute)); got != "" {
		t.Errorf("late finish off, nobody will credit the board: got %q", got)
	}
	st.SetFeature(chat, featLateFinish, true)

	// The quitter solves the board in time: the board is clean and counts.
	if ok, err := st.CreditLateFinish(quit.ID, p2.ID, 600, se.PointsTable); err != nil || !ok {
		t.Fatalf("CreditLateFinish: ok=%v err=%v", ok, err)
	}
	if got := b.coopFooterAt(quit, done.Add(5*time.Minute)); got != "\n\n🌟 Общая цель: 1/13" {
		t.Errorf("credited: got %q", got)
	}

	// A closed season takes no late finish, so its board is never open.
	old := coopGame(t, st, chat, se.ID, se.PointsTable, a.ID, p2.ID, true)
	lateWatchArm(old.ID)
	defer lateWatchDone(old.ID)
	if _, err := st.CloseSeason(se, a.ID); err != nil {
		t.Fatal(err)
	}
	if got := b.coopFooterAt(old, done.Add(5*time.Minute)); got != "" {
		t.Errorf("archived season: got %q", got)
	}
}
