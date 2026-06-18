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
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onNewGame.ensure", err)
	}
	chatID := c.Chat().ID
	if !b.enoughPlayers(c, chatID) {
		return nil
	}

	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil {
		return b.fail(c, "onNewGame.pending", err)
	}
	if pending != nil {
		return c.Send("⚠️ Уже есть незакрытая игра. Сначала запиши её результат или отмени:",
			pendingConflictKeyboard(pending.ID))
	}

	difficulty, mode := parseNewGameArgs(c.Args())
	var createdBy int64
	if c.Sender() != nil {
		createdBy = c.Sender().ID
	}
	gameID, err := b.st.CreatePendingGame(chatID, season.ID, createdBy, difficulty, mode)
	if err != nil {
		return b.fail(c, "onNewGame.create", err)
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
		log.Printf("onNewGame.setcode: %v", err)
	}
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

func (b *Bot) onDelete(c tele.Context) error {
	gameID := parseID(c.Data())
	if err := b.st.DeleteGame(gameID); err != nil {
		return b.fail(c, "onDelete.delete", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Удалено"})
	return c.Edit("🗑 Игра удалена. Таблица пересчитана.")
}

// finalize assigns points, shows the result, and rolls the season if the
// target was reached. Always edits the callback's message.
func (b *Bot) finalize(c tele.Context, game *storage.Game) error {
	season, err := b.st.SeasonByID(game.SeasonID)
	if err != nil {
		return b.fail(c, "finalize.season", err)
	}
	if err := b.st.FinalizeGame(game.ID, season.PointsTable); err != nil {
		return b.fail(c, "finalize.finalize", err)
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		return b.fail(c, "finalize.results", err)
	}
	if err := c.Edit(resultText(rows), resultKeyboard(game.ID)); err != nil {
		log.Printf("finalize.edit: %v", err)
	}

	standings, err := b.st.Standings(game.ChatID, season.ID)
	if err != nil {
		return b.fail(c, "finalize.standings", err)
	}
	if len(standings) > 0 && standings[0].Points >= season.Target {
		newSeason, err := b.st.CloseSeason(season, standings[0].PlayerID)
		if err != nil {
			return b.fail(c, "finalize.close", err)
		}
		return c.Send(seasonEndText(season.Number, standings[0].Name, standings[0].Points,
			newSeason.Target, newSeason.Number))
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
