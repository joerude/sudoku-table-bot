package storage

// VersusRow is one player's record against one opponent, over every game the
// two shared. Games: seasonal games where both played; whoever finished higher
// takes it (a DNF loses to a finisher, two DNFs count for nobody). Duels: wins
// of each side in duels between the two.
type VersusRow struct {
	OpponentID   int64
	OpponentName string
	GameWins     int
	GameLosses   int
	DuelWins     int
	DuelLosses   int
}

// Versus lists a player's record against every active opponent they ever
// shared a completed game with, most shared games first.
func (s *Store) Versus(chatID, playerID int64) ([]VersusRow, error) {
	rows, err := s.db.Query(`
		SELECT o.player_id, p.name,
		       COALESCE(SUM(CASE WHEN g.duel_target_id IS NULL AND me.rank > 0
		                          AND (o.rank = 0 OR me.rank < o.rank) THEN 1 ELSE 0 END), 0) AS gw,
		       COALESCE(SUM(CASE WHEN g.duel_target_id IS NULL AND o.rank > 0
		                          AND (me.rank = 0 OR o.rank < me.rank) THEN 1 ELSE 0 END), 0) AS gl,
		       COALESCE(SUM(CASE WHEN g.duel_target_id IS NOT NULL AND me.rank = 1 THEN 1 ELSE 0 END), 0) AS dw,
		       COALESCE(SUM(CASE WHEN g.duel_target_id IS NOT NULL AND o.rank = 1 THEN 1 ELSE 0 END), 0) AS dl
		FROM game_results me
		JOIN game_results o ON o.game_id = me.game_id AND o.player_id <> me.player_id
		JOIN games g ON g.id = me.game_id
		JOIN players p ON p.id = o.player_id
		WHERE me.player_id = ? AND g.chat_id = ? AND p.active = 1 AND `+sqlCompletedGames+`
		GROUP BY o.player_id, p.name
		HAVING gw + gl + dw + dl > 0
		ORDER BY COUNT(*) DESC, p.name COLLATE NOCASE`, playerID, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VersusRow
	for rows.Next() {
		var r VersusRow
		if err := rows.Scan(&r.OpponentID, &r.OpponentName, &r.GameWins, &r.GameLosses,
			&r.DuelWins, &r.DuelLosses); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
