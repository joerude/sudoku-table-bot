package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

var validDifficulty = map[string]bool{"easy": true, "medium": true, "hard": true, "extreme": true}

// parseNewGameArgs extracts difficulty (default medium) and mode (default hardcore).
func parseNewGameArgs(args []string) (difficulty, mode string) {
	difficulty, mode = "medium", "hardcore"
	for _, a := range args {
		switch low := strings.ToLower(a); {
		case validDifficulty[low]:
			difficulty = low
		case low == "hardcore" || low == "original":
			mode = low
		}
	}
	return
}

// defaultMinPlayers is the fallback minimum when a chat has no setting.
const defaultMinPlayers = 2

// minPlayers is the per-chat minimum participants for a game to count.
func (b *Bot) minPlayers(chatID int64) int {
	n, err := b.st.MinPlayers(chatID)
	if err != nil || n < defaultMinPlayers {
		return defaultMinPlayers
	}
	return n
}

// enoughPlayers guards game actions: a competitive result needs enough
// registered players, otherwise the leader's points are uncontested.
func (b *Bot) enoughPlayers(c tele.Context, chatID int64) bool {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		_ = b.fail(c, "enoughPlayers", err)
		return false
	}
	if min := b.minPlayers(chatID); len(players) < min {
		_ = b.ephemeral(c, fmt.Sprintf(
			"👥 Для зачёта нужно минимум <b>%d</b> игроков. Сейчас зарегистрировано: <b>%d</b>.\n"+
				"Пусть соперники сделают /join.", min, len(players)))
		return false
	}
	return true
}

func (b *Bot) onNewGame(c tele.Context) error {
	difficulty, mode := parseNewGameArgs(c.Args())
	return b.startNewGame(c, difficulty, mode)
}

// onQuickGame starts a default medium/hardcore game from the quick menu.
func (b *Bot) onQuickGame(c tele.Context) error {
	// The quick menu rides on the help message, which must survive: new card.
	err := b.startNewGameAt(c, "medium", "hardcore", placeNew)
	_ = c.Respond() // after, so a «уже создаю» toast from the lock can still show
	return err
}

// gameRoom is a freshly created pending game; code is empty when usdoku room
// creation failed (manual fallback: players use /result). msg is the chat
// message that currently shows «Создаю комнату…» and becomes the game's card;
// callers turn it into the card with publishCard.
type gameRoom struct {
	gameID int64
	code   string
	msg    *tele.Message
}

// placement says where the «Создаю комнату…» notice, and so the game card, goes.
type placement int

const (
	// placeAuto edits the message of the pressed button (the /play hub, a
	// conflict post, a duel picker) and sends a fresh message for a command.
	placeAuto placement = iota
	// placeNew always sends a fresh message, for buttons whose message must
	// survive (the result post, the help message).
	placeNew
)

// createGameRoom runs the shared setup for /newgame, /duel and /invite: guards,
// creates the pending game, tries to open a usdoku room, starts the watcher. It
// does NOT post the final chat message: it only shows «Создаю комнату…» and
// leaves that message in gameRoom.msg for the caller to render the card into.
// Returns (nil, nil) when a guard already replied to the user (caller should
// just return nil). origin names the caller's flow for the pending-conflict
// continuation (see resumeAfterDelete).
func (b *Bot) createGameRoom(c tele.Context, difficulty, mode, origin string) (*gameRoom, error) {
	return b.createGameRoomAt(c, difficulty, mode, origin, placeAuto)
}

// createGameRoomAt is createGameRoom with an explicit notice placement.
func (b *Bot) createGameRoomAt(c tele.Context, difficulty, mode, origin string, place placement) (*gameRoom, error) {
	// One creation per chat at a time: a double tap, or two people at once, must
	// not both pass the "no pending game" check below.
	unlock, ok := creatingRooms.TryLock(c.Chat().ID)
	if !ok {
		b.replyBusy(c)
		return nil, nil
	}
	defer unlock()
	season, err := b.ensure(c)
	if err != nil {
		return nil, err
	}
	chatID := c.Chat().ID
	if !b.enoughPlayers(c, chatID) {
		return nil, nil // guard replied
	}
	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		_ = b.pendingConflict(c, pending, origin)
		return nil, nil
	}
	var createdBy int64
	if c.Sender() != nil {
		createdBy = c.Sender().ID
	}
	gameID, err := b.st.CreatePendingGame(chatID, season.ID, createdBy, difficulty, mode)
	if err != nil {
		return nil, err
	}
	room := &gameRoom{gameID: gameID, msg: b.showCreating(c, place)}
	if room.msg != nil {
		if err := b.st.SetPostMsg(gameID, int64(room.msg.ID)); err != nil {
			log.Printf("createGameRoom.postmsg: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	code, err := b.ud.Create(ctx, difficulty, mode, "private")
	if err != nil {
		log.Printf("usdoku create: %v", err)
		return room, nil // game exists, no code
	}
	if err := b.st.SetUsdokuCode(gameID, code); err != nil {
		log.Printf("createGameRoom.setcode: %v", err)
	}
	room.code = code
	log.Printf("🎮 game %d created on usdoku: %s (%s/%s)", gameID, code, difficulty, mode)
	go b.watchGame(gameID, chatID, code)
	return room, nil
}

// replyBusy answers a create request that lost the per-chat lock: a toast for a
// button press, an ephemeral line for a command.
func (b *Bot) replyBusy(c tele.Context) {
	if c.Callback() != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: busyCreatingText})
		return
	}
	_ = b.ephemeral(c, busyCreatingText)
}

// showCreating gives instant feedback while the usdoku room is being made: the
// pressed button's message turns into «Создаю комнату…» (dropping its stale
// keyboard), or a fresh notice is sent. The returned message becomes the card.
func (b *Bot) showCreating(c tele.Context, place placement) *tele.Message {
	if place == placeAuto && c.Callback() != nil && c.Callback().Message != nil {
		if err := c.Edit(creatingText); err != nil {
			log.Printf("showCreating.edit: %v", err)
		}
		return c.Callback().Message
	}
	msg, err := b.tb.Send(c.Chat(), creatingText)
	if err != nil {
		log.Printf("showCreating.send: %v", err)
		return nil
	}
	return msg
}

// publishCard turns the room's notice message into the game card (text + kb),
// sending a fresh message if there is none or the edit fails, and stores the
// card so /code, live edits and the final close find it again, restart included.
func (b *Bot) publishCard(room *gameRoom, chatID int64, kind, text string, kb *tele.ReplyMarkup) error {
	var msgID int64
	if room.msg != nil {
		err := b.editPost(chatID, int64(room.msg.ID), text, kb)
		if err == nil {
			msgID = int64(room.msg.ID)
		} else {
			log.Printf("publishCard.edit: %v", err)
		}
	}
	if msgID == 0 {
		msg, err := b.tb.Send(tele.ChatID(chatID), text, kb)
		if err != nil {
			return err
		}
		room.msg = msg
		msgID = int64(msg.ID)
	}
	b.saveCard(room.gameID, chatID, msgID, text, kind)
	return nil
}

// resultKB is the keyboard of a recorded game's result post. It is the one
// place that decides which extra buttons ride along: a regular game with a known
// difficulty and mode gets «Ещё одну»; a duel gets its rematch button here (the
// duel flow adds rematchButton(gameID) in the isDuel branch).
func (b *Bot) resultKB(gameID int64) *tele.ReplyMarkup {
	game, err := b.st.GameByID(gameID)
	if err != nil || game == nil {
		return resultKeyboard(gameID)
	}
	if _, isDuel, err := b.st.DuelTargetID(gameID); err != nil || isDuel {
		return resultKeyboard(gameID) // duel: append rematchButton(gameID) here
	}
	if game.Difficulty.String == "" || game.Mode.String == "" {
		return resultKeyboard(gameID) // recorded by hand: nothing to repeat
	}
	return resultKeyboard(gameID, againButton(gameID))
}

// onAgain starts a new game with the same difficulty and mode as a recorded one.
// The result post must survive, so the new card is a new message. The usual
// createGameRoom guards apply (enough players, no pending game, one creation
// at a time).
func (b *Bot) onAgain(c tele.Context) error {
	game, err := b.st.GameByID(parseID(c.Data()))
	if err != nil {
		return b.fail(c, "onAgain.game", err)
	}
	if game == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игра не найдена"})
	}
	difficulty, mode := repeatParams(game.Difficulty.String, game.Mode.String)
	err = b.startNewGameAt(c, difficulty, mode, placeNew)
	_ = c.Respond() // after, so a guard's toast can still show
	return err
}

// repeatParams validates a recorded game's difficulty and mode for a rematch,
// falling back to the defaults for anything missing or unknown.
func repeatParams(difficulty, mode string) (string, string) {
	if !validDifficulty[difficulty] {
		difficulty = "medium"
	}
	if mode != "hardcore" && mode != "original" {
		mode = "hardcore"
	}
	return difficulty, mode
}

// pendingConflict posts the "unfinished game" warning with the pending game's
// metadata and its record/cancel buttons. origin (may be empty) is the command
// that got blocked; deleting the game then resumes it (resumeAfterDelete).
func (b *Bot) pendingConflict(c tele.Context, pending *storage.Game, origin string) error {
	creator := ""
	if pending.CreatedBy.Valid {
		if p, err := b.st.PlayerByTg(pending.ChatID, pending.CreatedBy.Int64); err == nil && p != nil {
			creator = p.Name
		}
	}
	return c.Send(pendingConflictText(pending, creator, b.chatTZ(pending.ChatID)),
		pendingConflictKeyboard(pending.ID, origin))
}

// codeRe matches a usdoku room code (short alphanumeric, e.g. LOVI, QPHA).
var codeRe = regexp.MustCompile(`^[A-Za-z0-9]{3,8}$`)

// onSetCode attaches a manually created usdoku room to the pending game and
// starts watching it, so auto-record works even when the bot couldn't create
// the room itself (usdoku down at game creation).
func (b *Bot) onSetCode(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onSetCode.ensure", err)
	}
	args := c.Args()
	if len(args) != 1 || !codeRe.MatchString(args[0]) {
		return b.ephemeral(c, "Пришли код комнаты, например: <code>/code LOVI</code>")
	}
	code := strings.ToUpper(args[0])
	chatID := c.Chat().ID
	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil {
		return b.fail(c, "onSetCode.pending", err)
	}
	if pending == nil {
		return b.ephemeral(c, "Нет незакрытой игры — сначала создай её: /play")
	}
	if pending.UsdokuCode.String != "" {
		return b.ephemeral(c, fmt.Sprintf(
			"У игры #%d уже есть комната: https://www.usdoku.com/%s",
			pending.ID, pending.UsdokuCode.String))
	}
	// Check the room exists before binding: a typo would otherwise start a
	// watcher that polls nothing for three hours.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.ud.Info(ctx, code)
	if err != nil {
		log.Printf("onSetCode.info %s: %v", code, err)
		return b.ephemeral(c, fmt.Sprintf("Комната <b>%s</b> не найдена на usdoku", esc(code)))
	}
	attached, err := b.st.AttachUsdokuCode(pending.ID, code)
	if err != nil {
		return b.fail(c, "onSetCode.set", err)
	}
	if !attached {
		return b.ephemeral(c, "У игры уже есть комната или она закрыта")
	}
	// Personal bests and /speed group by difficulty and mode, so take the real
	// ones from the room rather than whatever the game was created with.
	if diff, mode := roomParams(info); diff != "" || mode != "" {
		if err := b.st.SetGameParams(pending.ID, diff, mode); err != nil {
			log.Printf("onSetCode.params: %v", err)
		}
	}
	game, err := b.st.GameByID(pending.ID)
	if err != nil || game == nil {
		return b.fail(c, "onSetCode.reload", err)
	}
	b.showCodeAttached(game, code)
	go b.watchGame(pending.ID, chatID, code)
	return nil
}

// roomParams reads a usdoku room's difficulty and mode in the bot's vocabulary.
// Anything unrecognised comes back empty, so it never overwrites a known value.
func roomParams(info *usdoku.GameInfo) (difficulty, mode string) {
	if d := strings.ToLower(info.Info.Difficulty); validDifficulty[d] {
		difficulty = d
	}
	if m := strings.ToLower(info.Info.Mode); m == "hardcore" || m == "original" {
		mode = m
	}
	return
}

// showCodeAttached turns the game's card into the «room is bound» view (or
// sends one when the game has no card yet), keeping the card kind.
func (b *Bot) showCodeAttached(game *storage.Game, code string) {
	kind := game.PostKind.String
	var text string
	rsvp := 0
	switch {
	case kind == cardKindInvite:
		players, _ := b.st.ListPlayers(game.ChatID)
		roster, _ := b.st.RsvpPlayers(game.ID)
		rsvp = len(roster)
		text = inviteText(game.Difficulty.String, code, players, roster)
	case game.Difficulty.String != "":
		text = newGameWithCodeText(game.Difficulty.String, game.Mode.String, code)
	default:
		text = fmt.Sprintf(
			"🔗 Комната привязана к игре #%d: https://www.usdoku.com/%s\n"+
				"🤖 Слежу за ней, результат подтянется автоматически (у кого задан /setnick).",
			game.ID, code)
	}
	kb := liveKeyboard(game.ID, kind, false, rsvp)
	if game.PostMsgID.Valid {
		err := b.editPost(game.ChatID, game.PostMsgID.Int64, text, kb)
		if err == nil {
			b.saveCard(game.ID, game.ChatID, game.PostMsgID.Int64, text, kind)
			return
		}
		log.Printf("showCodeAttached.edit: %v", err)
	}
	msg, err := b.tb.Send(tele.ChatID(game.ChatID), text, kb)
	if err != nil {
		log.Printf("showCodeAttached.send: %v", err)
		return
	}
	b.saveCard(game.ID, game.ChatID, int64(msg.ID), text, kind)
}

// startNewGame creates a usdoku game (or falls back to a link) and shows it as
// the game card.
func (b *Bot) startNewGame(c tele.Context, difficulty, mode string) error {
	return b.startNewGameAt(c, difficulty, mode, placeAuto)
}

// startNewGameAt is startNewGame with an explicit card placement.
func (b *Bot) startNewGameAt(c tele.Context, difficulty, mode string, place placement) error {
	room, err := b.createGameRoomAt(c, difficulty, mode, "game:"+difficulty+":"+mode, place)
	if err != nil {
		return b.fail(c, "startNewGame", err)
	}
	if room == nil {
		return nil // a guard already replied
	}
	var text string
	if room.code == "" {
		text = newGameText(difficulty, mode)
	} else {
		text = newGameWithCodeText(difficulty, mode, room.code)
	}
	if players, perr := b.st.ListPlayers(c.Chat().ID); perr == nil {
		if missing := namesMissingNick(players); len(missing) > 0 {
			text += fmt.Sprintf(
				"\n\n⚠️ Без ника (авто-учёт не сработает): <b>%s</b> — задайте /setnick.",
				esc(strings.Join(missing, ", ")))
		}
	}
	return b.publishCard(room, c.Chat().ID, "", text, recordKeyboard(room.gameID))
}

func (b *Bot) onResult(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onResult.ensure", err)
	}
	chatID := c.Chat().ID
	if !b.enoughPlayers(c, chatID) {
		return nil
	}

	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil {
		return b.fail(c, "onResult.pending", err)
	}
	var gameID int64
	if pending != nil {
		gameID = pending.ID
	} else {
		var createdBy int64
		if c.Sender() != nil {
			createdBy = c.Sender().ID
		}
		gameID, err = b.st.CreatePendingGame(chatID, season.ID, createdBy, "", "")
		if err != nil {
			return b.fail(c, "onResult.create", err)
		}
	}

	text, markup, err := b.pickerView(chatID, gameID)
	if err != nil {
		return b.fail(c, "onResult.picker", err)
	}
	return c.Send(text, markup)
}

// gameToast says why a picker or record action must not run on g, or "" when it
// may. wantStatus is the status the action needs: "pending" for picker steps
// (pick, undo, reset, done), "completed" for reopening a result. A nil or
// soft-deleted game is never actionable.
func gameToast(g *storage.Game, wantStatus string) string {
	switch {
	case g == nil || g.Deleted:
		return "Игра не найдена"
	case g.Status == wantStatus:
		return ""
	case wantStatus == "pending":
		return "Игра уже закрыта"
	default:
		return toastAlreadyOpen
	}
}

const toastAlreadyOpen = "Игра уже открыта для записи"

// guardGame loads the game for a callback and checks gameToast. When the game
// is not actionable it answers the callback with the toast and returns ok=false
// (a DB error goes through fail), so stale buttons on an old picker or result
// post never change a game that was closed, reopened or deleted elsewhere. The
// mutating storage calls re-check the status in SQL, which covers the race
// between this read and the write.
func (b *Bot) guardGame(c tele.Context, op string, gameID int64, wantStatus string) (*storage.Game, bool) {
	game, err := b.st.GameByID(gameID)
	if err != nil {
		_ = b.fail(c, op+".game", err)
		return nil, false
	}
	if msg := gameToast(game, wantStatus); msg != "" {
		_ = c.Respond(&tele.CallbackResponse{Text: msg})
		return nil, false
	}
	return game, true
}

// onRecord opens the picker for an existing pending game (from its button).
func (b *Bot) onRecord(c tele.Context) error {
	gameID := parseID(c.Data())
	game, ok := b.guardGame(c, "onRecord", gameID, "pending")
	if !ok {
		return nil
	}
	text, markup, err := b.pickerView(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onRecord.picker", err)
	}
	_ = c.Respond()
	return c.Edit(text, markup)
}

// onPick records the next finisher; auto-finalises when everyone is placed.
func (b *Bot) onPick(c tele.Context) error {
	gameID, playerID := parsePair(c.Data())
	game, ok := b.guardGame(c, "onPick", gameID, "pending")
	if !ok {
		return nil
	}
	if err := b.st.AddPick(gameID, playerID); err != nil {
		return b.fail(c, "onPick.add", err)
	}

	remaining, err := b.remainingPlayers(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onPick.remaining", err)
	}
	if len(remaining) == 0 {
		return b.finalize(c, game)
	}
	text, markup, err := b.pickerView(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onPick.picker", err)
	}
	_ = c.Respond()
	return c.Edit(text, markup)
}

func (b *Bot) onDone(c tele.Context) error {
	gameID := parseID(c.Data())
	game, ok := b.guardGame(c, "onDone", gameID, "pending")
	if !ok {
		return nil
	}
	picked, err := b.st.PickedPlayerIDs(gameID)
	if err != nil {
		return b.fail(c, "onDone.picked", err)
	}
	if len(picked) == 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала выбери хотя бы одного игрока"})
	}
	return b.finalize(c, game)
}

func (b *Bot) onReset(c tele.Context) error {
	gameID := parseID(c.Data())
	game, ok := b.guardGame(c, "onReset", gameID, "pending")
	if !ok {
		return nil
	}
	if err := b.st.ClearResults(gameID); err != nil {
		return b.fail(c, "onReset.clear", err)
	}
	text, markup, err := b.pickerView(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onReset.picker", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Сброшено"})
	return c.Edit(text, markup)
}

// onUndoPick removes the last finisher tapped, stepping back one.
func (b *Bot) onUndoPick(c tele.Context) error {
	gameID := parseID(c.Data())
	game, ok := b.guardGame(c, "onUndoPick", gameID, "pending")
	if !ok {
		return nil
	}
	if err := b.st.RemoveLastPick(gameID); err != nil {
		return b.fail(c, "onUndoPick.remove", err)
	}
	text, markup, err := b.pickerView(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onUndoPick.picker", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Шаг назад"})
	return c.Edit(text, markup)
}

// onCancelRecord aborts recording: clears partial picks, keeps the game pending.
func (b *Bot) onCancelRecord(c tele.Context) error {
	gameID := parseID(c.Data())
	if _, ok := b.guardGame(c, "onCancelRecord", gameID, "pending"); !ok {
		return nil
	}
	if err := b.st.ClearResults(gameID); err != nil {
		return b.fail(c, "onCancelRecord.clear", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Отменено"})
	return c.Edit("✖️ Запись отменена. Нажми «📝 Записать результат», когда будете готовы.",
		recordKeyboard(gameID))
}

// onEdit reopens a finalised game for re-entry.
func (b *Bot) onEdit(c tele.Context) error {
	gameID := parseID(c.Data())
	game, ok := b.guardGame(c, "onEdit", gameID, "completed")
	if !ok {
		return nil
	}
	if err := b.st.ReopenGame(gameID); errors.Is(err, storage.ErrGameNotCompleted) {
		_ = c.Respond(&tele.CallbackResponse{Text: toastAlreadyOpen})
		return nil
	} else if err != nil {
		return b.fail(c, "onEdit.reopen", err)
	}
	text, markup, err := b.pickerView(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onEdit.picker", err)
	}
	_ = c.Respond()
	return c.Edit(text, markup)
}

// onDeleteAsk swaps the keyboard for a confirmation prompt (text is preserved).
func (b *Bot) onDeleteAsk(c tele.Context) error {
	gameID, origin := parseIDOrigin(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDeleteAsk.game", err)
	}
	if game == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игра не найдена"})
	}
	_ = c.Respond()
	return c.Edit(confirmDeleteKeyboard(gameID, origin))
}

// onDeleteConfirm soft-deletes the game. With no origin it offers to restore;
// with one (delete came from a pending-conflict post) it resumes that flow.
func (b *Bot) onDeleteConfirm(c tele.Context) error {
	gameID, origin := parseIDOrigin(c.Data())
	if err := b.st.SoftDeleteGame(gameID); err != nil {
		return b.fail(c, "onDeleteConfirm.delete", err)
	}
	b.closeOtherCard(c, gameID)
	if origin != "" {
		return b.resumeAfterDelete(c, origin)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
	return c.Edit("🗑 Игра удалена, таблица пересчитана.\nМожно вернуть:", restoreKeyboard(gameID))
}

// deletedNote closes out a pending-conflict post whose game was just deleted.
const deletedNote = "🗑 Игра удалена."

// originArgs extracts difficulty/mode from origin parts ("game:<diff>:<mode>"),
// falling back to the parseNewGameArgs defaults on anything unexpected.
func originArgs(parts []string) (difficulty, mode string) {
	difficulty, mode = "medium", "hardcore"
	if len(parts) > 1 && validDifficulty[parts[1]] {
		difficulty = parts[1]
	}
	if len(parts) > 2 && (parts[2] == "hardcore" || parts[2] == "original") {
		mode = parts[2]
	}
	return
}

// resumeAfterDelete continues the command that hit the pending-game conflict,
// so deleting the stale game flows straight into what the user asked for
// (the presser becomes the caller). Unknown origins degrade to a plain note.
func (b *Bot) resumeAfterDelete(c tele.Context, origin string) error {
	parts := strings.Split(origin, ":")
	switch parts[0] {
	case "play":
		_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
		return c.Edit(deletedNote+"\n\n🎮 <b>Что играем?</b>", playMenuKeyboard())
	case "duel":
		difficulty, _ := originArgs(parts)
		me := realSender(c)
		if me == nil {
			_ = c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
			return c.Edit(deletedNote)
		}
		caller, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
		if err != nil {
			return b.fail(c, "resumeAfterDelete.caller", err)
		}
		if caller == nil {
			_ = c.Respond(&tele.CallbackResponse{Text: "Сначала /join"})
			return c.Edit(deletedNote)
		}
		others, err := b.opponents(c.Chat().ID, caller.ID)
		if err != nil {
			return b.fail(c, "resumeAfterDelete.opponents", err)
		}
		if len(others) == 0 {
			_ = c.Respond(&tele.CallbackResponse{Text: "Нет соперников"})
			return c.Edit(deletedNote)
		}
		_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
		return c.Edit(deletedNote+"\n\n⚔️ Кого вызываешь на дуэль?",
			duelPickKeyboard(difficulty, others, me.ID))
	case "game":
		difficulty, mode := originArgs(parts)
		_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
		if err := c.Edit(deletedNote); err != nil {
			log.Printf("resumeAfterDelete.edit: %v", err)
		}
		return b.startNewGame(c, difficulty, mode)
	case "inv":
		difficulty, mode := originArgs(parts)
		_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
		if err := c.Edit(deletedNote); err != nil {
			log.Printf("resumeAfterDelete.edit: %v", err)
		}
		return b.startInvite(c, difficulty, mode)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
	return c.Edit(deletedNote)
}

// onDeleteCancel keeps the game and restores its normal keyboard (the
// conflict keyboard, origin intact, when the delete came from one).
func (b *Bot) onDeleteCancel(c tele.Context) error {
	gameID, origin := parseIDOrigin(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDeleteCancel.game", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Отменено"})
	if game != nil && game.Status == "completed" {
		return c.Edit(b.resultKB(gameID))
	}
	if origin != "" {
		return c.Edit(pendingConflictKeyboard(gameID, origin))
	}
	return c.Edit(recordKeyboard(gameID))
}

// onRestore un-deletes a game and rebuilds its view.
func (b *Bot) onRestore(c tele.Context) error {
	gameID := parseID(c.Data())
	if err := b.st.RestoreGame(gameID); err != nil {
		return b.fail(c, "onRestore.restore", err)
	}
	text, markup, err := b.gameView(gameID)
	if err != nil {
		return b.fail(c, "onRestore.view", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Возвращено"})
	return c.Edit(text, markup)
}

// gameView rebuilds the message text + keyboard for a game in its current state.
func (b *Bot) gameView(gameID int64) (string, *tele.ReplyMarkup, error) {
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return "", nil, err
	}
	if game == nil {
		return "🗑 Игра не найдена.", nil, nil
	}
	if game.Status == "completed" {
		rows, err := b.st.GameResults(gameID)
		if err != nil {
			return "", nil, err
		}
		if _, isDuel, derr := b.st.DuelTargetID(gameID); derr != nil {
			log.Printf("gameView.duelCheck: %v", derr)
		} else if isDuel {
			return b.duelResult(game, rows), b.resultKB(gameID), nil
		}
		target := 0
		if season, err := b.st.SeasonByID(game.SeasonID); err == nil && season != nil {
			target = season.Target
		}
		leader, _ := b.st.Leader(game.ChatID, game.SeasonID)
		return resultText(rows, game.Difficulty.String, game.Mode.String, leader, target),
			b.resultKB(gameID), nil
	}
	return "🧩 Игра ожидает результата — жми «📝 Записать результат».", recordKeyboard(gameID), nil
}

// scoreAndCheck assigns points to a game's picks, then rolls the season if the
// target was reached. Returns the result text and (if any) a season-end message.
// It does no Telegram I/O, so both the manual and auto-record paths can use it.
func (b *Bot) scoreAndCheck(game *storage.Game) (result, seasonEnd string, err error) {
	season, err := b.st.SeasonByID(game.SeasonID)
	if err != nil {
		return "", "", err
	}
	_, isDuel, err := b.st.DuelTargetID(game.ID)
	if err != nil {
		return "", "", err
	}

	// Snapshot the table before scoring so overtakes can be shown after.
	var before []storage.Standing
	if !isDuel {
		if before, err = b.st.Standings(game.ChatID, season.ID); err != nil {
			return "", "", err
		}
	}

	if err := b.st.FinalizeGame(game.ID, season.PointsTable); err != nil {
		return "", "", err
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		return "", "", err
	}

	if isDuel {
		return b.duelResult(game, rows), "", nil // duels don't touch the season
	}

	standings, err := b.st.Standings(game.ChatID, season.ID)
	if err != nil {
		return "", "", err
	}
	var leader *storage.Standing
	if len(standings) > 0 {
		leader = &standings[0]
	}
	result = resultText(rows, game.Difficulty.String, game.Mode.String, leader, season.Target)
	if line := movesLine(standingsMoves(before, standings)); line != "" {
		result += "\n" + line
	}
	if lines := b.celebrations(game, rows); len(lines) > 0 {
		result += "\n\n" + strings.Join(lines, "\n")
	}

	// Only an active season can roll over: re-scoring a game of an archived one
	// (edit) must not rewrite its winner or announce another season.
	if season.Status == "active" && len(standings) > 0 && standings[0].Points >= season.Target {
		newSeason, err := b.st.CloseSeason(season, standings[0].PlayerID)
		switch {
		case errors.Is(err, storage.ErrSeasonNotActive):
			// Closed meanwhile (e.g. by the deadline tick): no final to announce.
			log.Printf("scoreAndCheck: season %d already closed", season.Number)
		case err != nil:
			return "", "", err
		default:
			if derr := b.st.SetSeasonDeadline(newSeason.ID,
				fmtDBTime(domain.SeasonDeadline(time.Now(), loadLoc(b.chatTZ(game.ChatID))))); derr != nil {
				log.Printf("scoreAndCheck.setdeadline: %v", derr) // the reminder tick fills it in
			}
			awards := b.seasonAwards(game.ChatID, season.ID, standings)
			seasonEnd = seasonEndText(season.Number, standings[0].Name, standings[0].Points,
				awards, newSeason.Target, newSeason.Number)
			log.Printf("🏁 season %d closed, winner=%s (%d pts) → season %d started",
				season.Number, standings[0].Name, standings[0].Points, newSeason.Number)
		}
	}
	log.Printf("✅ game %d scored", game.ID)
	return result, seasonEnd, nil
}

// duelResult renders a finished duel: winner, time, the pair's head-to-head,
// and an upset line when the lower-rated player won.
func (b *Bot) duelResult(game *storage.Game, rows []storage.ResultRow) string {
	if len(rows) < 2 {
		return duelResultText(rows, 0, 0, false, 0)
	}
	upset := 0
	if rows[0].Rank == 1 {
		upset = b.duelUpsetGain(game, rows[0].PlayerID, rows[1].PlayerID)
	}
	wWins, lWins, err := b.st.HeadToHead(game.ChatID, rows[0].PlayerID, rows[1].PlayerID)
	if err != nil {
		log.Printf("duelResult.h2h: %v", err)
		return duelResultText(rows, 0, 0, false, upset)
	}
	return duelResultText(rows, wWins, lWins, true, upset)
}

// onDoneDNF finalises the game, recording every remaining active player as a
// non-finisher (rank 0 → 0 points, still counts as a played game).
func (b *Bot) onDoneDNF(c tele.Context) error {
	gameID := parseID(c.Data())
	game, ok := b.guardGame(c, "onDoneDNF", gameID, "pending")
	if !ok {
		return nil
	}
	picked, err := b.st.PickedPlayerIDs(gameID)
	if err != nil {
		return b.fail(c, "onDoneDNF.picked", err)
	}
	if len(picked) == 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала отметь хотя бы одного финишера"})
	}
	remaining, err := b.remainingPlayers(game.ChatID, gameID)
	if err != nil {
		return b.fail(c, "onDoneDNF.remaining", err)
	}
	for _, p := range remaining {
		if err := b.st.AddDNF(gameID, p.ID); err != nil {
			log.Printf("onDoneDNF.dnf: %v", err)
		}
	}
	return b.finalize(c, game)
}

// backfillUsdokuTimes fills finishers' solve times from the game's usdoku room
// (matched by usdoku nick) and returns the names of finishers who have no nick
// set, so the caller can nudge them. Best-effort with a short timeout so it never
// blocks the result for long; only meaningful when the game had a usdoku room.
// Used by the manual finalize path (auto-record captures times directly).
func (b *Bot) backfillUsdokuTimes(game *storage.Game) []string {
	if !game.UsdokuCode.Valid || game.UsdokuCode.String == "" {
		return nil
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		return nil
	}
	secsByNick := make(map[string]int64) // lower(usdoku name) -> solve seconds
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if info, err := b.ud.Info(ctx, game.UsdokuCode.String); err != nil {
		log.Printf("backfillUsdokuTimes.info %s: %v", game.UsdokuCode.String, err)
	} else {
		for _, p := range info.Players {
			if s := p.SolveSeconds(info.Info.StartedAt); s > 0 {
				secsByNick[strings.ToLower(p.Name)] = s
			}
		}
	}
	var noNick []string
	for _, r := range rows {
		if r.Rank == 0 { // DNF isn't a finisher
			continue
		}
		pl, err := b.st.PlayerByID(r.PlayerID)
		if err != nil || pl == nil {
			continue
		}
		if !pl.UsdokuNick.Valid || pl.UsdokuNick.String == "" {
			noNick = append(noNick, r.Name)
			continue
		}
		if r.Duration == 0 {
			if secs, ok := secsByNick[strings.ToLower(pl.UsdokuNick.String)]; ok {
				if err := b.st.SetPickDuration(game.ID, r.PlayerID, secs); err != nil {
					log.Printf("backfillUsdokuTimes.set: %v", err)
				}
			}
		}
	}
	return noNick
}

// ratingFooter computes the rating impact of the just-recorded game and renders
// the result-post footer (delta lines + optional crown change). Best-effort:
// returns "" on any error or when the game had no rating impact (solo/all-DNF),
// so rating never blocks or breaks the result post.
func (b *Bot) ratingFooter(game *storage.Game) string {
	games, err := b.st.GamesForRating(game.ChatID)
	if err != nil {
		log.Printf("ratingFooter.games: %v", err)
		return ""
	}
	r := domain.ComputeRatingsActive(games, b.activePlayerSet(game.ChatID))
	var gr *domain.GameRating
	for i := range r.PerGame {
		if r.PerGame[i].GameID == game.ID {
			gr = &r.PerGame[i]
			break
		}
	}
	if gr == nil {
		return ""
	}
	names, err := b.st.PlayerNames(game.ChatID)
	if err != nil {
		log.Printf("ratingFooter.players: %v", err)
		return ""
	}
	return ratingDeltaLines(*gr, names)
}

// finalize scores the game and edits the callback's message with the result,
// rolling the season if needed.
func (b *Bot) finalize(c tele.Context, game *storage.Game) error {
	noNick := b.backfillUsdokuTimes(game) // manual records: pull solve times from usdoku
	result, seasonEnd, err := b.scoreAndCheck(game)
	if errors.Is(err, storage.ErrGameNotPending) {
		// Closed meanwhile by another tap or by auto-record: that path already
		// posted the result, so stay quiet here.
		return c.Respond(&tele.CallbackResponse{Text: "Уже записано"})
	}
	if err != nil {
		return b.fail(c, "finalize", err)
	}
	_ = c.Respond()
	if len(noNick) > 0 {
		result += fmt.Sprintf("\n\n⏱ Время не подтянулось: <b>%s</b> — задайте /setnick",
			esc(strings.Join(noNick, ", ")))
	}
	result += b.ratingFooter(game)
	if err := c.Edit(result, b.resultKB(game.ID)); err != nil {
		log.Printf("finalize.edit: %v", err)
	}
	b.closeOtherCard(c, game.ID) // the result landed in the picker, not on the card
	if seasonEnd != "" {
		return c.Send(seasonEnd)
	}
	return nil
}

// pickerView builds the recording message text and keyboard from DB state.
func (b *Bot) pickerView(chatID, gameID int64) (string, *tele.ReplyMarkup, error) {
	picked, err := b.st.GameResults(gameID) // rank-ordered; points still 0
	if err != nil {
		return "", nil, err
	}
	remaining, err := b.remainingPlayers(chatID, gameID)
	if err != nil {
		return "", nil, err
	}
	return pickerText(picked), pickerKeyboard(gameID, remaining, len(picked)), nil
}

// remainingPlayers returns active players not yet picked for the game. For a
// duel only the two duelists are eligible — the picker offers just them, and
// DNF-finalize never drags a bystander into the duel.
func (b *Bot) remainingPlayers(chatID, gameID int64) ([]storage.Player, error) {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		return nil, err
	}
	if pair, err := b.st.DuelPlayerIDs(gameID); err != nil {
		return nil, err
	} else if len(pair) == 2 {
		inPair := map[int64]bool{pair[0]: true, pair[1]: true}
		var duelists []storage.Player
		for _, p := range players {
			if inPair[p.ID] {
				duelists = append(duelists, p)
			}
		}
		players = duelists
	}
	pickedIDs, err := b.st.PickedPlayerIDs(gameID)
	if err != nil {
		return nil, err
	}
	picked := make(map[int64]bool, len(pickedIDs))
	for _, id := range pickedIDs {
		picked[id] = true
	}
	var out []storage.Player
	for _, p := range players {
		if !picked[p.ID] {
			out = append(out, p)
		}
	}
	return out, nil
}
