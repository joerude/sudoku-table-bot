package storage

import "github.com/joerude/sudoku-bot-telegram/internal/domain"

// SeasonRatingInputs returns, for every active player of the chat, the counts
// the season rating is scored from: season games joined (DNF included), wins
// in them, season games plus duels joined, finishes across both (rank > 0),
// duels joined and duels won. Completed, non-deleted games of the given season
// only. Players with no game in the season come back with zeros.
func (s *Store) SeasonRatingInputs(chatID, seasonID int64) ([]domain.SeasonRatingInput, error) {
	rows, err := s.db.Query(`
		SELECT p.id,
		       COALESCE(SUM(CASE WHEN gr.id IS NOT NULL AND g.duel_target_id IS NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN gr.rank=1 AND g.duel_target_id IS NULL THEN 1 ELSE 0 END), 0),
		       COUNT(gr.id),
		       COALESCE(SUM(CASE WHEN gr.rank>0 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN gr.id IS NOT NULL AND g.duel_target_id IS NOT NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN gr.rank=1 AND g.duel_target_id IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM players p
		LEFT JOIN games g
		       ON g.season_id = ? AND `+sqlCompletedGames+` AND g.chat_id = p.chat_id
		LEFT JOIN game_results gr
		       ON gr.game_id = g.id AND gr.player_id = p.id
		WHERE p.chat_id = ? AND p.active = 1
		GROUP BY p.id
		ORDER BY p.id`, seasonID, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SeasonRatingInput
	for rows.Next() {
		var in domain.SeasonRatingInput
		if err := rows.Scan(&in.PlayerID, &in.SeasonGames, &in.SeasonWins, &in.Played,
			&in.Finished, &in.Duels, &in.DuelWins); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
