package bot

import (
	"context"
	"log"
	"time"

	tele "gopkg.in/telebot.v3"

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
