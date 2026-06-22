package storage

// DuelStanding is one player's all-time duel record.
type DuelStanding struct {
	Name   string
	Wins   int
	Losses int
}

// DuelRecord returns a player's all-time duel wins (rank 1) and losses (rank > 1)
// across completed, non-deleted duel games in a chat.
func (s *Store) DuelRecord(chatID, playerID int64) (wins, losses int, err error) {
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN gr.rank=1 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN gr.rank>1 THEN 1 ELSE 0 END),0)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE gr.player_id=? AND g.chat_id=? AND g.status='completed'
		  AND g.deleted=0 AND g.duel_target_id IS NOT NULL`,
		playerID, chatID).Scan(&wins, &losses)
	return wins, losses, err
}

// DuelLeaderboard ranks active players by all-time duel wins (then win rate).
func (s *Store) DuelLeaderboard(chatID int64) ([]DuelStanding, error) {
	rows, err := s.db.Query(`
		SELECT p.name,
		       COALESCE(SUM(CASE WHEN gr.rank=1 THEN 1 ELSE 0 END),0) AS wins,
		       COALESCE(SUM(CASE WHEN gr.rank>1 THEN 1 ELSE 0 END),0) AS losses
		FROM players p
		JOIN game_results gr ON gr.player_id = p.id
		JOIN games g ON g.id = gr.game_id
		WHERE p.chat_id=? AND p.active=1 AND g.status='completed'
		  AND g.deleted=0 AND g.duel_target_id IS NOT NULL
		GROUP BY p.id, p.name
		HAVING (wins + losses) > 0
		ORDER BY wins DESC, (CAST(wins AS REAL)/(wins+losses)) DESC, p.name COLLATE NOCASE`,
		chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuelStanding
	for rows.Next() {
		var d DuelStanding
		if err := rows.Scan(&d.Name, &d.Wins, &d.Losses); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// HeadToHead returns each player's duel wins in games where BOTH played.
func (s *Store) HeadToHead(chatID, aID, bID int64) (aWins, bWins int, err error) {
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN gr.player_id=? AND gr.rank=1 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN gr.player_id=? AND gr.rank=1 THEN 1 ELSE 0 END),0)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id=? AND g.status='completed' AND g.deleted=0
		  AND g.duel_target_id IS NOT NULL
		  AND gr.game_id IN (
		      SELECT game_id FROM game_results WHERE player_id=?
		      INTERSECT
		      SELECT game_id FROM game_results WHERE player_id=?
		  )`,
		aID, bID, chatID, aID, bID).Scan(&aWins, &bWins)
	return aWins, bWins, err
}
