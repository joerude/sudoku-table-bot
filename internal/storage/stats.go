package storage

import "database/sql"

// ExportGame is one completed game with the points each player earned.
type ExportGame struct {
	ID         int64
	Date       string
	Difficulty string
	Mode       string
	Code       string
	Points     map[int64]int // playerID -> points earned in this game
}

// ExportGames returns completed (non-deleted) games of a season with per-player
// points, for CSV export.
func (s *Store) ExportGames(chatID, seasonID int64) ([]ExportGame, error) {
	rows, err := s.db.Query(`
		SELECT g.id, date(g.completed_at), COALESCE(g.difficulty,''), COALESCE(g.mode,''),
		       COALESCE(g.usdoku_code,''), gr.player_id, gr.points
		FROM games g
		LEFT JOIN game_results gr ON gr.game_id = g.id
		WHERE g.chat_id=? AND g.season_id=? AND g.status='completed' AND g.deleted=0
		ORDER BY g.id`, chatID, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportGame
	idx := map[int64]int{}
	for rows.Next() {
		var (
			gid                    int64
			date, diff, mode, code string
			pid, pts               sql.NullInt64
		)
		if err := rows.Scan(&gid, &date, &diff, &mode, &code, &pid, &pts); err != nil {
			return nil, err
		}
		i, ok := idx[gid]
		if !ok {
			out = append(out, ExportGame{
				ID: gid, Date: date, Difficulty: diff, Mode: mode, Code: code,
				Points: map[int64]int{},
			})
			i = len(out) - 1
			idx[gid] = i
		}
		if pid.Valid {
			out[i].Points[pid.Int64] = int(pts.Int64)
		}
	}
	return out, rows.Err()
}

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
		WHERE chat_id=? AND status='completed' AND deleted=0
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
