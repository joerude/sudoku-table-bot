package bot

import (
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	tele "gopkg.in/telebot.v3"
)

// onRating shows the eternal ELO ladder for the chat.
func (b *Bot) onRating(c tele.Context) error {
	games, err := b.st.GamesForRating(c.Chat().ID)
	if err != nil {
		return b.fail(c, "rating", err)
	}
	players, err := b.st.ListPlayers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "rating", err)
	}
	names := make(map[int64]string, len(players))
	for _, p := range players {
		names[p.ID] = p.Name
	}
	return c.Send(ratingLadder(domain.ComputeRatings(games), names))
}
