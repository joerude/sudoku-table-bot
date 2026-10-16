package bot

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/chart"
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// Pictures (beta). Each one is rendered in memory on request and sent as a
// silent photo; nothing is stored. A photo can't replace the text panel it was
// asked from, so it always goes out as its own message.

// sendPicture answers the button press and posts png, or reports err.
func (b *Bot) sendPicture(c tele.Context, where string, png []byte, err error, caption string) error {
	if err != nil {
		return b.fail(c, where, err)
	}
	_ = c.Respond()
	return b.quiet(c, &tele.Photo{File: tele.FromReader(bytes.NewReader(png)), Caption: caption})
}

// onRaceChart posts the season race: cumulative points of every active player,
// game by game.
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
	return b.sendPicture(c, "onRaceChart", png, err,
		fmt.Sprintf("📈 Гонка сезона %d <i>(бета)</i>", season.Number))
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

// onRatingChart posts the eternal Elo history of the active players.
func (b *Bot) onRatingChart(c tele.Context) error {
	if season, err := b.ensure(c); err != nil || season == nil {
		return err
	}
	chatID := c.Chat().ID
	games, err := b.st.GamesForRating(chatID)
	if err != nil {
		return b.fail(c, "onRatingChart.games", err)
	}
	names, err := b.st.PlayerNames(chatID)
	if err != nil {
		return b.fail(c, "onRatingChart.names", err)
	}
	active := b.activePlayerSet(chatID)
	png, err := chart.RenderRating(ratingChartData(domain.ComputeRatingsActive(games, active), names, active))
	return b.sendPicture(c, "onRatingChart", png, err, "📈 Рейтинг Эло <i>(бета)</i>")
}

// ratingChartData draws one line per active player (all players when active is
// nil) from their first rated game, at the start rating, to now; between their
// games a line stays flat. x counts the chat's rated games.
func ratingChartData(r domain.Ratings, names map[int64]string, active map[int64]bool) chart.Rating {
	seen := map[int64]bool{}
	var ids []int64
	for _, g := range r.PerGame {
		for id := range g.NewRating {
			if !seen[id] && (active == nil || active[id]) {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := chart.Rating{
		Title:    "Рейтинг Эло",
		Subtitle: fmt.Sprintf("все игры за всё время, включая дуэли · старт %d", domain.StartRating),
		Games:    len(r.PerGame),
	}
	for _, id := range ids {
		s := chart.Series{Name: names[id], Base: domain.StartRating}
		cur, started := domain.StartRating, false
		for i, g := range r.PerGame {
			v, ok := g.NewRating[id]
			if !started {
				if !ok {
					continue
				}
				started, s.Offset = true, i
			}
			if ok {
				cur = v
			}
			s.Points = append(s.Points, cur)
		}
		out.Series = append(out.Series, s)
	}
	return out
}

// onCalendar posts the active season's days as a grid shaded by games played.
func (b *Bot) onCalendar(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil || season == nil {
		return err
	}
	chatID := c.Chat().ID
	started, err := b.st.SeasonStartedAt(season.ID)
	if err != nil {
		return b.fail(c, "onCalendar.started", err)
	}
	times, err := b.st.SeasonGameTimes(chatID, season.ID)
	if err != nil {
		return b.fail(c, "onCalendar.times", err)
	}
	cal := calendarData(season, started, times, time.Now(), loadLoc(b.chatTZ(chatID)))
	png, err := chart.RenderCalendar(cal)
	return b.sendPicture(c, "onCalendar", png, err,
		fmt.Sprintf("📅 Игры сезона %d по дням <i>(бета)</i>", season.Number))
}

// calendarData counts the season's games per local day and frames the grid
// from the season start to its last day (or today, before a deadline is set).
func calendarData(se *storage.Season, startedAt string, times []string, now time.Time, loc *time.Location) chart.Calendar {
	counts := map[string]int{}
	first := "" // UTC date of the first game, as actualPace wants it
	start := now
	if t, err := parseDBTime(startedAt); err == nil {
		start = t
	}
	for _, s := range times {
		t, err := parseDBTime(s)
		if err != nil {
			continue
		}
		counts[t.In(loc).Format("2006-01-02")]++
		if first == "" {
			first = t.UTC().Format("2006-01-02")
		}
		if t.Before(start) {
			start = t
		}
	}

	cal := chart.Calendar{
		Title:  fmt.Sprintf("Сезон %d · игры по дням", se.Number),
		Start:  start.In(loc),
		End:    now.In(loc),
		Today:  now.In(loc),
		Counts: counts,
	}
	if last, ok := seasonLastDay(se); ok && last.After(now) {
		cal.End, cal.Mark, cal.MarkLbl = last.In(loc), last.In(loc), "последний день"
	}

	games := len(times)
	parts := []string{fmt.Sprintf("%d из %d %s", games, se.MinGames, gamesWord(se.MinGames))}
	if games >= se.MinGames {
		parts = append(parts, "порог выполнен")
	} else {
		if p, ok := actualPace(games, first, now); ok {
			parts = append(parts, fmt.Sprintf("темп %.1f/день", p))
		}
		if p, ok := requiredPace(se, games, now); ok {
			parts = append(parts, fmt.Sprintf("нужно %.1f/день", p))
		}
	}
	cal.Subtitle = strings.Join(parts, " · ")
	return cal
}

// onPoster posts an archived season's poster on request.
func (b *Bot) onPoster(c tele.Context) error {
	if season, err := b.ensure(c); err != nil || season == nil {
		return err
	}
	n, _ := strconv.Atoi(c.Data())
	se, err := b.st.SeasonByNumber(c.Chat().ID, n)
	if err != nil {
		return b.fail(c, "onPoster.season", err)
	}
	if se == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сезон не найден"})
	}
	png, err := b.seasonPoster(c.Chat().ID, se)
	return b.sendPicture(c, "onPoster", png, err, fmt.Sprintf("🏆 Сезон %d", se.Number))
}

// postSeasonPoster follows a season-end post with the season's poster.
// Best-effort: a failure only logs, the text post already announced the end.
func (b *Bot) postSeasonPoster(chatID, seasonID int64) {
	if b.tb == nil {
		return
	}
	se, err := b.st.SeasonByID(seasonID)
	if err != nil || se == nil {
		log.Printf("postSeasonPoster.season %d: %v", seasonID, err)
		return
	}
	png, err := b.seasonPoster(chatID, se)
	if err != nil {
		log.Printf("postSeasonPoster.render %d: %v", seasonID, err)
		return
	}
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(png)), Caption: fmt.Sprintf("🏆 Сезон %d", se.Number)}
	if _, err := b.tb.Send(tele.ChatID(chatID), photo); err != nil {
		log.Printf("postSeasonPoster.send %d: %v", seasonID, err)
	}
}

// postBoard posts the puzzle's starting position when a round starts, as a
// silent reply to the game card. Best-effort: a failure only logs, the game
// goes on without it.
func (b *Bot) postBoard(g *storage.Game, code string) {
	if b.tb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	grid, err := b.ud.Board(ctx, code)
	if err != nil {
		log.Printf("postBoard.fetch %s: %v", code, err)
		return
	}
	tag := gameTag(g.Difficulty.String, g.Mode.String)
	png, err := chart.RenderBoard(chart.Board{Code: code, Tag: tag, Cells: grid})
	if err != nil {
		log.Printf("postBoard.render %s: %v", code, err)
		return
	}
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(png)),
		Caption: boardCaption(code, g.Difficulty.String, g.Mode.String)}
	opts := &tele.SendOptions{DisableNotification: true, AllowWithoutReply: true}
	if g.PostMsgID.Valid {
		opts.ReplyTo = &tele.Message{ID: int(g.PostMsgID.Int64), Chat: &tele.Chat{ID: g.ChatID}}
	}
	if _, err := b.tb.Send(tele.ChatID(g.ChatID), photo, opts); err != nil {
		log.Printf("postBoard.send %s: %v", code, err)
	}
}

// seasonPoster gathers a season's final table, race and awards and renders
// its poster.
func (b *Bot) seasonPoster(chatID int64, se *storage.Season) ([]byte, error) {
	standings, err := b.st.Standings(chatID, se.ID)
	if err != nil {
		return nil, err
	}
	ranks, err := b.st.SeasonRanks(chatID, se.ID)
	if err != nil {
		return nil, err
	}
	games, first, last, _, err := b.st.SeasonMeta(chatID, se.ID)
	if err != nil {
		return nil, err
	}
	in := posterInput{season: se, standings: standings, ranks: ranks, games: games, first: first, last: last,
		loc: loadLoc(b.chatTZ(chatID))}
	if in.fastest, err = b.st.FastestInSeason(chatID, se.ID); err != nil {
		log.Printf("seasonPoster.fastest: %v", err)
	}
	if in.runName, in.run, err = b.longestSeasonRun(chatID, se.ID); err != nil {
		log.Printf("seasonPoster.streak: %v", err)
	}
	return chart.RenderPoster(posterData(in))
}

// posterInput is everything a season poster shows.
type posterInput struct {
	season      *storage.Season
	standings   []storage.Standing
	ranks       []storage.SeasonRank
	games       int
	first, last string // UTC dates "2006-01-02" of the first and last game
	fastest     *storage.RecordRow
	runName     string
	run         int
	loc         *time.Location
}

// posterData builds the poster: standings[0] is the champion, awards are the
// same three the season-end post names.
func posterData(in posterInput) chart.Poster {
	p := chart.Poster{Season: in.season.Number, Race: raceChartData(in.season, in.standings, in.ranks)}
	for _, s := range in.standings {
		p.Standings = append(p.Standings, chart.PosterRow{Name: s.Name, Points: s.Points, Wins: s.Wins})
	}
	if len(in.standings) > 0 {
		p.Champion, p.Points = in.standings[0].Name, in.standings[0].Points
	}
	if name, wins, ok := uniqueTopWins(in.standings); ok {
		p.Awards = append(p.Awards, chart.Award{Title: "Больше всех побед", Value: fmt.Sprintf("%s · %d", name, wins)})
	}
	if f := in.fastest; f != nil {
		v := fmt.Sprintf("%s · %s", f.Name, fmtDuration(f.Secs))
		if f.Difficulty != "" {
			v += " (" + f.Difficulty + ")"
		}
		p.Awards = append(p.Awards, chart.Award{Title: "Быстрейшее решение", Value: v})
	}
	if in.run >= 2 {
		p.Awards = append(p.Awards, chart.Award{Title: "Самая длинная серия побед",
			Value: fmt.Sprintf("%s · %d подряд", in.runName, in.run)})
	}
	p.Footer = fmt.Sprintf("%d %s", in.games, gamesWord(in.games))
	from, err1 := time.Parse("2006-01-02", in.first)
	to, err2 := time.Parse("2006-01-02", in.last)
	if err1 == nil && err2 == nil {
		p.Footer += fmt.Sprintf(" · %s → %s", fmtRuDate(from, in.loc), fmtRuDate(to, in.loc))
	}
	return p
}
