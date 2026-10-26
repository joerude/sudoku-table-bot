package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/chart"
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestRaceChartData(t *testing.T) {
	se := &storage.Season{Number: 6, Target: 100}
	// Standings come in rank order; the chart must order lines by player id.
	standings := []storage.Standing{{PlayerID: 2, Name: "Nur"}, {PlayerID: 1, Name: "mister"}, {PlayerID: 3, Name: "Joe"}}
	ranks := []storage.SeasonRank{
		{GameID: 10, PlayerID: 1, Rank: 1, Points: 3},
		{GameID: 10, PlayerID: 2, Rank: 2, Points: 1},
		{GameID: 11, PlayerID: 2, Rank: 1, Points: 3}, // Joe and mister missing: no points
		{GameID: 12, PlayerID: 3, Rank: 1, Points: 3},
		{GameID: 12, PlayerID: 1, Rank: 0, Points: 0},
	}
	race := raceChartData(se, standings, ranks)
	if race.Target != 100 || race.Title != "Сезон 6 · гонка до 100" {
		t.Errorf("title/target: %+v", race)
	}
	want := map[string][]int{"mister": {3, 3, 3}, "Nur": {1, 4, 4}, "Joe": {0, 0, 3}}
	order := []string{"mister", "Nur", "Joe"}
	if len(race.Series) != 3 {
		t.Fatalf("want 3 series, got %d", len(race.Series))
	}
	for i, s := range race.Series {
		if s.Name != order[i] {
			t.Errorf("series %d = %s, want %s (by player id)", i, s.Name, order[i])
		}
		for g, p := range want[s.Name] {
			if s.Points[g] != p {
				t.Errorf("%s after game %d = %d, want %d", s.Name, g+1, s.Points[g], p)
			}
		}
	}
}

func TestRatingChartData(t *testing.T) {
	r := domain.Ratings{PerGame: []domain.GameRating{
		{GameID: 1, NewRating: map[int64]int{1: 1032, 2: 968}},
		{GameID: 2, NewRating: map[int64]int{2: 990, 3: 1010}},  // 1 sits out, 3 joins
		{GameID: 3, NewRating: map[int64]int{1: 1040, 4: 1016}}, // 4 is inactive
	}}
	names := map[int64]string{1: "mister", 2: "Nur", 3: "Joe", 4: "Gone"}
	got := ratingChartData(r, names, map[int64]bool{1: true, 2: true, 3: true})
	if got.Games != 3 || len(got.Series) != 3 {
		t.Fatalf("want 3 games and 3 active series, got %d / %+v", got.Games, got.Series)
	}
	want := []struct {
		name   string
		offset int
		points []int
	}{
		{"mister", 0, []int{1032, 1032, 1040}}, // flat while sitting out
		{"Nur", 0, []int{968, 990, 990}},
		{"Joe", 1, []int{1010, 1010}}, // starts at its first game
	}
	for i, w := range want {
		s := got.Series[i]
		if s.Name != w.name || s.Offset != w.offset || s.Base != domain.StartRating || !equalInts(s.Points, w.points) {
			t.Errorf("series %d = %+v, want %s offset %d %v", i, s, w.name, w.offset, w.points)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCalendarData(t *testing.T) {
	loc := time.FixedZone("BISH", 6*3600)
	se := &storage.Season{Number: 6, MinGames: 66,
		Deadline: sql.NullString{Valid: true, String: "2026-11-20 18:00:00"}} // local midnight of Nov 21
	now := time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC)
	times := []string{
		"2026-09-21 16:00:00", // 22:00 local, Sep 21
		"2026-09-21 19:00:00", // 01:00 local, Sep 22
		"2026-10-14 11:00:00",
	}
	cal := calendarData(se, "2026-09-21 15:59:10", times, now, loc)
	if cal.Counts["2026-09-21"] != 1 || cal.Counts["2026-09-22"] != 1 || cal.Counts["2026-10-14"] != 1 {
		t.Errorf("counts must use local days: %v", cal.Counts)
	}
	if cal.Mark.Format("2006-01-02") != "2026-11-20" || cal.End.Format("2006-01-02") != "2026-11-20" {
		t.Errorf("last day: mark %v end %v, want Nov 20", cal.Mark, cal.End)
	}
	if !strings.Contains(cal.Subtitle, "3 из 66 игр") || !strings.Contains(cal.Subtitle, "нужно") {
		t.Errorf("subtitle: %q", cal.Subtitle)
	}
}

func TestPosterData(t *testing.T) {
	se := &storage.Season{Number: 5, Target: 100}
	standings := []storage.Standing{
		{PlayerID: 1, Name: "mister", Points: 100, Wins: 30},
		{PlayerID: 2, Name: "Nur", Points: 81, Wins: 22},
	}
	p := posterData(posterInput{
		season: se, standings: standings, games: 66, first: "2026-09-21", last: "2026-11-20",
		fastest: &storage.RecordRow{Name: "Nur", Secs: 221, Difficulty: "medium"},
		runName: "mister", run: 6, duelName: "Nur", duelWins: 4, duelLosses: 1, loc: time.UTC,
	})
	if p.Champion != "mister" || p.Points != 100 || len(p.Standings) != 2 {
		t.Errorf("champion/table: %+v", p)
	}
	want := []chart.Award{
		{Title: "Больше всех побед", Value: "mister · 30"},
		{Title: "Быстрейшее решение", Value: "Nur · 3:41 (medium)"},
		{Title: "Самая длинная серия побед", Value: "mister · 6 подряд"},
		{Title: "Дуэлянт", Value: "Nur · 4:1"},
	}
	if len(p.Awards) != len(want) {
		t.Fatalf("awards: %+v", p.Awards)
	}
	for i := range want {
		if p.Awards[i] != want[i] {
			t.Errorf("award %d = %+v, want %+v", i, p.Awards[i], want[i])
		}
	}
	if p.Footer != "66 игр · 21 сентября → 20 ноября" {
		t.Errorf("footer = %q", p.Footer)
	}
}
