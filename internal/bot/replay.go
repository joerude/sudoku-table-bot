package bot

import (
	"bytes"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/chart"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// featReplay follows an auto-recorded result with «Ход игры»: a picture of how
// far every player was on the board, minute by minute. It is drawn from the
// board progress the watcher already fetches for the card's bars, so it costs
// usdoku nothing extra. The samples live in memory only: a restart mid-game or
// a result recorded by hand simply has no picture.
const featReplay = "replay"

// evReplayShown logs a posted replay picture.
const evReplayShown = "replay_shown"

const (
	replayStep       = 10  // seconds per chart point (a divisor of 60)
	replayMinSamples = 4   // fewer fetches: too little to draw
	replayMaxSamples = 720 // a stuck room must not grow the slice forever
	replayNameRunes  = 12
	replayLateAfter  = 180 // seconds with no cell: the player began later than the round
)

// progSample is one board fetch: when, and how many cells each usdoku nick
// had right.
type progSample struct {
	at     time.Time
	open   int
	solved map[string]int
}

var (
	replayMu      sync.Mutex
	replaySamples = map[int64][]progSample{} // gameID → samples of the running round
)

// addReplaySample keeps one board fetch of a watched game.
func addReplaySample(gameID int64, p usdoku.Progress, at time.Time) {
	replayMu.Lock()
	defer replayMu.Unlock()
	if len(replaySamples[gameID]) >= replayMaxSamples {
		return
	}
	replaySamples[gameID] = append(replaySamples[gameID], progSample{at: at, open: p.Open, solved: p.Solved})
}

// takeReplay hands over a game's samples and forgets them.
func takeReplay(gameID int64) []progSample {
	replayMu.Lock()
	defer replayMu.Unlock()
	s := replaySamples[gameID]
	delete(replaySamples, gameID)
	return s
}

// lastReplaySample is the newest board fetch of a watched game, left in place.
func lastReplaySample(gameID int64) (progSample, bool) {
	replayMu.Lock()
	defer replayMu.Unlock()
	s := replaySamples[gameID]
	if len(s) == 0 {
		return progSample{}, false
	}
	return s[len(s)-1], true
}

// dropReplay forgets a game's samples (the watcher is done with the game).
func dropReplay(gameID int64) { takeReplay(gameID) }

// replayPlayer is one recorded participant of the game.
type replayPlayer struct {
	ID     int64
	Name   string
	Nick   string // usdoku nick, the key of progSample.solved
	Rank   int    // recorded place; 0 = did not finish
	Finish int    // solve time in seconds from the round start; 0 = unknown or did not finish
}

// roundStart puts usdoku's startedAt (seconds or milliseconds, the unit is not
// documented) on the bot's clock. When it is missing or implausible the round
// is taken to have begun one fetch before the first sample.
func roundStart(startedAt int64, first time.Time) time.Time {
	if startedAt > 0 {
		t := time.Unix(startedAt, 0)
		if startedAt > 1e11 {
			t = time.UnixMilli(startedAt)
		}
		if d := first.Sub(t); d >= 0 && d < 6*time.Hour {
			return t
		}
	}
	return first.Add(-progressEvery)
}

// ownStart is when p began, in seconds from the round start. usdoku keeps no
// join time, so a player who had no cell for replayLateAfter or longer (came
// late, or was away) is taken to begin at the last fetch that still showed
// none; everybody else began with the round.
func ownStart(nick string, start time.Time, samples []progSample) int {
	from := 0
	for _, s := range samples {
		if s.solved[nick] > 0 {
			if from >= replayLateAfter {
				return from
			}
			return 0
		}
		from = int(s.at.Sub(start).Seconds())
	}
	return 0
}

// replayData turns the samples into chart lines, one per player in id order
// (the same colours as the season charts), each on the player's own clock (see
// ownStart). A finisher's line ends on the finish line at their solve time;
// everybody else's runs to the last sample. The standing beside it goes by
// place, then by cells. False when there is too little to draw.
func replayData(start time.Time, loc *time.Location, samples []progSample, players []replayPlayer) (chart.Replay, bool) {
	if len(samples) < replayMinSamples || len(players) < 2 {
		return chart.Replay{}, false
	}
	open := 0
	for _, s := range samples {
		open = max(open, s.open)
	}
	end := int(samples[len(samples)-1].at.Sub(start).Seconds())
	if open <= 0 || end < replayStep {
		return chart.Replay{}, false
	}
	ps := make([]replayPlayer, len(players))
	copy(ps, players)
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })

	r := chart.Replay{Open: open, Step: replayStep}
	cells := make([]int, len(ps))
	span := 0
	for i, p := range ps {
		from := ownStart(p.Nick, start, samples)
		timed := p.Rank > 0 && p.Finish > from
		ts, vs := []int{0}, []int{0}
		for _, s := range samples {
			t := int(s.at.Sub(start).Seconds())
			if t-from <= ts[len(ts)-1] || (timed && t >= p.Finish) {
				continue
			}
			ts, vs = append(ts, t-from), append(vs, min(s.solved[p.Nick], open))
		}
		until := end - from
		if timed {
			until = p.Finish - from
			ts, vs = append(ts, until), append(vs, open)
		}
		span = max(span, from+until)
		line := make([]int, (until+replayStep-1)/replayStep)
		for k := range line {
			line[k] = interpolate(ts, vs, (k+1)*replayStep)
		}
		s := chart.Series{Name: p.Name, Points: line}
		if timed {
			s.Last = float64(until) / replayStep
		}
		r.Series = append(r.Series, s)

		cells[i] = vs[len(vs)-1]
		row := chart.ReplayRow{Slot: i, Place: p.Rank, Name: clipRunes(p.Name, replayNameRunes),
			Value: fmt.Sprintf("%d/%d", cells[i], open)}
		switch {
		case timed:
			row.Value = fmtDuration(p.Finish) // the recorded time, as in the result post
		case p.Rank > 0:
			row.Value = "финиш" // finished, the time is unknown
		}
		if from > 0 {
			row.Note = fmt.Sprintf("с %d-й минуты", (from+30)/60)
		}
		r.Rows = append(r.Rows, row)
	}
	sort.SliceStable(r.Rows, func(i, j int) bool {
		a, b := r.Rows[i], r.Rows[j]
		if (a.Place > 0) != (b.Place > 0) {
			return a.Place > 0
		}
		if a.Place != b.Place {
			return a.Place < b.Place
		}
		return cells[a.Slot] > cells[b.Slot]
	})
	r.Meta = replayMeta(start.In(loc), span)
	return r, true
}

var ruWeekdays = [...]string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}

// replayMeta is the line under the replay's title: "Пн, 23 ноября · 09:13 ·
// 5 мин 12 с" (start in the chat's time, then how long the round ran).
func replayMeta(start time.Time, secs int) string {
	m, s := secs/60, secs%60
	dur := fmt.Sprintf("%d мин %d с", m, s)
	switch {
	case m == 0:
		dur = fmt.Sprintf("%d с", s)
	case m >= 10 || s == 0:
		dur = fmt.Sprintf("%d мин", m)
	}
	return fmt.Sprintf("%s, %d %s · %s · %s", ruWeekdays[start.Weekday()], start.Day(),
		ruMonthsGen[int(start.Month())-1], start.Format("15:04"), dur)
}

// interpolate reads the value at time t off the points (ts ascending): linear
// between two points, flat after the last one.
func interpolate(ts, vs []int, t int) int {
	for i := 1; i < len(ts); i++ {
		if t <= ts[i] {
			return vs[i-1] + (vs[i]-vs[i-1])*(t-ts[i-1])/(ts[i]-ts[i-1])
		}
	}
	return vs[len(vs)-1]
}

// postReplay answers an auto-recorded result with the «Ход игры» picture, as a
// silent photo. Best-effort and after the fact: the result is recorded and
// posted before this runs, so a failure only logs.
func (b *Bot) postReplay(game *storage.Game, info *usdoku.GameInfo, result *tele.Message, samples []progSample) {
	if b.tb == nil || len(samples) < replayMinSamples || !b.featureOn(game.ChatID, featReplay) {
		return
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("postReplay.results %d: %v", game.ID, err)
		return
	}
	recorded := make(map[int64]storage.ResultRow, len(rows))
	for _, r := range rows {
		recorded[r.PlayerID] = r
	}
	var players []replayPlayer
	for _, p := range info.Players {
		pl, err := b.st.PlayerByNick(game.ChatID, p.Name)
		if err != nil || pl == nil {
			continue
		}
		row, ok := recorded[pl.ID]
		if !ok {
			continue // a guest in a duel room, or not part of the record
		}
		players = append(players, replayPlayer{ID: pl.ID, Name: pl.Name, Nick: p.Name,
			Rank: row.Rank, Finish: row.Duration})
	}
	code := game.UsdokuCode.String
	replay, ok := replayData(roundStart(info.Info.StartedAt, samples[0].at),
		loadLoc(b.chatTZ(game.ChatID)), samples, players)
	if !ok {
		return
	}
	replay.Title, replay.Tag = "Ход игры · "+code, gameTag(game.Difficulty.String, game.Mode.String)
	if phrase := b.winnerPhrase(game.ChatID, rows); phrase != "" {
		replay.Quote, replay.By = phrase, rows[0].Name
	}
	png, err := chart.RenderReplay(replay)
	if err != nil {
		log.Printf("postReplay.render %d: %v", game.ID, err)
		return
	}
	photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(png)), Caption: "🧩 Ход игры · " + esc(code)}
	if _, err := b.tb.Send(tele.ChatID(game.ChatID), photo, quietReply(result)); err != nil {
		log.Printf("postReplay.send %d: %v", game.ID, err)
		return
	}
	b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, Kind: evReplayShown})
}
