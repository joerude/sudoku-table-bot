package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
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
	_ = c.Respond()
	return b.startNewGame(c, "medium", "hardcore")
}

// gameRoom is a freshly created pending game; code is empty when usdoku room
// creation failed (manual fallback — players use /result).
type gameRoom struct {
	gameID int64
	code   string
}

// createGameRoom runs the shared setup for /newgame, /duel and /invite: guards,
// creates the pending game, tries to open a usdoku room, starts the watcher. It
// does NOT post a chat message. Returns (nil, nil) when a guard already replied
// to the user (caller should just return nil).
func (b *Bot) createGameRoom(c tele.Context, difficulty, mode string) (*gameRoom, error) {
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
		_ = c.Send("⚠️ Уже есть незакрытая игра. Сначала запиши её результат или отмени:",
			pendingConflictKeyboard(pending.ID))
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
	room := &gameRoom{gameID: gameID}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
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

// startNewGame creates a usdoku game (or falls back to a link) and posts it.
func (b *Bot) startNewGame(c tele.Context, difficulty, mode string) error {
	room, err := b.createGameRoom(c, difficulty, mode)
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
	return c.Send(text, recordKeyboard(room.gameID))
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

// onRecord opens the picker for an existing pending game (from its button).
func (b *Bot) onRecord(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onRecord.game", err)
	}
	if game == nil || game.Status == "completed" {
		_ = c.Respond(&tele.CallbackResponse{Text: "Игра не найдена или уже закрыта"})
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
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onPick.game", err)
	}
	if game == nil || game.Status == "completed" {
		_ = c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
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
		_ = c.Respond()
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
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDone.game", err)
	}
	if game == nil || game.Status == "completed" {
		_ = c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
		return nil
	}
	picked, err := b.st.PickedPlayerIDs(gameID)
	if err != nil {
		return b.fail(c, "onDone.picked", err)
	}
	if len(picked) == 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала выбери хотя бы одного игрока"})
	}
	_ = c.Respond()
	return b.finalize(c, game)
}

func (b *Bot) onReset(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onReset.game", err)
	}
	if game == nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "Игра не найдена"})
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
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onUndoPick.game", err)
	}
	if game == nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "Игра не найдена"})
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
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onEdit.game", err)
	}
	if game == nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "Игра не найдена"})
		return nil
	}
	if err := b.st.ReopenGame(gameID); err != nil {
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
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDeleteAsk.game", err)
	}
	if game == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игра не найдена"})
	}
	_ = c.Respond()
	return c.Edit(confirmDeleteKeyboard(gameID))
}

// onDeleteConfirm soft-deletes the game and offers to restore it.
func (b *Bot) onDeleteConfirm(c tele.Context) error {
	gameID := parseID(c.Data())
	if err := b.st.SoftDeleteGame(gameID); err != nil {
		return b.fail(c, "onDeleteConfirm.delete", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
	return c.Edit("🗑 Игра удалена, таблица пересчитана.\nМожно вернуть:", restoreKeyboard(gameID))
}

// onDeleteCancel keeps the game and restores its normal keyboard.
func (b *Bot) onDeleteCancel(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDeleteCancel.game", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Отменено"})
	if game != nil && game.Status == "completed" {
		return c.Edit(resultKeyboard(gameID))
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
			return b.duelResult(game, rows), resultKeyboard(gameID), nil
		}
		target := 0
		if season, err := b.st.SeasonByID(game.SeasonID); err == nil && season != nil {
			target = season.Target
		}
		leader, _ := b.st.Leader(game.ChatID, game.SeasonID)
		return resultText(rows, game.Difficulty.String, game.Mode.String, leader, target),
			resultKeyboard(gameID), nil
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
	if err := b.st.FinalizeGame(game.ID, season.PointsTable); err != nil {
		return "", "", err
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		return "", "", err
	}

	if _, isDuel, derr := b.st.DuelTargetID(game.ID); derr != nil {
		return "", "", derr
	} else if isDuel {
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

	if len(standings) > 0 && standings[0].Points >= season.Target {
		newSeason, err := b.st.CloseSeason(season, standings[0].PlayerID)
		if err != nil {
			return "", "", err
		}
		seasonEnd = seasonEndText(season.Number, standings[0].Name, standings[0].Points,
			newSeason.Target, newSeason.Number)
		log.Printf("🏁 season %d closed, winner=%s (%d pts) → season %d started",
			season.Number, standings[0].Name, standings[0].Points, newSeason.Number)
	}
	log.Printf("✅ game %d scored", game.ID)
	return result, seasonEnd, nil
}

// duelResult renders a finished duel: winner, time, and the pair's head-to-head.
func (b *Bot) duelResult(game *storage.Game, rows []storage.ResultRow) string {
	if len(rows) < 2 {
		return duelResultText(rows, 0, 0, false)
	}
	wWins, lWins, err := b.st.HeadToHead(game.ChatID, rows[0].PlayerID, rows[1].PlayerID)
	if err != nil {
		log.Printf("duelResult.h2h: %v", err)
		return duelResultText(rows, 0, 0, false)
	}
	return duelResultText(rows, wWins, lWins, true)
}

// onDoneDNF finalises the game, recording every remaining active player as a
// non-finisher (rank 0 → 0 points, still counts as a played game).
func (b *Bot) onDoneDNF(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onDoneDNF.game", err)
	}
	if game == nil || game.Status == "completed" {
		return c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
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
	_ = c.Respond()
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
			if s := p.SolveSeconds(); s > 0 {
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

// finalize scores the game and edits the callback's message with the result,
// rolling the season if needed.
func (b *Bot) finalize(c tele.Context, game *storage.Game) error {
	noNick := b.backfillUsdokuTimes(game) // manual records: pull solve times from usdoku
	result, seasonEnd, err := b.scoreAndCheck(game)
	if err != nil {
		return b.fail(c, "finalize", err)
	}
	if len(noNick) > 0 {
		result += fmt.Sprintf("\n\n⏱ Время не подтянулось: <b>%s</b> — задайте /setnick",
			esc(strings.Join(noNick, ", ")))
	}
	if err := c.Edit(result, resultKeyboard(game.ID)); err != nil {
		log.Printf("finalize.edit: %v", err)
	}
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

// remainingPlayers returns active players not yet picked for the game.
func (b *Bot) remainingPlayers(chatID, gameID int64) ([]storage.Player, error) {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		return nil, err
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
