package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// featLateFinish credits a board solved after the result was recorded. The
// auto-record stops waiting a few minutes after the last finish (settleQuiet),
// and with the usual points table the last place scores what a non-finish
// does, so finishing late changed nothing and nobody saw it. Now the bot keeps
// an eye on the room for a while and says so under the result. It only says
// it: ranks, points and ratings stay as recorded.
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

// lateFinishText is the quiet line under the result. The name stays in the
// nominative and the verb belongs to the board: the bot knows neither the
// player's case nor gender.
func lateFinishText(name string, secs int64) string {
	solved := "доска решена"
	if secs > 0 {
		solved += " за " + fmtDuration(int(secs))
	}
	return fmt.Sprintf("🧱 <b>%s</b>: %s, уже после записи\n<i>Очки и места не меняются</i>", esc(name), solved)
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
func (b *Bot) watchLateFinish(game *storage.Game, info *usdoku.GameInfo, result *tele.Message) {
	code := game.UsdokuCode.String
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

// postLateFinish posts one late finish, unless the record no longer lists the
// player as a non-finisher (corrected by hand meanwhile).
func (b *Bot) postLateFinish(game *storage.Game, lf lateFinish, result *tele.Message) {
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("postLateFinish.results %d: %v", game.ID, err)
		return
	}
	for _, r := range rows {
		if r.PlayerID != lf.PlayerID || r.Rank != 0 {
			continue
		}
		if _, err := b.tb.Send(tele.ChatID(game.ChatID), lateFinishText(r.Name, lf.Secs), quietReply(result)); err != nil {
			log.Printf("postLateFinish.send %d: %v", game.ID, err)
			return
		}
		b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: lf.PlayerID,
			Kind: evLateFinish, Detail: strconv.FormatInt(lf.Secs, 10)})
		return
	}
}
