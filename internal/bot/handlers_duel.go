package bot

import (
	"log"
	"strconv"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// onDuel asks who to challenge: buttons of every active player except the caller.
func (b *Bot) onDuel(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onDuel.ensure", err)
	}
	chatID := c.Chat().ID
	if !b.enoughPlayers(c, chatID) {
		return nil
	}
	difficulty, _ := parseNewGameArgs(c.Args())
	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil {
		return b.fail(c, "onDuel.pending", err)
	}
	if pending != nil { // don't offer opponents for a challenge that can't start
		return b.pendingConflict(c, pending, "duel:"+difficulty)
	}
	me := realSender(c)
	if me == nil {
		return b.ephemeral(c, anonMsg)
	}
	caller, err := b.st.PlayerByTg(chatID, me.ID)
	if err != nil {
		return b.fail(c, "onDuel.caller", err)
	}
	if caller == nil {
		return b.ephemeral(c, "Сначала зарегистрируйся: /join")
	}
	others, err := b.opponents(chatID, caller.ID)
	if err != nil {
		return b.fail(c, "onDuel.players", err)
	}
	if len(others) == 0 {
		return b.ephemeral(c, "Нет соперников. Пусть кто-то ещё сделает /join.")
	}
	return c.Send("⚔️ Кого вызываешь на дуэль?", duelPickKeyboard(difficulty, others, me.ID))
}

// opponents lists the chat's active players except the caller.
func (b *Bot) opponents(chatID, callerID int64) ([]storage.Player, error) {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		return nil, err
	}
	var others []storage.Player
	for _, p := range players {
		if p.ID != callerID {
			others = append(others, p)
		}
	}
	return others, nil
}

// onDuelPick issues the challenge: creates the room, tags the target, posts
// Accept/Decline. Only the target may answer.
func (b *Bot) onDuelPick(c tele.Context) error {
	difficulty, targetID, ownerTg := parseDuelPick(c.Data())
	if difficulty == "" || targetID == 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял выбор"})
	}
	me := realSender(c)
	if me == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
	}
	if ownerTg != 0 && me.ID != ownerTg {
		return c.Respond(&tele.CallbackResponse{Text: "Это не твой выбор"})
	}
	caller, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		return b.fail(c, "onDuelPick.caller", err)
	}
	if caller == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала /join"})
	}
	if targetID == caller.ID {
		return c.Respond(&tele.CallbackResponse{Text: "Нельзя вызвать самого себя"})
	}
	target, err := b.st.PlayerByID(targetID)
	if err != nil {
		return b.fail(c, "onDuelPick.target", err)
	}
	if target == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игрок не найден"})
	}
	posted, err := b.issueDuel(c, caller, target, difficulty)
	if err != nil || !posted {
		return err
	}
	// The picker only confirms; the challenge itself went out as a NEW message
	// (an edit would not notify the target).
	if err := c.Edit(duelSentText); err != nil {
		log.Printf("onDuelPick.edit: %v", err)
	}
	return nil
}

// issueDuel creates the room, tags the target and posts the challenge as a new
// message pinging the target. posted=false with a nil error means a guard
// already replied (e.g. a game is pending).
func (b *Bot) issueDuel(c tele.Context, caller, target *storage.Player, difficulty string) (posted bool, err error) {
	room, err := b.createGameRoom(c, difficulty, "hardcore", "duel:"+difficulty)
	if err != nil {
		return false, b.fail(c, "issueDuel.room", err)
	}
	if room == nil {
		_ = c.Respond()
		return false, nil // a guard replied (e.g. game already pending)
	}
	if err := b.st.SetDuelTarget(room.gameID, target.ID); err != nil {
		return false, b.fail(c, "issueDuel.settarget", err)
	}
	nickWarn := !caller.UsdokuNick.Valid || caller.UsdokuNick.String == "" ||
		!target.UsdokuNick.Valid || target.UsdokuNick.String == ""
	eloC, eloT, gainC, gainT, serr := b.duelStakes(c.Chat().ID, caller.ID, target.ID)
	if serr != nil {
		log.Printf("issueDuel.stakes: %v", serr)
		eloC = 0 // hide the stakes block, the challenge still posts
	}
	_ = c.Respond()
	if err := c.Send(duelChallengeText(caller.Name, *target, difficulty, room.code, nickWarn,
		eloC, eloT, gainC, gainT), duelKeyboard(room.gameID)); err != nil {
		return false, b.fail(c, "issueDuel.send", err)
	}
	return true, nil
}

// duelTargetGuard returns the duel's target player and whether the presser IS
// that target. It answers the callback itself on any rejection.
func (b *Bot) duelTargetGuard(c tele.Context, gameID int64) (*storage.Player, bool) {
	targetID, ok, err := b.st.DuelTargetID(gameID)
	if err != nil {
		_ = b.fail(c, "duelTargetGuard", err)
		return nil, false
	}
	if !ok {
		_ = c.Respond(&tele.CallbackResponse{Text: "Это не дуэль"})
		return nil, false
	}
	me := realSender(c)
	if me == nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
		return nil, false
	}
	presser, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		_ = b.fail(c, "duelTargetGuard.presser", err)
		return nil, false
	}
	if presser == nil || presser.ID != targetID {
		_ = c.Respond(&tele.CallbackResponse{Text: "Этот вызов не тебе ✋"})
		return nil, false
	}
	return presser, true
}

func (b *Bot) onAccept(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onAccept.game", err)
	}
	if game == nil || game.Status != "pending" {
		return c.Respond(&tele.CallbackResponse{Text: "Дуэль уже закрыта"})
	}
	target, ok := b.duelTargetGuard(c, gameID)
	if !ok {
		return nil
	}
	if err := b.st.MarkDuelAccepted(gameID); err != nil {
		log.Printf("onAccept.mark: %v", err) // stats-only stamp, never blocks the duel
	}
	_ = c.Respond()
	text := duelAcceptText(*target, game.UsdokuCode.String)
	kb := recordKeyboard(gameID)
	err = c.Edit(text, kb)
	if err == nil && game.UsdokuCode.String != "" {
		registerLive(gameID, c.Message(), text, kb)
	}
	b.replyToChallenger(c, game, duelAcceptReplyText)
	return err
}

// replyToChallenger answers the challenge post with a short reply that pings
// the challenger (edits of the post itself notify nobody). Best-effort.
func (b *Bot) replyToChallenger(c tele.Context, game *storage.Game, text func(storage.Player) string) {
	if !game.CreatedBy.Valid {
		return
	}
	challenger, err := b.st.PlayerByTg(game.ChatID, game.CreatedBy.Int64)
	if err != nil || challenger == nil {
		return
	}
	if err := c.Reply(text(*challenger)); err != nil {
		log.Printf("replyToChallenger: %v", err)
	}
}

// onDuelCancel dismisses the opponent picker (no game was created yet).
// Only whoever opened the picker may dismiss it.
func (b *Bot) onDuelCancel(c tele.Context) error {
	if owner, _ := strconv.ParseInt(c.Data(), 10, 64); owner != 0 {
		if me := realSender(c); me == nil || me.ID != owner {
			return c.Respond(&tele.CallbackResponse{Text: "Это не твой выбор"})
		}
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Отменено"})
	return c.Edit("✖️ Вызов отменён.")
}

// onDuels shows the all-time duel leaderboard.
func (b *Bot) onDuels(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onDuels.ensure", err)
	}
	text, err := b.duelsPanel(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onDuels.panel", err)
	}
	return c.Send(text)
}

func (b *Bot) onDecline(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDecline.game", err)
	}
	if game == nil || game.Status != "pending" {
		return c.Respond(&tele.CallbackResponse{Text: "Дуэль уже закрыта"})
	}
	target, ok := b.duelTargetGuard(c, gameID)
	if !ok {
		return nil
	}
	if err := b.st.MarkDuelDeclined(gameID); err != nil {
		log.Printf("onDecline.mark: %v", err) // the stamp tells a decline from a cancel
	}
	if err := b.st.SoftDeleteGame(gameID); err != nil {
		return b.fail(c, "onDecline.cancel", err)
	}
	_ = c.Respond()
	err = c.Edit(duelDeclineText(*target))
	b.replyToChallenger(c, game, duelDeclineReplyText)
	return err
}
