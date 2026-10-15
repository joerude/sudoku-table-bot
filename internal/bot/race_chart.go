package bot

import (
	"bytes"
	"fmt"
	"sort"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/chart"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// onRaceChart posts the season race as a picture (beta): cumulative points of
// every active player, game by game. A photo can't replace the text panel, so
// it goes out as its own silent message.
func (b *Bot) onRaceChart(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil || season == nil {
		return err
	}
	standings, err := b.st.Standings(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onRaceChart.standings", err)
	}
	ranks, err := b.st.SeasonRanks(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onRaceChart.ranks", err)
	}
	png, err := chart.RenderRace(raceChartData(season, standings, ranks))
	if err != nil {
		return b.fail(c, "onRaceChart.render", err)
	}
	photo := &tele.Photo{
		File:    tele.FromReader(bytes.NewReader(png)),
		Caption: fmt.Sprintf("📈 Гонка сезона %d <i>(бета)</i>", season.Number),
	}
	_ = c.Respond()
	return b.quiet(c, photo)
}

// raceChartData turns the season's results into chart lines: one per active
// player, coloured by player id so a colour sticks to a person whatever the
// standings do, with cumulative points after every season game.
func raceChartData(se *storage.Season, standings []storage.Standing, ranks []storage.SeasonRank) chart.Race {
	players := make([]storage.Standing, len(standings))
	copy(players, standings)
	sort.Slice(players, func(i, j int) bool { return players[i].PlayerID < players[j].PlayerID })

	var games []int64
	pts := map[int64]map[int64]int{} // game → player → points
	for _, r := range ranks {
		if pts[r.GameID] == nil {
			pts[r.GameID] = map[int64]int{}
			games = append(games, r.GameID)
		}
		pts[r.GameID][r.PlayerID] = r.Points
	}

	race := chart.Race{
		Title:  fmt.Sprintf("Сезон %d · гонка до %d", se.Number, se.Target),
		Target: se.Target,
	}
	for _, p := range players {
		line := make([]int, len(games))
		total := 0
		for i, g := range games {
			total += pts[g][p.PlayerID]
			line[i] = total
		}
		race.Series = append(race.Series, chart.Series{Name: p.Name, Points: line})
	}
	return race
}
