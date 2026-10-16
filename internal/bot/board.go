package bot

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/chart"
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// The game's starting grid: read from usdoku once (round start, or the result
// if the start was missed), kept in games.puzzle, and graded after the game.

// ensurePuzzle returns the game's starting grid: the stored one, else fetched
// from usdoku and stored. False when neither works (no room, usdoku down).
func (b *Bot) ensurePuzzle(g *storage.Game) ([9][9]int, bool) {
	if p, err := b.st.Puzzle(g.ID); err != nil {
		log.Printf("ensurePuzzle.load %d: %v", g.ID, err)
	} else if grid, ok := domain.ParsePuzzle(p); ok {
		return grid, true
	}
	if g.UsdokuCode.String == "" || b.ud == nil {
		return [9][9]int{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grid, err := b.ud.Board(ctx, g.UsdokuCode.String)
	if err != nil {
		log.Printf("ensurePuzzle.fetch %s: %v", g.UsdokuCode.String, err)
		return [9][9]int{}, false
	}
	if err := b.st.SetPuzzle(g.ID, domain.EncodePuzzle(grid)); err != nil {
		log.Printf("ensurePuzzle.save %d: %v", g.ID, err)
	}
	return grid, true
}

// storedGrading grades the game's stored grid without touching usdoku, for
// renders that must stay fast (the result keyboard).
func (b *Bot) storedGrading(gameID int64) (domain.Grading, bool) {
	p, err := b.st.Puzzle(gameID)
	if err != nil {
		return domain.Grading{}, false
	}
	grid, ok := domain.ParsePuzzle(p)
	if !ok {
		return domain.Grading{}, false
	}
	return domain.GradePuzzle(grid), true
}

// boardFooter is the result post's line on what the board took to solve.
// Best-effort: "" when the grid is unknown. It comes after the game on
// purpose: shown at the start it would be a hint.
func (b *Bot) boardFooter(g *storage.Game) string {
	grid, ok := b.ensurePuzzle(g)
	if !ok {
		return ""
	}
	return "\n\n" + boardLine(domain.GradePuzzle(grid))
}

// boardTechButton offers the hardest technique the board needed, if it took
// more than singles.
func (b *Bot) boardTechButton(gameID int64) (tele.Btn, bool) {
	gr, ok := b.storedGrading(gameID)
	if !ok {
		return tele.Btn{}, false
	}
	return techButton(gr)
}

// onBoardTech explains a technique from a result post's button. Ephemeral, so
// the result post stays as it is (the /learn picker edits its own message).
func (b *Bot) onBoardTech(c tele.Context) error {
	tech, ok := domain.TechniqueByKey(c.Data())
	if !ok {
		return c.Respond()
	}
	return b.ephemeral(c, learnTechMsg(tech))
}

// boardPhoto draws a game's starting grid. A finished game also gets the
// board line and the technique button; a live one never does (a hint).
func boardPhoto(g *storage.Game, grid [9][9]int) (*tele.Photo, *tele.ReplyMarkup, error) {
	code := g.UsdokuCode.String
	png, err := chart.RenderBoard(chart.Board{Code: code, Tag: gameTag(g.Difficulty.String, g.Mode.String), Cells: grid})
	if err != nil {
		return nil, nil, err
	}
	caption := boardCaption(code, g.Difficulty.String, g.Mode.String)
	var kb *tele.ReplyMarkup
	if g.Status == "completed" {
		gr := domain.GradePuzzle(grid)
		caption += "\n" + boardLine(gr)
		if btn, ok := techButton(gr); ok {
			kb = &tele.ReplyMarkup{}
			kb.Inline(kb.Row(btn))
		}
	}
	return &tele.Photo{File: tele.FromReader(bytes.NewReader(png)), Caption: caption}, kb, nil
}

// postBoard posts the puzzle's starting position when a round starts, as a
// silent photo of its own. Best-effort: a failure only logs, the game goes on
// without it.
func (b *Bot) postBoard(g *storage.Game) {
	if b.tb == nil {
		return
	}
	grid, ok := b.ensurePuzzle(g)
	if !ok {
		return // no grid, nothing to draw (ensurePuzzle logs a failed fetch)
	}
	photo, _, err := boardPhoto(g, grid)
	if err != nil {
		log.Printf("postBoard.render %d: %v", g.ID, err)
		return
	}
	if _, err := b.tb.Send(tele.ChatID(g.ChatID), photo, tele.Silent); err != nil {
		log.Printf("postBoard.send %d: %v", g.ID, err)
	}
}

// onBoard posts a stored starting grid: /board for the chat's latest game,
// /board QWER for that room.
func (b *Bot) onBoard(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onBoard.ensure", err)
	}
	code := ""
	if args := c.Args(); len(args) > 0 {
		if len(args) > 1 || !codeRe.MatchString(args[0]) {
			return b.ephemeral(c, "Пришли код комнаты, например: <code>/board QWER</code>")
		}
		code = strings.ToUpper(args[0])
	}
	g, err := b.st.BoardGame(c.Chat().ID, code)
	if err != nil {
		return b.fail(c, "onBoard.game", err)
	}
	if g == nil {
		if code != "" {
			return b.ephemeral(c, fmt.Sprintf("Доски комнаты <b>%s</b> нет", esc(code)))
		}
		return b.ephemeral(c, "Сохранённых досок пока нет: они появляются с новыми играми")
	}
	grid, ok := b.ensurePuzzle(g) // stored: no usdoku call
	if !ok {
		return b.ephemeral(c, "Доска повреждена, показать не получится")
	}
	photo, kb, err := boardPhoto(g, grid)
	if err != nil {
		return b.fail(c, "onBoard.render", err)
	}
	if kb != nil {
		return b.quiet(c, photo, kb)
	}
	return b.quiet(c, photo)
}
