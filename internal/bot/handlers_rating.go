package bot

import (
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	tele "gopkg.in/telebot.v3"
)

// onRating is an alias of the stats hub's «Рейтинг» tab.
func (b *Bot) onRating(c tele.Context) error { return b.openStatsTab(c, "rating") }

// ratingTab renders the eternal ELO ladder for the chat.
func (b *Bot) ratingTab(chatID int64) (string, error) {
	games, err := b.st.GamesForRating(chatID)
	if err != nil {
		return "", err
	}
	names, err := b.st.PlayerNames(chatID)
	if err != nil {
		return "", err
	}
	return ratingLadder(domain.ComputeRatings(games), names), nil
}
