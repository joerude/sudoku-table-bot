package storage

import "database/sql"

// SpeedStat is a player's timed-solve summary at one difficulty in a season.
type SpeedStat struct {
	AvgSecs  int // rounded average solve time; 0 when Games == 0
	BestSecs int // fastest solve; 0 when Games == 0
	Games    int // number of timed games (duration_secs NOT NULL)
}

// SpeedFor returns a player's timed-solve stats at a difficulty within a
// season. Only finishers with a recorded duration count; DNFs (no row) and
// manual entries (NULL duration) are naturally excluded.
func (s *Store) SpeedFor(chatID, seasonID, playerID int64, difficulty string) (*SpeedStat, error) {
	var (
		avg  sql.NullFloat64
		best sql.NullInt64
		n    int
	)
	err := s.db.QueryRow(`
		SELECT AVG(gr.duration_secs), MIN(gr.duration_secs), COUNT(gr.duration_secs)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE gr.player_id = ? AND g.chat_id = ? AND g.season_id = ?
		  AND g.status = 'completed' AND g.deleted = 0
		  AND g.difficulty = ? AND gr.duration_secs IS NOT NULL`,
		playerID, chatID, seasonID, difficulty).Scan(&avg, &best, &n)
	if err != nil {
		return nil, err
	}
	st := &SpeedStat{Games: n}
	if avg.Valid {
		st.AvgSecs = int(avg.Float64 + 0.5) // round to nearest second
	}
	if best.Valid {
		st.BestSecs = int(best.Int64)
	}
	return st, nil
}

// SpeedRow is one player's entry in the /speed leaderboard.
type SpeedRow struct {
	Name     string
	AvgSecs  int
	BestSecs int
	Games    int
}

// Speedboard ranks players by average solve time at a difficulty in a season
// (fastest first). Players with >= minGames timed games are `ranked`; players
// with at least one but fewer than minGames timed games are returned as
// `fewer` so the UI can list them in a footer. Players with no timed games at
// this difficulty do not appear at all.
func (s *Store) Speedboard(chatID, seasonID int64, difficulty string, minGames int) (ranked, fewer []SpeedRow, err error) {
	rows, err := s.db.Query(`
		SELECT p.name,
		       AVG(gr.duration_secs) AS avg_secs,
		       MIN(gr.duration_secs) AS best_secs,
		       COUNT(gr.duration_secs) AS games
		FROM players p
		JOIN game_results gr ON gr.player_id = p.id
		JOIN games g ON g.id = gr.game_id
		WHERE p.chat_id = ? AND p.active = 1
		  AND g.season_id = ? AND g.status = 'completed' AND g.deleted = 0
		  AND g.difficulty = ? AND gr.duration_secs IS NOT NULL
		GROUP BY p.id, p.name
		ORDER BY avg_secs ASC, games DESC, p.name COLLATE NOCASE`,
		chatID, seasonID, difficulty)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r    SpeedRow
			avg  float64
			best int
		)
		if err := rows.Scan(&r.Name, &avg, &best, &r.Games); err != nil {
			return nil, nil, err
		}
		r.AvgSecs = int(avg + 0.5)
		r.BestSecs = best
		if r.Games >= minGames {
			ranked = append(ranked, r)
		} else {
			fewer = append(fewer, r)
		}
	}
	return ranked, fewer, rows.Err()
}

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
