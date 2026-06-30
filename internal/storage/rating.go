package storage

import "github.com/joerude/sudoku-bot-telegram/internal/domain"

// GamesForRating returns every completed, non-deleted game for a chat (season
// games AND duels) with its participants, ordered chronologically by game id.
// This is the full input to domain.ComputeRatings — the rating ignores the
// season/duel split entirely.
func (s *Store) GamesForRating(chatID int64) ([]domain.RatingGame, error) {
	rows, err := s.db.Query(`
		SELECT g.id, gr.player_id, gr.rank
		FROM games g
		JOIN game_results gr ON gr.game_id = g.id
		WHERE g.chat_id=? AND `+sqlCompletedGames+`
		ORDER BY g.id, gr.rank`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RatingGame
	idx := map[int64]int{}
	for rows.Next() {
		var gid, pid int64
		var rank int
		if err := rows.Scan(&gid, &pid, &rank); err != nil {
			return nil, err
		}
		i, ok := idx[gid]
		if !ok {
			out = append(out, domain.RatingGame{ID: gid})
			i = len(out) - 1
			idx[gid] = i
		}
		out[i].Participants = append(out[i].Participants,
			domain.RatingResult{PlayerID: pid, Rank: rank})
	}
	return out, rows.Err()
}
