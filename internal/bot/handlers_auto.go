package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	var state watchState
	for {
		g, err := b.st.GameByID(gameID)
		if err != nil {
			log.Printf("watchGame.game %s: %v", code, err)
			return
		}
		if g == nil || g.Status != "pending" || g.Deleted {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		info, err := b.ud.Info(ctx, code)
		cancel()
		if err != nil {
			log.Printf("watchGame.info %s: %v", code, err)
		} else if state.shouldRecord(time.Now(), len(info.FinishOrder()), len(info.Players), info.Finished()) {
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

// autoRecord maps usdoku players (by nickname) to registered players and records
// the finish order. It records only when the field was genuinely contested: at
// least two known players joined and every joined nick is recognised. A
// did-not-finish is fine — that player simply isn't in the finish order and
// scores 0. Otherwise it asks for a manual entry instead of guessing.
func (b *Bot) autoRecord(game *storage.Game, info *usdoku.GameInfo) {
	to := tele.ChatID(game.ChatID)

	// Map everyone who joined; collect unknown nicks and known player ids.
	mappedJoined := 0
	var unknown []string
	var joinedIDs []int64
	for _, p := range info.Players {
		pl, err := b.st.PlayerByNick(game.ChatID, p.Name)
		if err != nil {
			log.Printf("autoRecord.nick: %v", err)
		}
		if pl != nil {
			mappedJoined++
			joinedIDs = append(joinedIDs, pl.ID)
		} else {
			unknown = append(unknown, p.Name)
		}
	}

	// Finishers in order; map the known ones to player ids, keeping solve times.
	type pick struct {
		id, durSecs int64
	}
	var picks []pick
	var finisherNicks []string
	for _, p := range info.FinishOrder() {
		finisherNicks = append(finisherNicks, p.Name)
		if pl, _ := b.st.PlayerByNick(game.ChatID, p.Name); pl != nil {
			secs := p.SolveSeconds(info.Info.StartedAt)
			if secs == 0 {
				var comp int64
				if p.CompletedAt != nil {
					comp = *p.CompletedAt
				}
				log.Printf("⏱ no solve time for %s: startedAt=%d joinedAt=%d completedAt=%d",
					p.Name, info.Info.StartedAt, p.JoinedAt, comp)
			}
			picks = append(picks, pick{id: pl.ID, durSecs: secs})
		}
	}

	min := b.minPlayers(game.ChatID)

	// Too few known players actually joined — don't count it (anti-farming).
	if len(unknown) == 0 && mappedJoined < min {
		log.Printf("🤖 game %d not counted: mappedJoined=%d < min=%d", game.ID, mappedJoined, min)
		_, _ = b.tb.Send(to, fmt.Sprintf(
			"🚫 Игра не засчитана: участвовало меньше <b>%d</b> игроков (текущий минимум).\n"+
				"Изменить: /settings minplayers", min))
		return
	}

	if len(unknown) > 0 || len(picks) == 0 {
		log.Printf("🤖 game %d finished, finishers=%v unknown=%v mappedJoined=%d → manual",
			game.ID, finisherNicks, unknown, mappedJoined)
		_, _ = b.tb.Send(to, autoMappingText(finisherNicks, unknown), recordKeyboard(game.ID))
		if len(unknown) > 0 {
			if kb := claimNickKeyboard(game.ID, unknown); len(kb.InlineKeyboard) > 0 {
				_, _ = b.tb.Send(to,
					"Это твой ник? Привяжу его к тебе одним тапом:", kb)
			}
		}
		return
	}
	log.Printf("🤖 game %d auto-recording, order=%v", game.ID, finisherNicks)

	for _, pk := range picks {
		if err := b.st.AddPickTimed(game.ID, pk.id, pk.durSecs); err != nil {
			log.Printf("autoRecord.pick: %v", err)
		}
	}
	finished := make(map[int64]bool, len(picks))
	for _, pk := range picks {
		finished[pk.id] = true
	}
	for _, id := range joinedIDs {
		if !finished[id] {
			if err := b.st.AddDNF(game.ID, id); err != nil {
				log.Printf("autoRecord.dnf: %v", err)
			}
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

// onClaimNick binds an unrecognised usdoku nick to the tapping player, so future
// games auto-record for them. Re-attribution of the just-finished game is left to
// a manual /result if needed (keeps this a safe, single-write action).
func (b *Bot) onClaimNick(c tele.Context) error {
	data := c.Data()
	i := strings.IndexByte(data, ':')
	if i < 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял ник"})
	}
	nick := data[i+1:]
	sender := realSender(c)
	if sender == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не получилось определить тебя"})
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, sender.ID)
	if err != nil {
		return b.fail(c, "onClaimNick.player", err)
	}
	if player == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала /join"})
	}
	if err := b.st.SetNick(player.ID, nick); err != nil {
		return b.fail(c, "onClaimNick.set", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Готово: ник " + nick + " привязан"})
	return c.Send(fmt.Sprintf("✅ <b>%s</b> = usdoku <code>%s</code>. Дальше учёт сам.",
		esc(player.Name), esc(nick)))
}
