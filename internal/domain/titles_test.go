package domain

import (
	"testing"
	"time"
)

var tz6 = time.FixedZone("UTC+6", 6*3600)

// since is the start of the titles week: Monday 12 Oct 2026, 10:00 local.
var since = time.Date(2026, 10, 12, 10, 0, 0, 0, tz6)

// g builds a seasonal medium/hardcore game finished at a local day/hour;
// results are (player, rank, secs) triples in finish order.
func g(day, hour int, res ...[3]int) TitleGame {
	out := TitleGame{At: time.Date(2026, 10, day, hour, 0, 0, 0, tz6).UTC(), SeasonID: 7, Difficulty: "medium", Mode: "hardcore"}
	for _, r := range res {
		out.Results = append(out.Results, TitleResult{PlayerID: int64(r[0]), Rank: r[1], Secs: r[2]})
	}
	return out
}

func input(games ...TitleGame) TitleInput {
	return TitleInput{Games: games, Since: since, Loc: tz6, Active: map[int64]bool{1: true, 2: true, 3: true}}
}

func only(t *testing.T, got []TitleAward, kind string) (TitleAward, bool) {
	t.Helper()
	for _, a := range got {
		if a.Kind == kind {
			return a, true
		}
	}
	return TitleAward{}, false
}

func TestTitleFinisher(t *testing.T) {
	var games []TitleGame
	for d := 13; d < 18; d++ {
		games = append(games, g(d, 14, [3]int{1, 1, 300}, [3]int{2, 0, 0}))
	}
	games = append(games, g(13, 15, [3]int{3, 1, 300}, [3]int{2, 2, 320}))
	a, ok := only(t, WeekTitles(input(games...)), TitleFinisher)
	if !ok || a.PlayerID != 1 || a.Score != 5 || a.Total != 5 {
		t.Errorf("finisher = %+v %v; want player 1, 5 of 5", a, ok)
	}

	short := input(games[:4]...)
	if _, ok := only(t, WeekTitles(short), TitleFinisher); ok {
		t.Errorf("four games are too few for the finisher title")
	}
}

func TestTitleHunter(t *testing.T) {
	old := func(day int) TitleGame { // last season: player 1 led nothing then
		x := g(day, 9, [3]int{3, 1, 300}, [3]int{1, 2, 310})
		x.SeasonID = 6
		return x
	}
	in := input(
		old(13), old(13),
		g(13, 14, [3]int{2, 1, 300}, [3]int{1, 2, 310}, [3]int{3, 3, 400}),
		g(14, 14, [3]int{2, 1, 300}, [3]int{1, 0, 0}),
		g(15, 14, [3]int{3, 1, 300}, [3]int{1, 2, 310}),
		g(16, 14, [3]int{1, 1, 300}, [3]int{2, 2, 310}),
	)
	in.LeaderID, in.SeasonID = 1, 7
	a, ok := only(t, WeekTitles(in), TitleHunter)
	if !ok || a.PlayerID != 2 || a.Score != 2 {
		t.Errorf("hunter = %+v %v; want player 2 ahead of the leader twice", a, ok)
	}
	in.LeaderID = 0
	if _, ok := only(t, WeekTitles(in), TitleHunter); ok {
		t.Errorf("no leader, no hunter")
	}
}

func TestTitleBreaker(t *testing.T) {
	in := input(
		// Before the week: player 1 wins three in a row, player 3 stops it.
		g(5, 14, [3]int{1, 1, 300}, [3]int{2, 2, 310}),
		g(6, 14, [3]int{1, 1, 300}, [3]int{2, 2, 310}),
		g(7, 14, [3]int{1, 1, 300}, [3]int{3, 2, 310}),
		g(8, 14, [3]int{3, 1, 300}, [3]int{1, 2, 310}),
		// Player 1 builds another run that carries into the week...
		g(9, 14, [3]int{1, 1, 300}, [3]int{2, 2, 310}),
		g(10, 14, [3]int{1, 1, 300}, [3]int{2, 2, 310}),
		g(13, 14, [3]int{1, 1, 300}, [3]int{2, 2, 310}),
		// ...and player 2 stops it inside the week.
		g(14, 14, [3]int{2, 1, 300}, [3]int{1, 2, 310}),
	)
	a, ok := only(t, WeekTitles(in), TitleBreaker)
	if !ok || a.PlayerID != 2 || a.Score != 1 {
		t.Errorf("breaker = %+v %v; want player 2 with one stopped streak", a, ok)
	}

	duel := g(14, 14, [3]int{2, 1, 300}, [3]int{1, 2, 310})
	duel.Duel = true
	in.Games[len(in.Games)-1] = duel
	if _, ok := only(t, WeekTitles(in), TitleBreaker); ok {
		t.Errorf("a duel does not stop a season win streak")
	}
}

func TestTitleOwl(t *testing.T) {
	in := input(
		g(13, 22, [3]int{3, 1, 300}),
		g(14, 23, [3]int{3, 0, 0}, [3]int{1, 1, 300}),
		g(16, 1, [3]int{3, 2, 320}, [3]int{1, 1, 300}),
		g(17, 21, [3]int{3, 1, 300}),
	)
	got := WeekTitles(in)
	a, ok := only(t, got, TitleOwl)
	if !ok || a.PlayerID != 3 || a.Score != 3 {
		t.Errorf("owl = %+v %v; want player 3 with three late games", a, ok)
	}
}

func TestTitleSprinter(t *testing.T) {
	easy := g(15, 14, [3]int{3, 1, 100})
	easy.Difficulty = "easy"
	in := input(
		g(13, 14, [3]int{1, 1, 250}, [3]int{2, 2, 260}),
		g(14, 14, [3]int{2, 1, 240}, [3]int{1, 2, 255}),
		easy,
	)
	a, ok := only(t, WeekTitles(in), TitleSprinter)
	if !ok || a.PlayerID != 2 || a.Score != 240 || a.Difficulty != "medium" {
		t.Errorf("sprinter = %+v %v; want player 2, 4:00 at medium (the week's usual board)", a, ok)
	}
}

func TestTitleSprinterTie(t *testing.T) {
	in := input(
		g(13, 14, [3]int{2, 1, 240}, [3]int{1, 2, 260}),
		g(14, 14, [3]int{3, 1, 240}, [3]int{1, 2, 255}),
	)
	in.PaceWins = map[int64]int{2: 2} // player 2 takes growth first
	a, ok := only(t, WeekTitles(in), TitleSprinter)
	if !ok || a.PlayerID != 3 || a.Score != 240 {
		t.Errorf("sprinter = %+v %v; the tie at 4:00 goes to player 3, who has no title yet", a, ok)
	}
}

func TestTitleGrowth(t *testing.T) {
	in := input(g(13, 14, [3]int{1, 1, 300}, [3]int{3, 2, 310}))
	in.PaceWins = map[int64]int{3: 3, 2: 1}
	a, ok := only(t, WeekTitles(in), TitleGrowth)
	if !ok || a.PlayerID != 3 || a.Score != 3 {
		t.Errorf("growth = %+v %v; want player 3 with three pace games", a, ok)
	}
	in.PaceWins = map[int64]int{3: 1}
	if _, ok := only(t, WeekTitles(in), TitleGrowth); ok {
		t.Errorf("one pace game is not a title yet")
	}
}

func TestWeekTitlesGoToDifferentPlayers(t *testing.T) {
	var games []TitleGame
	for d := 13; d < 18; d++ { // player 1 finishes everything, fastest too
		games = append(games, g(d, 14, [3]int{1, 1, 240}, [3]int{2, 2, 300}, [3]int{4, 3, 330}))
	}
	in := input(games...)
	in.PaceWins = map[int64]int{1: 2}
	got := WeekTitles(in)
	holders := map[int64]int{}
	for _, a := range got {
		holders[a.PlayerID]++
		if a.PlayerID == 4 {
			t.Errorf("player 4 left the league: no title, got %+v", a)
		}
	}
	for p, n := range holders {
		if n > 1 {
			t.Errorf("player %d holds %d titles; one each", p, n)
		}
	}
	if a, ok := only(t, got, TitleGrowth); !ok || a.PlayerID != 1 {
		t.Errorf("growth comes first and goes to player 1: %+v", got)
	}
	if _, ok := only(t, got, TitleSprinter); ok {
		t.Errorf("the sprinter title is player 1's or nobody's, never the runner-up's: %+v", got)
	}
	if a, ok := only(t, got, TitleFinisher); !ok || a.PlayerID != 2 {
		t.Errorf("finisher: player 2 also finished 5 of 5, so it is theirs: %+v", got)
	}
	if len(got) > 3 {
		t.Errorf("at most three titles a week, got %d", len(got))
	}
}
