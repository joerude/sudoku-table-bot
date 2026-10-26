package bot

import (
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	tele "gopkg.in/telebot.v3"
)

// onRating is an alias of the stats hub's «Рейтинг» tab.
func (b *Bot) onRating(c tele.Context) error { return b.openStatsTab(c, "rating") }

// ratingTab renders the season rating (recomputed every season) above the
// eternal ELO ladder for the chat.
func (b *Bot) ratingTab(chatID int64, season *storage.Season) (string, error) {
	games, err := b.st.GamesForRating(chatID)
	if err != nil {
		return "", err
	}
	names, err := b.st.PlayerNames(chatID)
	if err != nil {
		return "", err
	}
	counts, err := b.st.SeasonRatingInputs(chatID, season.ID)
	if err != nil {
		return "", err
	}
	return seasonRatingBlock(season.Number, domain.SeasonRating(counts), names) + "\n\n" +
		ratingLadder(domain.ComputeRatingsActive(games, b.activePlayerSet(chatID)), names), nil
}
