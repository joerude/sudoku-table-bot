package bot

import (
	"log"
	"strconv"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
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
	kb := duelPickKeyboard(difficulty, others, me.ID)
	if c.Callback() != nil { // from the /play hub: turn the hub into the picker
		return c.Edit("⚔️ Кого вызываешь на дуэль?", kb)
	}
	return c.Send("⚔️ Кого вызываешь на дуэль?", kb)
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

// duelChallengeTTLMinutes is how long a challenge waits for an answer before it
// burns out. A pending game blocks the whole league, so it cannot wait forever.
const duelChallengeTTLMinutes = 20

// closedChallengeToast answers a press on a challenge that was already settled.
const closedChallengeToast = "Вызов уже закрыт"

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
		return c.Respond(&tele.CallbackResponse{Text: guardJoinFirst})
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
	// The picker is the «Создаю комнату…» notice and ends as a confirmation; the
	// challenge itself goes out as a NEW message (an edit would not notify the target).
	_, err = b.issueDuel(c, caller, target, difficulty, placeAuto)
	return err
}

// issueDuel creates the room, tags the target and posts the challenge as a new
// message pinging the target; that message becomes the game's card. The notice
// shown while the room is made (the pressed message for placeAuto, a fresh one
// for placeNew) ends as «Вызов отправлен». A button whose message must survive
// (a result post) passes placeNew. posted=false with a nil error means a guard
// already replied (e.g. a game is pending).
func (b *Bot) issueDuel(c tele.Context, caller, target *storage.Player, difficulty string, place placement) (posted bool, err error) {
	room, err := b.createGameRoomAt(c, difficulty, "hardcore", "duel:"+difficulty, place)
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
	ratC, ratT, gainC, gainT, serr := b.duelStakes(c.Chat().ID, caller.ID, target.ID)
	if serr != nil {
		log.Printf("issueDuel.stakes: %v", serr)
		ratC = 0 // hide the stakes block, the challenge still posts
	}
	_ = c.Respond()
	chatID := c.Chat().ID
	sent, err := b.tb.Send(c.Chat(), duelChallengeText(caller.Name, *target, difficulty, room.code, nickWarn,
		ratC, ratT, gainC, gainT), duelKeyboard(room.gameID))
	if err != nil {
		return false, b.fail(c, "issueDuel.send", err)
	}
	// The challenge post is the game's card. Its base text stays empty: live
	// edits must not touch the buttons before the target answers (accept turns
	// it into the live card). saveCard also replaces an anchor the watcher may
	// have cached while the notice was still the card.
	b.saveCard(room.gameID, chatID, int64(sent.ID), "", "")
	if room.msg != nil {
		if err := b.editPost(chatID, int64(room.msg.ID), duelSentText, nil); err != nil {
			log.Printf("issueDuel.sent: %v", err)
		}
	}
	return true, nil
}

// onRematch challenges the other duelist again, straight from a duel result
// post. Payload: "<difficulty>:<playerAID>:<playerBID>"; only those two may press.
func (b *Bot) onRematch(c tele.Context) error {
	difficulty, aID, bID := parseRematch(c.Data())
	if difficulty == "" {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял выбор"})
	}
	me := realSender(c)
	if me == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
	}
	caller, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		return b.fail(c, "onRematch.caller", err)
	}
	if caller == nil || (caller.ID != aID && caller.ID != bID) {
		return c.Respond(&tele.CallbackResponse{Text: "Реванш просят только участники дуэли"})
	}
	otherID := aID
	if caller.ID == aID {
		otherID = bID
	}
	target, err := b.st.PlayerByID(otherID)
	if err != nil {
		return b.fail(c, "onRematch.target", err)
	}
	if target == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игрок не найден"})
	}
	// The result post stays as it is: the rematch notice and challenge are new messages.
	_, err = b.issueDuel(c, caller, target, difficulty, placeNew)
	return err
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
	if game == nil || game.Status != "pending" || game.Deleted {
		return c.Respond(&tele.CallbackResponse{Text: closedChallengeToast})
	}
	target, ok := b.duelTargetGuard(c, gameID)
	if !ok {
		return nil
	}
	won, err := b.st.AcceptDuel(gameID)
	if err != nil {
		return b.fail(c, "onAccept.mark", err)
	}
	if !won { // revoked, declined or expired a moment ago
		return c.Respond(&tele.CallbackResponse{Text: closedChallengeToast})
	}
	_ = c.Respond()
	text := duelAcceptText(*target, game.UsdokuCode.String)
	kb := recordKeyboard(gameID)
	err = c.Edit(text, kb)
	if err == nil && game.UsdokuCode.String != "" {
		registerLive(gameID, c.Message(), text)
	}
	b.replyToChallenger(c, game, duelAcceptReplyText)
	return err
}

// onRevoke lets the challenger withdraw a challenge nobody has answered yet.
func (b *Bot) onRevoke(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onRevoke.game", err)
	}
	if game == nil || game.Status != "pending" || game.Deleted {
		return c.Respond(&tele.CallbackResponse{Text: closedChallengeToast})
	}
	me := realSender(c)
	if me == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
	}
	if !game.CreatedBy.Valid || game.CreatedBy.Int64 != me.ID {
		return c.Respond(&tele.CallbackResponse{Text: "Отозвать может только автор вызова"})
	}
	won, err := b.st.RevokeDuel(gameID)
	if err != nil {
		return b.fail(c, "onRevoke.revoke", err)
	}
	if !won { // answered or expired a moment ago
		return c.Respond(&tele.CallbackResponse{Text: closedChallengeToast})
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Вызов отозван"})
	return c.Edit(duelRevokedText)
}

// expireStaleDuels burns challenges nobody answered within duelChallengeTTLMinutes,
// freeing the league's single pending slot. Called from the reminder ticker. A
// challenge whose room is being watched and already has somebody besides the
// challenger in it is alive (the target may simply have joined by the link), so
// it stays. The challenge post itself turns into the note and loses its buttons;
// a separate message goes out only when that edit fails.
func (b *Bot) expireStaleDuels() {
	expired, err := b.st.ExpireStaleDuels(duelChallengeTTLMinutes, func(d storage.ExpiredDuel) bool {
		return isWatching(d.GameID) && roomBusy(d.GameID)
	})
	if err != nil {
		log.Printf("expireStaleDuels: %v", err)
	}
	for _, d := range expired {
		challenger, target := "?", "?"
		if d.CreatedBy.Valid {
			if p, perr := b.st.PlayerByTg(d.ChatID, d.CreatedBy.Int64); perr == nil && p != nil {
				challenger = p.Name
			}
		}
		if p, perr := b.st.PlayerByID(d.TargetID); perr == nil && p != nil {
			target = p.Name
		}
		text := duelExpiredText(challenger, target)
		edited := false
		if d.PostMsgID.Valid {
			if eerr := b.editPost(d.ChatID, d.PostMsgID.Int64, text, nil); eerr != nil {
				log.Printf("expireStaleDuels edit: %v", eerr)
			} else {
				edited = true
			}
		}
		if !edited {
			if _, serr := b.tb.Send(tele.ChatID(d.ChatID), text); serr != nil {
				log.Printf("expireStaleDuels send: %v", serr)
			}
		}
		log.Printf("duel %d expired unanswered", d.GameID)
	}
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

func (b *Bot) onDecline(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDecline.game", err)
	}
	if game == nil || game.Status != "pending" || game.Deleted {
		return c.Respond(&tele.CallbackResponse{Text: closedChallengeToast})
	}
	target, ok := b.duelTargetGuard(c, gameID)
	if !ok {
		return nil
	}
	won, err := b.st.DeclineDuel(gameID)
	if err != nil {
		return b.fail(c, "onDecline.mark", err)
	}
	if !won { // accepted, revoked or expired a moment ago
		return c.Respond(&tele.CallbackResponse{Text: closedChallengeToast})
	}
	_ = c.Respond()
	err = c.Edit(duelDeclineText(*target))
	b.replyToChallenger(c, game, duelDeclineReplyText)
	return err
}

// duelRoomSeen reacts to who sits in a watched duel's usdoku room. The target
// who joined by the link without pressing «Принять» has accepted in fact: the
// challenge is accepted for them, so the expiry ticker cannot burn it mid-game.
// It also records whether anybody besides the challenger is in the room, which
// keeps expireStaleDuels away from a live game.
func (b *Bot) duelRoomSeen(g *storage.Game, info *usdoku.GameInfo) {
	targetID, isDuel, err := b.st.DuelTargetID(g.ID)
	if err != nil || !isDuel {
		return
	}
	var creatorID int64
	if pair, perr := b.st.DuelPlayerIDs(g.ID); perr == nil && len(pair) == 2 {
		creatorID = pair[0]
	}
	busy, targetIn := false, false
	for _, p := range info.Players {
		pl, _ := b.st.PlayerByNick(g.ChatID, p.Name)
		switch {
		case pl != nil && pl.ID == targetID:
			targetIn, busy = true, true
		case pl != nil && pl.ID == creatorID:
			// the challenger alone does not make a duel
		default:
			busy = true
		}
	}
	setRoomBusy(g.ID, busy)
	if targetIn {
		b.autoAcceptDuel(g.ID, targetID)
	}
}

// autoAcceptDuel accepts the challenge on the target's behalf (atomic and
// idempotent: only the first accept, from here or from the button, wins) and
// turns the challenge post into the accepted card, without extra messages.
func (b *Bot) autoAcceptDuel(gameID, targetID int64) {
	won, err := b.st.AcceptDuel(gameID)
	if err != nil {
		log.Printf("autoAcceptDuel %d: %v", gameID, err)
		return
	}
	if !won {
		return
	}
	game, err := b.st.GameByID(gameID)
	target, perr := b.st.PlayerByID(targetID)
	if err != nil || perr != nil || game == nil || target == nil || !game.PostMsgID.Valid {
		return
	}
	text := duelAcceptText(*target, game.UsdokuCode.String)
	if err := b.editPost(game.ChatID, game.PostMsgID.Int64, text, recordKeyboard(gameID)); err != nil {
		log.Printf("autoAcceptDuel.edit %d: %v", gameID, err)
		return
	}
	b.saveCard(gameID, game.ChatID, game.PostMsgID.Int64, text, "")
	log.Printf("duel %d accepted by joining the room", gameID)
}
