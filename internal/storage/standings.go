package storage

// Standing is one row of the season leaderboard.
type Standing struct {
	PlayerID int64
	Name     string
	Points   int
	Wins     int
	Games    int
}

// Standings returns the season leaderboard for all active players, ordered by
// points, then wins (tie-break), then name. Players with no games appear with 0.
// Only completed games in the given season contribute.
func (s *Store) Standings(chatID, seasonID int64) ([]Standing, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name,
		       COALESCE(SUM(gr.points), 0)                          AS pts,
		       COALESCE(SUM(CASE WHEN gr.rank=1 THEN 1 ELSE 0 END), 0) AS wins,
		       COUNT(gr.id)                                          AS games
		FROM players p
		LEFT JOIN games g
		       ON g.season_id = ? AND g.status = 'completed' AND g.chat_id = p.chat_id
		LEFT JOIN game_results gr
		       ON gr.game_id = g.id AND gr.player_id = p.id
		WHERE p.chat_id = ? AND p.active = 1
		GROUP BY p.id, p.name
		ORDER BY pts DESC, wins DESC, p.name COLLATE NOCASE`, seasonID, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Standing
	for rows.Next() {
		var st Standing
		if err := rows.Scan(&st.PlayerID, &st.Name, &st.Points, &st.Wins, &st.Games); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Leader returns the top standing, or nil when there are no players/points yet.
func (s *Store) Leader(chatID, seasonID int64) (*Standing, error) {
	st, err := s.Standings(chatID, seasonID)
	if err != nil || len(st) == 0 {
		return nil, err
	}
	return &st[0], nil
}
