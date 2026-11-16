package bot

import (
	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// evTap logs a press of a button the mechanics are judged by; detail says
// which one. Without it "are the buttons still pressed" has no answer in the
// database, only in the logs.
const evTap = "tap"

const (
	tapAgain   = "again"   // «Ещё одну» or «Реванш» under a seasonal result
	tapRematch = "rematch" // «Реванш» under a duel result
	tapTech    = "tech"    // the board's technique button under a result
	tapChart   = "chart"   // any picture button (race, calendar, rating, poster)
)

// logTap records one press; gameID is 0 when the button is not tied to a game.
func (b *Bot) logTap(c tele.Context, what string, gameID int64) {
	if c.Chat() == nil {
		return
	}
	b.logEvent(storage.Event{ChatID: c.Chat().ID, GameID: gameID, Kind: evTap, Detail: what})
}
