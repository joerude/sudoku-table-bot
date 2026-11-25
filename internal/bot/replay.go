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
	replayStep        = 10  // seconds per chart point (a divisor of 60)
	replayMinSamples  = 4   // fewer fetches: too little to draw
	replayMaxSamples  = 720 // a stuck room must not grow the slice forever
	replayNameRunes   = 12
	replayIdleAfter   = 180 // an empty board this long at the start is cut off the chart
	replayIdleCells   = 2   // cells on the whole board that still count as nobody playing
	replayFinishSlack = 120 // a finish this far past the last fetch is still believed
)

// progSample is one board fetch: when, and how many cells each usdoku nick
// had right (and, for the DNF line only, wrong).
type progSample struct {
	at     time.Time
	open   int
	solved map[string]int
	wrong  map[string]int
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
	replaySamples[gameID] = append(replaySamples[gameID], progSample{at: at, open: p.Open, solved: p.Solved, wrong: p.Wrong})
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
	Nick   string    // usdoku nick, the key of progSample.solved
	Rank   int       // recorded place; 0 = did not finish
	Finish int       // recorded solve time in seconds, as in the result post; 0 = unknown or did not finish
	Done   time.Time // when usdoku saw the board solved; zero = unknown
}

// roundWindow puts usdoku's startedAt (seconds or milliseconds, the unit is
// not documented) on the bot's clock and keeps the samples of that round.
// usdoku can restart a round in the same room: startedAt then moves past the
// first samples, which belong to the round before and are dropped (keeping
// them stretched the axis and pushed everybody onto a late start). When
// startedAt is missing or implausible the round is taken to have begun one
// fetch before the first sample.
func roundWindow(startedAt int64, samples []progSample) (time.Time, []progSample) {
	if len(samples) == 0 {
		return time.Time{}, nil
	}
	first, last := samples[0].at, samples[len(samples)-1].at
	if startedAt > 0 {
		t := usdokuTime(startedAt)
		if d := first.Sub(t); d >= 0 && d < 6*time.Hour {
			return t, samples
		}
		if t.After(first) && t.Before(last) {
			i := sort.Search(len(samples), func(i int) bool { return !samples[i].at.Before(t) })
			return t, samples[i:]
		}
	}
	return first.Add(-progressEvery), samples
}

// usdokuTime reads a usdoku timestamp, seconds or milliseconds (the unit is
// not documented).
func usdokuTime(v int64) time.Time {
	if v > 1e11 {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}

// idleLead is how much of the round's start to cut off: the room can sit
// with an empty board for long minutes before anybody plays. It is the last
// fetch of that empty stretch (a stray cell or two still counts as empty),
// or 0 when the stretch is shorter than replayIdleAfter. Everybody keeps one
// clock: shifting a single player's line made a race that never happened.
func idleLead(start time.Time, samples []progSample) int {
	lead := 0
	for _, s := range samples {
		total := 0
		for _, n := range s.solved {
			total += n
		}
		if total > replayIdleCells {
			break
		}
		lead = int(s.at.Sub(start).Seconds())
	}
	if lead < replayIdleAfter {
		return 0
	}
	return lead
}

// finishAt puts p's finish on the chart's clock (seconds after start), or 0
// when it can't be placed. usdoku's completion moment comes first: the
// recorded time can be counted from the player's own join, not from the
// round, and then it lands far from the line. lead is what idleLead cut off.
func finishAt(p replayPlayer, start time.Time, lead, end int) int {
	if p.Rank <= 0 {
		return 0
	}
	fits := func(f int) bool { return f > 0 && f <= end+replayFinishSlack }
	if !p.Done.IsZero() {
		if f := int(p.Done.Sub(start).Seconds()); fits(f) {
			return f
		}
	}
	if f := p.Finish - lead; p.Finish > 0 && fits(f) {
		return f
	}
	return 0
}

// replayData turns the samples into chart lines, one per player in id order
// (the same colours as the season charts), all on one clock that starts when
// the board stops being empty (see idleLead). A finisher's line ends on the
// finish line at their finish; everybody else's runs to the last sample. The
// standing beside it goes by place, then by cells. False when there is too
// little to draw.
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
	lead := idleLead(start, samples)
	if lead > 0 {
		start = start.Add(time.Duration(lead) * time.Second)
		end -= lead
	}
	ps := make([]replayPlayer, len(players))
	copy(ps, players)
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })

	r := chart.Replay{Open: open, Step: replayStep}
	cells := make([]int, len(ps))
	span := 0
	for i, p := range ps {
		fin := finishAt(p, start, lead, end)
		ts, vs := []int{0}, []int{0}
		for _, s := range samples {
			t := int(s.at.Sub(start).Seconds())
			if t <= ts[len(ts)-1] || (fin > 0 && t >= fin) {
				continue
			}
			ts, vs = append(ts, t), append(vs, min(s.solved[p.Nick], open))
		}
		until := end
		if fin > 0 {
			until = fin
			ts, vs = append(ts, until), append(vs, open)
		}
		span = max(span, until)
		line := make([]int, (until+replayStep-1)/replayStep)
		for k := range line {
			line[k] = interpolate(ts, vs, (k+1)*replayStep)
		}
		s := chart.Series{Name: p.Name, Points: line}
		if fin > 0 {
			s.Last = float64(until) / replayStep
		}
		r.Series = append(r.Series, s)

		cells[i] = vs[len(vs)-1]
		row := chart.ReplayRow{Slot: i, Place: p.Rank, Name: clipRunes(p.Name, replayNameRunes),
			Value: fmt.Sprintf("%d/%d", cells[i], open)}
		switch {
		case p.Rank > 0 && p.Finish > 0:
			row.Value = fmtDuration(p.Finish) // the recorded time, as in the result post
		case p.Rank > 0:
			row.Value = "финиш" // finished, the time is unknown
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
		rp := replayPlayer{ID: pl.ID, Name: pl.Name, Nick: p.Name, Rank: row.Rank, Finish: row.Duration}
		if p.CompletedAt != nil && *p.CompletedAt > 0 {
			rp.Done = usdokuTime(*p.CompletedAt)
		}
		players = append(players, rp)
	}
	code := game.UsdokuCode.String
	start, samples := roundWindow(info.Info.StartedAt, samples)
	replay, ok := replayData(start, loadLoc(b.chatTZ(game.ChatID)), samples, players)
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
