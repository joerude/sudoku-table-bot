package bot

import (
	"context"
	"log"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

const (
	watchInterval = 25 * time.Second
	watchTimeout  = 3 * time.Hour
)

// resumeWatches restarts pollers for pending games that have a usdoku code,
// so auto-record survives a bot restart.
func (b *Bot) resumeWatches() {
	games, err := b.st.PendingGamesWithCode()
	if err != nil {
		log.Printf("resumeWatches: %v", err)
		return
	}
	for _, g := range games {
		go b.watchGame(g.ID, g.ChatID, g.Code)
	}
	if len(games) > 0 {
		log.Printf("resumed %d usdoku game watch(es)", len(games))
	}
}

// watchGame polls usdoku for a game's result and auto-records it once finished.
// It stops if the game is recorded manually, deleted, or the deadline passes.
func (b *Bot) watchGame(gameID, chatID int64, code string) {
	deadline := time.Now().Add(watchTimeout)
	for {
		g, err := b.st.GameByID(gameID)
		if err != nil {
			log.Printf("watchGame.game %s: %v", code, err)
			return
		}
		if g == nil || g.Status != "pending" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		info, err := b.ud.Info(ctx, code)
		cancel()
		if err != nil {
			log.Printf("watchGame.info %s: %v", code, err)
		} else if info.Finished() {
			b.autoRecord(g, info)
			return
		}

		if time.Now().After(deadline) {
			log.Printf("watchGame %s: gave up after %s", code, watchTimeout)
			return
		}
		time.Sleep(watchInterval)
	}
}

// autoRecord maps usdoku finishers (by nickname) to registered players and
// records the result. If any finisher is unknown or fewer than two map, it asks
// for help instead of guessing.
func (b *Bot) autoRecord(game *storage.Game, info *usdoku.GameInfo) {
	order := info.FinishOrder()
	var ids []int64
	var nicks, unknown []string
	for _, p := range order {
		nicks = append(nicks, p.Name)
		pl, err := b.st.PlayerByNick(game.ChatID, p.Name)
		if err != nil {
			log.Printf("autoRecord.nick: %v", err)
		}
		if pl != nil {
			ids = append(ids, pl.ID)
		} else {
			unknown = append(unknown, p.Name)
		}
	}

	to := tele.ChatID(game.ChatID)
	if len(unknown) > 0 || len(ids) < minPlayers {
		_, _ = b.tb.Send(to, autoMappingText(nicks, unknown), recordKeyboard(game.ID))
		return
	}

	for _, id := range ids {
		if err := b.st.AddPick(game.ID, id); err != nil {
			log.Printf("autoRecord.pick: %v", err)
		}
	}
	result, seasonEnd, err := b.scoreAndCheck(game)
	if err != nil {
		log.Printf("autoRecord.score: %v", err)
		_, _ = b.tb.Send(to, "⚠️ Не удалось авто-записать результат. Запишите вручную:",
			recordKeyboard(game.ID))
		return
	}
	_, _ = b.tb.Send(to, autoResultHeader()+result, resultKeyboard(game.ID))
	if seasonEnd != "" {
		_, _ = b.tb.Send(to, seasonEnd)
	}
}
