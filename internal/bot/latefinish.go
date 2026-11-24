package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// featLateFinish credits a board solved after the result was recorded. The
// auto-record stops waiting a few minutes after the last finish (settleQuiet),
// so a slow solver was left a non-finisher. Now the bot keeps an eye on the
// room for a while: in a season game of the running season the solver becomes
// the next place (points, table and rating follow) and the result post is
// rewritten in place; elsewhere (a duel, where a non-finish already is the
// loss, or a closed season) it only says so under the result.
const featLateFinish = "latefinish"

// evLateFinish logs a late finish; player_id = who, detail = solve seconds.
const evLateFinish = "late_finish"

const (
	lateFinishPoll  = time.Minute
	lateFinishWatch = 20 * time.Minute
)

// lateFinish is a recorded non-finisher who has solved the board since.
type lateFinish struct {
	Nick     string
	PlayerID int64
	Secs     int64 // solve time from the round start; 0 = unknown
}

// lateFinishes picks the watched players (usdoku nick → player id) who have
// finished by now, in usdoku's finish order.
func lateFinishes(info *usdoku.GameInfo, waiting map[string]int64) []lateFinish {
	var out []lateFinish
	for _, p := range info.FinishOrder() {
		if id, ok := waiting[p.Name]; ok {
			out = append(out, lateFinish{Nick: p.Name, PlayerID: id, Secs: p.SolveSeconds(info.Info.StartedAt)})
		}
	}
	return out
}

// lateFinishText is the quiet line under the result. place > 0: the finish
// counted and the result above shows it. The name stays in the nominative and
// the verb belongs to the board: the bot knows neither the player's case nor
// gender.
func lateFinishText(name string, secs int64, place, points int) string {
	solved := "доска решена"
	if secs > 0 {
		solved += " за " + fmtDuration(int(secs))
	}
	note := "Очки и места не меняются"
	if place > 0 {
		note = fmt.Sprintf("Засчитано: %d-е место", place)
		if points > 0 {
			note += fmt.Sprintf(" · +%d", points)
		}
	}
	return fmt.Sprintf("⏱ <b>%s</b>: %s, уже после записи\n<i>%s</i>", esc(name), solved, note)
}

// unfinished maps the usdoku nicks of the game's recorded non-finishers to
// their player ids.
func (b *Bot) unfinished(game *storage.Game, info *usdoku.GameInfo) map[string]int64 {
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("lateFinish.results %d: %v", game.ID, err)
		return nil
	}
	dnf := map[int64]bool{}
	for _, r := range rows {
		if r.Rank == 0 {
			dnf[r.PlayerID] = true
		}
	}
	waiting := map[string]int64{}
	for _, p := range info.Players {
		if p.CompletedAt != nil {
			continue
		}
		if pl, err := b.st.PlayerByNick(game.ChatID, p.Name); err == nil && pl != nil && dnf[pl.ID] {
			waiting[p.Name] = pl.ID
		}
	}
	return waiting
}

// watchLateFinish polls a recorded game's room for lateFinishWatch and answers
// the result post when a recorded non-finisher solves the board after all.
// It stops as soon as the record is touched (reopened for an edit, deleted):
// the line describes the record as it was posted. Read-only towards the
// league: nothing but the event log is written.
// While it watches, the result's shared-goal line says the board is open
// until the window's end (lateWatched). There is no redraw when the window
// closes: "open until 21:40" stays true afterwards, and a redraw would lose
// the table moves of the original post.
func (b *Bot) watchLateFinish(game *storage.Game, info *usdoku.GameInfo, result *tele.Message) {
	code := game.UsdokuCode.String
	defer lateWatchDone(game.ID)
	if b.tb == nil || b.ud == nil || code == "" || !b.featureOn(game.ChatID, featLateFinish) {
		return
	}
	waiting := b.unfinished(game, info)
	deadline := time.Now().Add(lateFinishWatch)
	for len(waiting) > 0 && time.Now().Before(deadline) {
		time.Sleep(lateFinishPoll)
		if g, err := b.st.GameByID(game.ID); err != nil || g == nil || g.Deleted || g.Status != "completed" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cur, err := b.ud.Info(ctx, code)
		cancel()
		if err != nil {
			log.Printf("watchLateFinish %s: %v", code, err)
			continue
		}
		for _, lf := range lateFinishes(cur, waiting) {
			delete(waiting, lf.Nick)
			b.postLateFinish(game, lf, result)
		}
	}
}

var (
	lateMu      sync.Mutex
	lateWatched = map[int64]bool{} // games whose room watchLateFinish is (about to be) watching
)

// lateWatchArm marks a game as watched for a late finish. autoRecord arms it
// before rendering the result, so the first render already shows the open
// board; watchLateFinish clears it when it ends. A manual record or a bot
// restart never arms it: nobody would credit the board, so no line promises it.
func lateWatchArm(gameID int64) { lateMu.Lock(); lateWatched[gameID] = true; lateMu.Unlock() }

func lateWatchDone(gameID int64) { lateMu.Lock(); delete(lateWatched, gameID); lateMu.Unlock() }

func lateWatching(gameID int64) bool {
	lateMu.Lock()
	defer lateMu.Unlock()
	return lateWatched[gameID]
}

// boardOpen says whether a recorded game's board still waits for a late
// finish at now: someone was left a non-finisher, a late finish can count
// here (a season game of the running season, countable) and lateFinishWatch
// has not run out since completedAt (UTC, as stored). until is when the window
// closes, zero when the game does not qualify at all, so a caller can tell a
// window that is over from one that never opened.
func boardOpen(now time.Time, completedAt string, hasDNF, countable bool) (until time.Time, open bool) {
	if !hasDNF || !countable {
		return time.Time{}, false
	}
	at, err := parseDBTime(completedAt)
	if err != nil {
		return time.Time{}, false
	}
	until = at.Add(lateFinishWatch)
	return until, now.Before(until)
}

// boardState reads what boardOpen needs from the database: the completion
// stamp, whether a non-finisher is left, and whether a late finish can count
// (the mechanics are on, the record stands, a season game of the active
// season). Anything unreadable counts as not countable: the line stays out.
func (b *Bot) boardState(game *storage.Game) (completedAt string, hasDNF, countable bool) {
	if !lateWatching(game.ID) || !b.featureOn(game.ChatID, featLateFinish) || !b.featureOn(game.ChatID, featCoop) {
		return "", false, false
	}
	g, err := b.st.GameByID(game.ID)
	if err != nil || g == nil || g.Deleted || g.Status != "completed" || !g.CompletedAt.Valid {
		return "", false, false
	}
	if _, isDuel, err := b.st.DuelTargetID(g.ID); err != nil || isDuel {
		return "", false, false
	}
	if season, err := b.st.SeasonByID(g.SeasonID); err != nil || season == nil || season.Status != "active" {
		return "", false, false
	}
	rows, err := b.st.GameResults(g.ID)
	if err != nil {
		log.Printf("boardState.results %d: %v", g.ID, err)
		return "", false, false
	}
	for _, r := range rows {
		if r.Rank == 0 {
			hasDNF = true
			break
		}
	}
	return g.CompletedAt.String, hasDNF, true
}

// postLateFinish posts one late finish, unless the record no longer lists the
// player as a non-finisher (corrected by hand meanwhile). It holds
// recordingGames, so an edit tap cannot interleave with the credit.
func (b *Bot) postLateFinish(game *storage.Game, lf lateFinish, result *tele.Message) {
	defer recordingGames.Lock(game.ID)()
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("postLateFinish.results %d: %v", game.ID, err)
		return
	}
	for _, r := range rows {
		if r.PlayerID != lf.PlayerID || r.Rank != 0 {
			continue
		}
		place, points := b.creditLateFinish(game, lf, result)
		if _, err := b.tb.Send(tele.ChatID(game.ChatID), lateFinishText(r.Name, lf.Secs, place, points),
			quietReply(result)); err != nil {
			log.Printf("postLateFinish.send %d: %v", game.ID, err)
			return
		}
		b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: lf.PlayerID,
			Kind: evLateFinish, Detail: strconv.FormatInt(lf.Secs, 10)})
		return
	}
}

// errNotCredited: the game or the row no longer qualifies, nothing was written.
var errNotCredited = errors.New("late finish not credited")

// creditLateFinish makes the late solver the next place of a season game of
// the running season and rewrites the result post. Returns the new place and
// points; 0, 0 when the finish was not credited (a duel, a closed season, no
// post to rewrite, or an error).
func (b *Bot) creditLateFinish(game *storage.Game, lf lateFinish, result *tele.Message) (place, points int) {
	if result == nil {
		return 0, 0
	}
	if season, err := b.st.SeasonByID(game.SeasonID); err != nil || season == nil || season.Status != "active" {
		return 0, 0
	}
	text, seasonEnd, err := b.rescore(game, func(s *storage.Season) error {
		ok, err := b.st.CreditLateFinish(game.ID, lf.PlayerID, lf.Secs, s.PointsTable)
		if err == nil && !ok {
			err = errNotCredited
		}
		return err
	})
	if err != nil {
		if !errors.Is(err, errNotCredited) {
			log.Printf("creditLateFinish %d: %v", game.ID, err)
		}
		return 0, 0
	}
	log.Printf("⏱ game %d: late finish of player %d credited", game.ID, lf.PlayerID)
	if _, err := b.tb.Edit(result, autoResultHeader()+text+b.resultFooters(game), b.resultKB(game.ID)); err != nil {
		log.Printf("creditLateFinish.edit %d: %v", game.ID, err)
	}
	if seasonEnd != "" {
		_, _ = b.tb.Send(tele.ChatID(game.ChatID), seasonEnd)
		b.postSeasonPoster(game.ChatID, game.SeasonID)
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		return 0, 0
	}
	for _, r := range rows {
		if r.PlayerID == lf.PlayerID {
			return r.Rank, r.Points
		}
	}
	return 0, 0
}
