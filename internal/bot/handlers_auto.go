package bot

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

const (
	watchInterval = 25 * time.Second
	// watchHotInterval is used while a round is live (players joined, not all
	// finished) so finish lines land in the chat within seconds.
	watchHotInterval = 7 * time.Second
	watchTimeout     = 3 * time.Hour
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
// While the round is live it polls faster and mirrors each finish into the
// game's chat post (liveUpdate).
func (b *Bot) watchGame(gameID, chatID int64, code string) {
	defer dropLive(gameID)
	markWatching(gameID)
	defer unmarkWatching(gameID)
	defer setRoomBusy(gameID, false)
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

		interval := watchInterval
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		info, err := b.ud.Info(ctx, code)
		cancel()
		if err != nil {
			log.Printf("watchGame.info %s: %v", code, err)
		} else {
			b.duelRoomSeen(g, info)
			if state.shouldRecord(time.Now(), len(info.FinishOrder()), len(info.Players), info.Finished()) {
				b.autoRecord(g, info)
				return
			}
			b.liveUpdate(gameID, info) // dedupes: edits only when the card text changes
			if len(info.Players) > 0 && !info.Finished() {
				interval = watchHotInterval
			}
		}

		if time.Now().After(deadline) {
			log.Printf("watchGame %s: gave up after %s", code, watchTimeout)
			return
		}
		time.Sleep(interval)
	}
}

// autoRecord maps usdoku players (by nickname) to registered players and records
// the finish order. It records only when the field was genuinely contested: at
// least two known players joined and every joined nick is recognised. A
// did-not-finish is fine — that player simply isn't in the finish order and
// scores 0. Otherwise it asks for a manual entry instead of guessing.
func (b *Bot) autoRecord(game *storage.Game, info *usdoku.GameInfo) {
	to := tele.ChatID(game.ChatID)

	// The watcher and a «Записать сейчас» tap can both get here: one at a time,
	// and the loser finds the game already closed.
	defer recordingGames.Lock(game.ID)()
	if cur, err := b.st.GameByID(game.ID); err != nil || cur == nil || cur.Status != "pending" || cur.Deleted {
		return
	}

	// A duel counts only its two participants — anyone else in the room is a
	// guest and must not end up in the duel's results (that skews every duel
	// stat and misnames the loser in the recent-duels log).
	pair, err := b.st.DuelPlayerIDs(game.ID)
	if err != nil {
		log.Printf("autoRecord.pair: %v", err)
	}
	isDuel := len(pair) == 2
	inPair := make(map[int64]bool, len(pair))
	for _, id := range pair {
		inPair[id] = true
	}

	// Map everyone who joined; collect unknown nicks and known player ids.
	mappedJoined := 0
	var unknown []string
	var joinedIDs []int64
	for _, p := range info.Players {
		pl, err := b.st.PlayerByNick(game.ChatID, p.Name)
		if err != nil {
			log.Printf("autoRecord.nick: %v", err)
		}
		if isDuel && (pl == nil || !inPair[pl.ID]) {
			continue // guest in a duel room — ignored, doesn't block auto-record
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
		pl, _ := b.st.PlayerByNick(game.ChatID, p.Name)
		if pl == nil || (isDuel && !inPair[pl.ID]) {
			continue
		}
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

	if isDuel {
		// Both duelists must be in the room (matched by nick) and at least
		// one must finish; otherwise a human decides.
		if mappedJoined < 2 || len(picks) == 0 {
			log.Printf("🤖 duel %d → manual: mappedJoined=%d picks=%d finishers=%v",
				game.ID, mappedJoined, len(picks), finisherNicks)
			b.cardNote(game.ID, game.ChatID, "⚔️ Не смог авто-записать дуэль, запишите результат вручную:",
				recordKeyboard(game.ID))
			return
		}
	} else {
		min := b.minPlayers(game.ChatID)

		// Too few known players actually joined — don't count it (anti-farming).
		if len(unknown) == 0 && mappedJoined < min {
			log.Printf("🤖 game %d not counted: mappedJoined=%d < min=%d", game.ID, mappedJoined, min)
			b.cardNote(game.ID, game.ChatID, notCountedText(min), notCountedKeyboard(game.ID))
			return
		}

		if len(unknown) > 0 || len(picks) == 0 {
			log.Printf("🤖 game %d finished, finishers=%v unknown=%v mappedJoined=%d → manual",
				game.ID, finisherNicks, unknown, mappedJoined)
			b.cardNote(game.ID, game.ChatID, autoMappingText(finisherNicks, unknown),
				recordAndClaimKeyboard(game.ID, unknown))
			return
		}
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
	if errors.Is(err, storage.ErrGameNotPending) {
		log.Printf("🤖 game %d already closed, skipping auto-post", game.ID)
		return
	}
	if err != nil {
		log.Printf("autoRecord.score: %v", err)
		b.cardNote(game.ID, game.ChatID, "⚠️ Не удалось авто-записать результат. Запишите вручную:",
			recordKeyboard(game.ID))
		return
	}
	result += b.ratingFooter(game)
	// The result is a NEW message (an edit would not notify anyone); the card
	// is retired to a one-liner pointing at it.
	if _, err := b.tb.Send(to, autoResultHeader()+result, b.resultKB(game.ID)); err != nil {
		log.Printf("autoRecord.send: %v", err)
	}
	b.closeCard(game.ID)
	if seasonEnd != "" {
		_, _ = b.tb.Send(to, seasonEnd)
	}
}

// onClaimNick binds an unrecognised usdoku nick to the tapping player, so future
// games auto-record for them. The claim button then disappears from the post and,
// once every nick in the room is known, the game is recorded right away.
func (b *Bot) onClaimNick(c tele.Context) error {
	data := c.Data()
	i := strings.IndexByte(data, ':')
	if i < 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял ник"})
	}
	gameID, nick := parseID(data[:i]), data[i+1:]
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
	claimed, err := b.st.ClaimNick(player.ID, nick)
	if err != nil {
		return b.fail(c, "onClaimNick.set", err)
	}
	if !claimed {
		text := "Ник " + nick + " уже занят"
		if owner, _ := b.st.PlayerByNick(c.Chat().ID, nick); owner != nil {
			text += ": " + owner.Name
		}
		return c.Respond(&tele.CallbackResponse{Text: text})
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Готово: ник " + nick + " привязан"})
	b.afterClaim(c, gameID)
	return nil
}

// afterClaim refreshes the fallback post after a claim: drop the claimed nick's
// button, or, when no unknown nick is left, record the game from fresh state.
func (b *Bot) afterClaim(c tele.Context, gameID int64) {
	game, err := b.st.GameByID(gameID)
	if err != nil || game == nil || game.Status != "pending" || game.Deleted ||
		!game.UsdokuCode.Valid || game.UsdokuCode.String == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.ud.Info(ctx, game.UsdokuCode.String)
	if err != nil {
		log.Printf("afterClaim.info %s: %v", game.UsdokuCode.String, err)
		return
	}
	unknown := b.unknownNicks(game.ChatID, info)
	if len(unknown) == 0 {
		b.autoRecord(game, info)
		return
	}
	if err := c.Edit(autoMappingText(finisherNicks(info), unknown),
		recordAndClaimKeyboard(gameID, unknown)); err != nil {
		log.Printf("afterClaim.edit: %v", err)
	}
}

// unknownNicks lists the room's usdoku nicks that match no registered player.
func (b *Bot) unknownNicks(chatID int64, info *usdoku.GameInfo) []string {
	var unknown []string
	for _, p := range info.Players {
		if pl, err := b.st.PlayerByNick(chatID, p.Name); err == nil && pl == nil {
			unknown = append(unknown, p.Name)
		}
	}
	return unknown
}

// finisherNicks is the room's finish order as usdoku nicks.
func finisherNicks(info *usdoku.GameInfo) []string {
	var nicks []string
	for _, p := range info.FinishOrder() {
		nicks = append(nicks, p.Name)
	}
	return nicks
}

// onRecordNow records the game from a fresh usdoku poll without waiting for the
// drain timer. The watcher notices the game is completed and exits; if it gets
// to autoRecord first, one of the two finds the game closed and does nothing.
func (b *Bot) onRecordNow(c tele.Context) error {
	game, err := b.st.GameByID(parseID(c.Data()))
	if err != nil {
		return b.fail(c, "onRecordNow.game", err)
	}
	if game == nil || game.Status != "pending" || game.Deleted {
		return c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
	}
	if !game.UsdokuCode.Valid || game.UsdokuCode.String == "" {
		return c.Respond(&tele.CallbackResponse{Text: "У игры нет комнаты usdoku"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.ud.Info(ctx, game.UsdokuCode.String)
	if err != nil {
		log.Printf("onRecordNow.info %s: %v", game.UsdokuCode.String, err)
		return c.Respond(&tele.CallbackResponse{Text: "usdoku не отвечает, попробуй ещё раз"})
	}
	if len(info.FinishOrder()) == 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Пока никто не финишировал"})
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Записываю…"})
	b.autoRecord(game, info)
	return nil
}
