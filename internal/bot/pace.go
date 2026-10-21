package bot

import (
	"fmt"
	"log"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featPace is the personal pace line: a finisher who trailed the winner
// clearly less than usual gets a line of their own, whatever their place.
const featPace = "pace"

// evPaceShown logs a shown pace line (player_id = whose line).
const evPaceShown = "pace_shown"

// winnerSolve is the winner's solve time, 0 when unknown (a manual record
// without /setnick): then there is no lag to show.
func winnerSolve(rows []storage.ResultRow) int {
	for _, r := range rows {
		if r.Rank == 1 {
			return r.Duration
		}
	}
	return 0
}

// paceLine is a non-winner's pace line for a just-recorded game, if notable.
func (b *Bot) paceLine(game *storage.Game, r storage.ResultRow, best int) (string, bool) {
	if r.Rank < 2 || r.Duration <= 0 || best <= 0 {
		return "", false
	}
	samples, err := b.st.LagsBefore(game.ChatID, r.PlayerID, game.ID, game.Difficulty.String, game.Mode.String)
	if err != nil {
		log.Printf("paceLine: %v", err)
		return "", false
	}
	lags := make([]float64, 0, len(samples))
	for _, s := range samples {
		if l, ok := domain.Lag(s.Secs, s.Best); ok {
			lags = append(lags, l)
		}
	}
	lag, usual, ok := domain.PaceWin(r.Duration, best, lags)
	if !ok {
		return "", false
	}
	b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: r.PlayerID, Kind: evPaceShown})
	return paceText(r.Name, lag, usual), true
}

// paceText renders "🏃 Bob: 20 с от победителя, обычно около 1:30".
func paceText(name string, lagSecs, usualSecs int) string {
	behind := "вровень с победителем"
	if lagSecs > 0 {
		behind = secsPhrase(lagSecs) + " от победителя"
	}
	return fmt.Sprintf("🏃 <b>%s</b>: %s, обычно около %s", esc(name), behind, secsPhrase(usualSecs))
}
