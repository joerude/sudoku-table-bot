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

// dropReplay forgets a game's samples (the watcher is done with the game).
func dropReplay(gameID int64) { takeReplay(gameID) }

// replayPlayer is one recorded participant of the game.
type replayPlayer struct {
	ID     int64
	Name   string
	Nick   string // usdoku nick, the key of progSample.solved
	Done   bool   // finished the board
	Finish int    // solve time in seconds; 0 = unknown or did not finish
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

// replayData turns the samples into chart lines, one per player in id order
// (the same colours as the season charts). A finisher's line ends on the
// finish line at their solve time; everybody else's runs to the last sample
// and is labelled with the cells they had. False when there is too little to
// draw.
func replayData(title, tag string, start time.Time, samples []progSample, players []replayPlayer) (chart.Replay, bool) {
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

	r := chart.Replay{Title: title, Tag: tag, Open: open, Step: replayStep}
	for _, p := range ps {
		timed := p.Done && p.Finish > 0
		ts, vs := []int{0}, []int{0}
		for _, s := range samples {
			t := int(s.at.Sub(start).Seconds())
			if t <= ts[len(ts)-1] || (timed && t >= p.Finish) {
				continue
			}
			ts, vs = append(ts, t), append(vs, min(s.solved[p.Nick], open))
		}
		until := end
		if timed {
			until = p.Finish
			ts, vs = append(ts, p.Finish), append(vs, open)
		}
		n := (until + replayStep - 1) / replayStep
		line := make([]int, n)
		for k := range line {
			line[k] = interpolate(ts, vs, (k+1)*replayStep)
		}
		name := clipRunes(p.Name, replayNameRunes)
		label := fmt.Sprintf("%s %d/%d", name, vs[len(vs)-1], open)
		switch {
		case timed:
			label = name + " " + fmtDuration(p.Finish)
		case p.Done:
			label = name // finished, the time is unknown
		}
		r.Series = append(r.Series, chart.Series{Name: p.Name, Points: line, Label: label})
	}
	return r, true
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
			Done: row.Rank > 0, Finish: row.Duration})
	}
	code := game.UsdokuCode.String
	replay, ok := replayData("Ход игры · "+code, gameTag(game.Difficulty.String, game.Mode.String),
		roundStart(info.Info.StartedAt, samples[0].at), samples, players)
	if !ok {
		return
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
