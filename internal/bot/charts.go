package bot

import (
	"bytes"
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

// chartBusyText is the toast for a second chart tap while one is being drawn.
const chartBusyText = "Уже рисую"

// drawingCharts serialises chart rendering per chat: a second tap while a chart
// is being drawn would only post a duplicate photo.
var drawingCharts keyLocks

// chartLock takes the chat's drawing lock. When another chart is in flight it
// answers the tap with chartBusyText and returns ok=false; otherwise call
// unlock when the photo is sent.
func (b *Bot) chartLock(c tele.Context) (unlock func(), ok bool) {
	unlock, ok = drawingCharts.TryLock(c.Chat().ID)
	if !ok {
		_ = c.Respond(&tele.CallbackResponse{Text: chartBusyText})
	}
	return unlock, ok
}

// chartStart answers the tap (so the button stops spinning and Telegram does not
// drop the answer) and shows «отправляет фото» while the picture is rendered.
// Called after the cheap guards, so their toasts can still show.
func (b *Bot) chartStart(c tele.Context) {
	b.logTap(c, tapChart, 0)
	_ = c.Respond()
	_ = b.tb.Notify(c.Chat(), tele.UploadingPhoto)
}

// sendPicture posts png (the tap is already answered by chartStart), or reports
// err: a toast would be lost by now, so the failure goes out as an ephemeral note.
func (b *Bot) sendPicture(c tele.Context, where string, png []byte, err error, caption string) error {
	if err != nil {
		log.Printf("%s: %v", where, err)
		return b.ephemeral(c, guardFailText)
	}
	return b.quiet(c, &tele.Photo{File: tele.FromReader(bytes.NewReader(png)), Caption: caption})
}

// onRaceChart posts the season race: cumulative points of every active player,
// game by game.
func (b *Bot) onRaceChart(c tele.Context) error {
	unlock, ok := b.chartLock(c)
	if !ok {
		return nil
	}
	defer unlock()
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onRaceChart.ensure", err)
	}
	if season == nil {
		return c.Respond()
	}
	standings, err := b.st.Standings(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onRaceChart.standings", err)
	}
	ranks, err := b.st.SeasonRanks(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onRaceChart.ranks", err)
	}
	b.chartStart(c)
	png, err := chart.RenderRace(raceChartData(season, standings, ranks))
	return b.sendPicture(c, "onRaceChart", png, err,
		fmt.Sprintf("📈 Гонка сезона %d", season.Number))
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
	won := map[int64]int64{}         // game → winner's player id
	for _, r := range ranks {
		if pts[r.GameID] == nil {
			pts[r.GameID] = map[int64]int{}
			games = append(games, r.GameID)
		}
		pts[r.GameID][r.PlayerID] = r.Points
		if r.Rank == 1 {
			won[r.GameID] = r.PlayerID
		}
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
	race.Winners = raceWinners(players, games, won)
	return race
}

// raceWinners maps each game to the chart line of its winner: the index of
// that player in players (the lines' order), -1 when nobody won it or the
// winner has no line (left the league).
func raceWinners(players []storage.Standing, games []int64, won map[int64]int64) []int {
	line := make(map[int64]int, len(players))
	for i, p := range players {
		line[p.PlayerID] = i
	}
	out := make([]int, len(games))
	for i, g := range games {
		out[i] = -1
		if id, ok := won[g]; ok {
			if l, ok := line[id]; ok {
				out[i] = l
			}
		}
	}
	return out
}

// featSeasonPic follows the season's own posts (last week, final day, season
// extended) with the season race picture: those posts already notify the
// chat, the picture rides along silently.
const featSeasonPic = "seasonpic"

// postSeasonRace sends the season race as a silent photo after a season post.
// Best-effort: the text is already out and nothing about the season depends
// on the picture, so a failure only logs.
func (b *Bot) postSeasonRace(season *storage.Season, standings []storage.Standing) {
	if b.tb == nil || !b.featureOn(season.ChatID, featSeasonPic) {
		return
	}
	ranks, err := b.st.SeasonRanks(season.ChatID, season.ID)
	if err != nil {
		log.Printf("postSeasonRace.ranks %d: %v", season.ID, err)
		return
	}
	png, err := chart.RenderRace(raceChartData(season, standings, ranks))
	if err != nil {
		log.Printf("postSeasonRace.render %d: %v", season.ID, err)
		return
	}
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(png)),
		Caption: fmt.Sprintf("🏆 Гонка сезона %d", season.Number)}
	if _, err := b.tb.Send(tele.ChatID(season.ChatID), photo, tele.Silent); err != nil {
		log.Printf("postSeasonRace.send %d: %v", season.ID, err)
	}
}

// featWeekPic follows the weekly digest (Monday's post and /weekly) with a
// picture of the week's race.
const featWeekPic = "weekpic"

// weekRaceMinGames is how many games the window needs: fewer draw a picture
// of almost flat lines, which says nothing the digest's text does not.
const weekRaceMinGames = 3

// weekRaceData is the race over the season games completed since `since`:
// every line starts at the player's points before the window and adds the
// window's games in completion order. games is how many fell into the window.
// Lines keep raceChartData's order (by player id), so colours match the
// season race.
func weekRaceData(se *storage.Season, standings []storage.Standing, ranks []storage.SeasonRank,
	since time.Time) (race chart.Race, games int) {
	type weekGame struct {
		id  int64
		at  time.Time
		pts map[int64]int
	}
	base := map[int64]int{}
	idx := map[int64]int{}
	won := map[int64]int64{} // game → winner's player id
	var week []weekGame
	for _, r := range ranks {
		at, err := parseDBTime(r.CompletedAt)
		if err != nil || at.Before(since) { // unreadable time: an old row, not this week's
			base[r.PlayerID] += r.Points
			continue
		}
		i, ok := idx[r.GameID]
		if !ok {
			i = len(week)
			idx[r.GameID] = i
			week = append(week, weekGame{id: r.GameID, at: at, pts: map[int64]int{}})
		}
		week[i].pts[r.PlayerID] = r.Points
		if r.Rank == 1 {
			won[r.GameID] = r.PlayerID
		}
	}
	sort.SliceStable(week, func(i, j int) bool {
		if !week[i].at.Equal(week[j].at) {
			return week[i].at.Before(week[j].at)
		}
		return week[i].id < week[j].id
	})

	players := make([]storage.Standing, len(standings))
	copy(players, standings)
	sort.Slice(players, func(i, j int) bool { return players[i].PlayerID < players[j].PlayerID })

	race = chart.Race{
		Title:  fmt.Sprintf("Сезон %d · гонка за неделю", se.Number),
		Target: se.Target,
		Fit:    true,
		XTitle: "игры за неделю",
	}
	for _, p := range players {
		total := base[p.PlayerID]
		line := make([]int, len(week))
		for i, g := range week {
			total += g.pts[p.PlayerID]
			line[i] = total
		}
		race.Series = append(race.Series, chart.Series{Name: p.Name, Base: base[p.PlayerID], Points: line})
	}
	ids := make([]int64, len(week))
	for i, g := range week {
		ids[i] = g.id
	}
	race.Winners = raceWinners(players, ids, won)
	return race, len(week)
}

// postWeekRace follows a weekly digest with the week's race as a silent
// photo. Best-effort: the digest's text is already out, a failure only logs.
func (b *Bot) postWeekRace(chatID int64) {
	if b.tb == nil || !b.featureOn(chatID, featWeekPic) {
		return
	}
	season, err := b.st.ActiveSeason(chatID)
	if err != nil {
		log.Printf("postWeekRace.season %d: %v", chatID, err)
		return
	}
	standings, err := b.st.Standings(chatID, season.ID)
	if err != nil {
		log.Printf("postWeekRace.standings %d: %v", chatID, err)
		return
	}
	ranks, err := b.st.SeasonRanks(chatID, season.ID)
	if err != nil {
		log.Printf("postWeekRace.ranks %d: %v", chatID, err)
		return
	}
	race, games := weekRaceData(season, standings, ranks, b.weekSince(season, time.Now()))
	if games < weekRaceMinGames {
		return
	}
	png, err := chart.RenderRace(race)
	if err != nil {
		log.Printf("postWeekRace.render %d: %v", chatID, err)
		return
	}
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(png)),
		Caption: fmt.Sprintf("🏆 Гонка недели · сезон %d", season.Number)}
	if _, err := b.tb.Send(tele.ChatID(chatID), photo, tele.Silent); err != nil {
		log.Printf("postWeekRace.send %d: %v", chatID, err)
	}
}

// onRatingChart posts the eternal Elo history of the active players.
func (b *Bot) onRatingChart(c tele.Context) error {
	unlock, ok := b.chartLock(c)
	if !ok {
		return nil
	}
	defer unlock()
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onRatingChart.ensure", err)
	}
	if season == nil {
		return c.Respond()
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
	b.chartStart(c)
	png, err := chart.RenderRating(ratingChartData(domain.ComputeRatingsActive(games, active), names, active))
	return b.sendPicture(c, "onRatingChart", png, err, "📈 Рейтинг Эло")
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
	unlock, ok := b.chartLock(c)
	if !ok {
		return nil
	}
	defer unlock()
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onCalendar.ensure", err)
	}
	if season == nil {
		return c.Respond()
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
	b.chartStart(c)
	png, err := chart.RenderCalendar(cal)
	return b.sendPicture(c, "onCalendar", png, err,
		fmt.Sprintf("📅 Игры сезона %d по дням", season.Number))
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
	unlock, ok := b.chartLock(c)
	if !ok {
		return nil
	}
	defer unlock()
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onPoster.ensure", err)
	}
	if season == nil {
		return c.Respond()
	}
	n, _ := strconv.Atoi(c.Data())
	se, err := b.st.SeasonByNumber(c.Chat().ID, n)
	if err != nil {
		return b.fail(c, "onPoster.season", err)
	}
	if se == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сезон не найден"})
	}
	b.chartStart(c)
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
	_ = b.tb.Notify(tele.ChatID(chatID), tele.UploadingPhoto)
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
	if in.duelName, _, in.duelWins, in.duelLosses, _, err = b.seasonDuelist(chatID, se.ID); err != nil {
		log.Printf("seasonPoster.duelist: %v", err)
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
	duelName    string // the season's duelist; empty when nobody earned the title
	duelWins    int
	duelLosses  int
	loc         *time.Location
}

// posterData builds the poster: standings[0] is the champion, awards are the
// same four the season-end post names.
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
	if in.duelName != "" {
		p.Awards = append(p.Awards, chart.Award{Title: "Дуэлянт",
			Value: fmt.Sprintf("%s · %d:%d", in.duelName, in.duelWins, in.duelLosses)})
	}
	p.Footer = fmt.Sprintf("%d %s", in.games, gamesWord(in.games))
	from, err1 := time.Parse("2006-01-02", in.first)
	to, err2 := time.Parse("2006-01-02", in.last)
	if err1 == nil && err2 == nil {
		p.Footer += fmt.Sprintf(" · %s → %s", fmtRuDate(from, in.loc), fmtRuDate(to, in.loc))
	}
	return p
}
