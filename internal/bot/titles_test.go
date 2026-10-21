package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestTitleLine(t *testing.T) {
	cases := []struct {
		a    domain.TitleAward
		want string
	}{
		{domain.TitleAward{Kind: domain.TitleGrowth, Score: 3}, "🏃 Рост недели: <b>Вика</b> (3 игры быстрее своей нормы)"},
		{domain.TitleAward{Kind: domain.TitleFinisher, Score: 9, Total: 9}, "🧱 Железный финиш: <b>Вика</b> (доиграно 9 из 9)"},
		{domain.TitleAward{Kind: domain.TitleHunter, Score: 2}, "🎯 Охотник: <b>Вика</b> (впереди лидера в 2 играх)"},
		{domain.TitleAward{Kind: domain.TitleHunter, Score: 21}, "🎯 Охотник: <b>Вика</b> (впереди лидера в 21 игре)"},
		{domain.TitleAward{Kind: domain.TitleBreaker, Score: 1}, "🧯 Стоп-кран: <b>Вика</b> (прервана серия побед)"},
		{domain.TitleAward{Kind: domain.TitleBreaker, Score: 2}, "🧯 Стоп-кран: <b>Вика</b> (прервано серий побед: 2)"},
		{domain.TitleAward{Kind: domain.TitleOwl, Score: 5}, "🦉 Сова недели: <b>Вика</b> (5 игр после 22:00)"},
		{domain.TitleAward{Kind: domain.TitleSprinter, Score: 252, Difficulty: "medium"}, "⚡ Спринтер недели: <b>Вика</b> (4:12 на Medium)"},
	}
	for _, c := range cases {
		if got := titleLine(c.a, "Вика"); got != c.want {
			t.Errorf("titleLine(%s) = %q\nwant %q", c.a.Kind, got, c.want)
		}
	}
	if got := titleLine(domain.TitleAward{Kind: domain.TitleOwl, Score: 3}, "<i>x</i>"); !strings.Contains(got, "&lt;i&gt;") {
		t.Errorf("names are escaped: %q", got)
	}
	if got := titleLine(domain.TitleAward{Kind: "unknown"}, "Вика"); got != "" {
		t.Errorf("an unknown title renders nothing: %q", got)
	}
}

func TestGroupTitleGames(t *testing.T) {
	rows := []storage.ResultRowSince{
		{GameID: 4, CompletedAt: "2026-10-13 10:00:00", Difficulty: "medium", Mode: "hardcore", PlayerID: 1, Rank: 1, Secs: 300},
		{GameID: 4, CompletedAt: "2026-10-13 10:00:00", Difficulty: "medium", Mode: "hardcore", PlayerID: 2, Rank: 0},
		{GameID: 7, CompletedAt: "2026-10-14 10:00:00", Duel: true, PlayerID: 2, Rank: 1, Secs: 280},
		{GameID: 9, CompletedAt: "junk", PlayerID: 2, Rank: 1},
	}
	got := groupTitleGames(rows)
	if len(got) != 2 {
		t.Fatalf("want 2 games (the unreadable one dropped), got %+v", got)
	}
	if len(got[0].Results) != 2 || got[0].Results[1].Rank != 0 || got[0].Duel || got[0].Difficulty != "medium" {
		t.Errorf("first game = %+v", got[0])
	}
	if !got[1].Duel || got[1].At.Format("2006-01-02 15:04") != "2026-10-14 10:00" {
		t.Errorf("second game = %+v", got[1])
	}
}

// titlesChat plays five games now: Alice wins in 250 s, Bob is 2nd in 300 s.
func titlesChat(t *testing.T, chat int64) (*Bot, *storage.Store) {
	t.Helper()
	st := openTempStore(t)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	bob, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	for i := 0; i < 5; i++ {
		gid, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		st.AddPickTimed(gid, a.ID, 250)
		st.AddPickTimed(gid, bob.ID, 300)
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatal(err)
		}
	}
	return &Bot{st: st}, st
}

func TestDigestCarriesTitles(t *testing.T) {
	b, _ := titlesChat(t, -7601)
	text, hasGames, err := b.buildWeeklyDigest(-7601, true)
	if err != nil || !hasGames {
		t.Fatalf("digest: %v %v", hasGames, err)
	}
	// Both finished 5 of 5; the tie goes to the lower id. Alice was also the
	// fastest, but holds a title already, so there is no sprinter this week.
	if !strings.Contains(text, "🏷 <b>Титулы недели</b>\n🧱 Железный финиш: <b>Alice</b> (доиграно 5 из 5)") {
		t.Errorf("digest misses the titles:\n%s", text)
	}
	if strings.Contains(text, "Спринтер недели: <b>Bob</b>") {
		t.Errorf("the runner-up never gets the sprinter title:\n%s", text)
	}
	// /weekly on any day shows a rolling week: titles belong to Monday's post.
	if text, _, _ := b.buildWeeklyDigest(-7601, false); strings.Contains(text, "Титулы недели") {
		t.Errorf("the /weekly preview must not hand out titles:\n%s", text)
	}
}

func TestDigestTitlesOff(t *testing.T) {
	b, st := titlesChat(t, -7602)
	st.SetFeature(-7602, featTitles, false)
	text, _, _ := b.buildWeeklyDigest(-7602, true)
	if strings.Contains(text, "Титулы недели") {
		t.Errorf("switched off, still there:\n%s", text)
	}
}
