package storage

// InitiativeRow is one player's "who calls the games" record: how many rounds
// they started, all-time and in the recent window, plus when they last played.
type InitiativeRow struct {
	PlayerID   int64
	Name       string
	GamesAll   int    // season games this player started
	DuelsAll   int    // duels this player issued
	Games30    int    // season games started inside the window
	Duels30    int    // duels issued inside the window
	LastPlayed string // UTC "2006-01-02 15:04:05"; "" = never played
}

// InitiativeStats returns every active player's initiative record, most
// initiated first. since bounds the recent window (UTC datetime string).
// Games whose created_by matches no player (legacy rows imported before the
// column existed) are attributed to nobody.
func (s *Store) InitiativeStats(chatID int64, since string) ([]InitiativeRow, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlSeasonalGames+`) AS games_all,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlDuelGames+`) AS duels_all,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlSeasonalGames+`
		          AND g.completed_at >= ?) AS games_30,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlDuelGames+`
		          AND g.completed_at >= ?) AS duels_30,
		       COALESCE((SELECT MAX(g.completed_at) FROM game_results gr
		                 JOIN games g ON g.id = gr.game_id
		                 WHERE gr.player_id=p.id AND `+sqlCompletedGames+`), '') AS last_played
		FROM players p
		WHERE p.chat_id=? AND p.active=1
		ORDER BY (games_all + duels_all) DESC, p.name COLLATE NOCASE`,
		since, since, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InitiativeRow
	for rows.Next() {
		var r InitiativeRow
		if err := rows.Scan(&r.PlayerID, &r.Name, &r.GamesAll, &r.DuelsAll,
			&r.Games30, &r.Duels30, &r.LastPlayed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
