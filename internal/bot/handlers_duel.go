package bot

import (
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
	me := realSender(c)
	if me == nil {
		return c.Send(anonMsg)
	}
	caller, err := b.st.PlayerByTg(chatID, me.ID)
	if err != nil {
		return b.fail(c, "onDuel.caller", err)
	}
	if caller == nil {
		return c.Send("Сначала зарегистрируйся: /join")
	}
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		return b.fail(c, "onDuel.players", err)
	}
	var others []storage.Player
	for _, p := range players {
		if p.ID != caller.ID {
			others = append(others, p)
		}
	}
	if len(others) == 0 {
		return c.Send("Нет соперников. Пусть кто-то ещё сделает /join.")
	}
	difficulty, _ := parseNewGameArgs(c.Args())
	return c.Send("⚔️ Кого вызываешь на дуэль?", duelPickKeyboard(difficulty, others))
}

// onDuelPick issues the challenge: creates the room, tags the target, posts
// Accept/Decline. Only the target may answer.
func (b *Bot) onDuelPick(c tele.Context) error {
	difficulty, targetID := parseDuelPick(c.Data())
	if difficulty == "" || targetID == 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял выбор"})
	}
	me := realSender(c)
	if me == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
	}
	caller, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		return b.fail(c, "onDuelPick.caller", err)
	}
	if caller == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала /join"})
	}
	target, err := b.st.PlayerByID(targetID)
	if err != nil {
		return b.fail(c, "onDuelPick.target", err)
	}
	if target == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игрок не найден"})
	}
	room, err := b.createGameRoom(c, difficulty, "hardcore")
	if err != nil {
		return b.fail(c, "onDuelPick.room", err)
	}
	if room == nil {
		_ = c.Respond()
		return nil // a guard replied (e.g. game already pending)
	}
	if err := b.st.SetDuelTarget(room.gameID, targetID); err != nil {
		return b.fail(c, "onDuelPick.settarget", err)
	}
	nickWarn := !caller.UsdokuNick.Valid || caller.UsdokuNick.String == "" ||
		!target.UsdokuNick.Valid || target.UsdokuNick.String == ""
	_ = c.Respond()
	return c.Edit(duelChallengeText(caller.Name, *target, difficulty, room.code, nickWarn),
		duelKeyboard(room.gameID))
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
	_ = c.Respond()
	return c.Edit(duelAcceptText(*target, game.UsdokuCode.String), recordKeyboard(gameID))
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
	if err := b.st.SoftDeleteGame(gameID); err != nil {
		return b.fail(c, "onDecline.cancel", err)
	}
	_ = c.Respond()
	return c.Edit(duelDeclineText(*target))
}
