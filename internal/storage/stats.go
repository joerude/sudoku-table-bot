package storage

// HistoryGame summarises a completed game for /history.
type HistoryGame struct {
	ID         int64
	Date       string   // YYYY-MM-DD
	Difficulty string   // may be empty
	Order      []string // player names in finish order
	UsdokuCode string   // may be empty
}

// RecentGames returns the last n completed games for a chat, newest first.
func (s *Store) RecentGames(chatID int64, n int) ([]HistoryGame, error) {
	rows, err := s.db.Query(`
		SELECT id, date(completed_at), COALESCE(difficulty,''), COALESCE(usdoku_code,'')
		FROM games
		WHERE chat_id=? AND status='completed'
		ORDER BY id DESC LIMIT ?`, chatID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []HistoryGame
	for rows.Next() {
		var g HistoryGame
		if err := rows.Scan(&g.ID, &g.Date, &g.Difficulty, &g.UsdokuCode); err != nil {
			return nil, err
		}
		games = append(games, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range games {
		order, err := s.finishOrder(games[i].ID)
		if err != nil {
			return nil, err
		}
		games[i].Order = order
	}
	return games, nil
}

func (s *Store) finishOrder(gameID int64) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT p.name FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		WHERE gr.game_id=? ORDER BY gr.rank`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// PlayerStat is a single player's season summary for /me.
type PlayerStat struct {
	Points int
	Wins   int
	Games  int
	Rank   int // 1-based position in the standings
}

// StatFor computes a player's place and totals within a season.
func (s *Store) StatFor(chatID, seasonID, playerID int64) (*PlayerStat, error) {
	standings, err := s.Standings(chatID, seasonID)
	if err != nil {
		return nil, err
	}
	for i, st := range standings {
		if st.PlayerID == playerID {
			return &PlayerStat{Points: st.Points, Wins: st.Wins, Games: st.Games, Rank: i + 1}, nil
		}
	}
	return &PlayerStat{}, nil
}
