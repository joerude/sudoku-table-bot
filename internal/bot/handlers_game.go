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

// minPlayers is the smallest league that produces a meaningful result.
const minPlayers = 2

// enoughPlayers guards game actions: a competitive result needs at least two
// registered players, otherwise the leader's points are uncontested.
func (b *Bot) enoughPlayers(c tele.Context, chatID int64) bool {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		_ = b.fail(c, "enoughPlayers", err)
		return false
	}
	if len(players) < minPlayers {
		_ = c.Send(fmt.Sprintf(
			"👥 Для зачёта нужно минимум <b>%d</b> игрока. Сейчас зарегистрировано: <b>%d</b>.\n"+
				"Пусть соперники сделают /join.", minPlayers, len(players)))
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

// startNewGame creates a usdoku game (or falls back to a link) and posts it.
func (b *Bot) startNewGame(c tele.Context, difficulty, mode string) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "startNewGame.ensure", err)
	}
	chatID := c.Chat().ID
	if !b.enoughPlayers(c, chatID) {
		return nil
	}

	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil {
		return b.fail(c, "startNewGame.pending", err)
	}
	if pending != nil {
		return c.Send("⚠️ Уже есть незакрытая игра. Сначала запиши её результат или отмени:",
			pendingConflictKeyboard(pending.ID))
	}

	var createdBy int64
	if c.Sender() != nil {
		createdBy = c.Sender().ID
	}
	gameID, err := b.st.CreatePendingGame(chatID, season.ID, createdBy, difficulty, mode)
	if err != nil {
		return b.fail(c, "startNewGame.create", err)
	}

	// Try to create a real game on usdoku; fall back to a plain link if the API
	// is unreachable or changed shape.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	code, err := b.ud.Create(ctx, difficulty, mode, "private")
	if err != nil {
		log.Printf("usdoku create: %v", err)
		return c.Send(newGameText(difficulty, mode), recordKeyboard(gameID))
	}
	if err := b.st.SetUsdokuCode(gameID, code); err != nil {
		log.Printf("startNewGame.setcode: %v", err)
	}
	log.Printf("🎮 game %d created on usdoku: %s (%s/%s)", gameID, code, difficulty, mode)
	go b.watchGame(gameID, chatID, code) // auto-record when the game finishes
	return c.Send(newGameWithCodeText(difficulty, mode, code), recordKeyboard(gameID))
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
		return resultText(rows), resultKeyboard(gameID), nil
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
	result = resultText(rows)

	standings, err := b.st.Standings(game.ChatID, season.ID)
	if err != nil {
		return "", "", err
	}
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

// finalize scores the game and edits the callback's message with the result,
// rolling the season if needed.
func (b *Bot) finalize(c tele.Context, game *storage.Game) error {
	result, seasonEnd, err := b.scoreAndCheck(game)
	if err != nil {
		return b.fail(c, "finalize", err)
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
