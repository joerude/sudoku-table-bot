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
		  AND `+sqlSeasonalGames+`
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
// seasonID <= 0 means all seasons (career speedboard).
func (s *Store) Speedboard(chatID, seasonID int64, difficulty string, minGames int) (ranked, fewer []SpeedRow, err error) {
	seasonFilter, args := "", []any{chatID}
	if seasonID > 0 {
		seasonFilter = "AND g.season_id = ? "
		args = append(args, seasonID)
	}
	args = append(args, difficulty)
	rows, err := s.db.Query(`
		SELECT p.name,
		       AVG(gr.duration_secs) AS avg_secs,
		       MIN(gr.duration_secs) AS best_secs,
		       COUNT(gr.duration_secs) AS games
		FROM players p
		JOIN game_results gr ON gr.player_id = p.id
		JOIN games g ON g.id = gr.game_id
		WHERE p.chat_id = ? AND p.active = 1
		  `+seasonFilter+`AND `+sqlSeasonalGames+`
		  AND g.difficulty = ? AND gr.duration_secs IS NOT NULL
		GROUP BY p.id, p.name
		ORDER BY avg_secs ASC, games DESC, p.name COLLATE NOCASE`,
		args...)
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
		WHERE g.chat_id=? AND g.season_id=? AND `+sqlSeasonalGames+`
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
	ID          int64
	CompletedAt string   // UTC datetime "2006-01-02 15:04:05" (render converts to chat TZ)
	Difficulty  string   // may be empty
	Order       []string // player names in finish order
	UsdokuCode  string   // may be empty
}

// RecentGames returns the last n completed games for a chat, newest first.
func (s *Store) RecentGames(chatID int64, n int) ([]HistoryGame, error) {
	rows, err := s.db.Query(`
		SELECT g.id, g.completed_at, COALESCE(g.difficulty,''), COALESCE(g.usdoku_code,'')
		FROM games g
		WHERE g.chat_id=? AND `+sqlSeasonalGames+`
		ORDER BY g.id DESC LIMIT ?`, chatID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []HistoryGame
	for rows.Next() {
		var g HistoryGame
		if err := rows.Scan(&g.ID, &g.CompletedAt, &g.Difficulty, &g.UsdokuCode); err != nil {
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
		WHERE gr.game_id=? AND gr.rank > 0 ORDER BY gr.rank`, gameID)
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

// RecordRow is the fastest all-time solve at one difficulty and who set it.
type RecordRow struct {
	Difficulty string
	Secs       int
	Name       string
}

// RecordsBoard returns the fastest auto-recorded solve per difficulty across
// all seasons (duels and deleted/pending games excluded). SQLite returns the
// row matching MIN() for the bare columns in a GROUP BY.
func (s *Store) RecordsBoard(chatID int64) ([]RecordRow, error) {
	rows, err := s.db.Query(`
		SELECT g.difficulty, MIN(gr.duration_secs) AS best, p.name
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id = ? AND `+sqlSeasonalGames+`
		  AND gr.duration_secs IS NOT NULL
		  AND g.difficulty IS NOT NULL AND g.difficulty <> ''
		GROUP BY g.difficulty`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordRow
	for rows.Next() {
		var r RecordRow
		if err := rows.Scan(&r.Difficulty, &r.Secs, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecentRanks returns a player's finishing ranks for completed non-duel games,
// newest first — for win-streak computation.
func (s *Store) RecentRanks(chatID, playerID int64) ([]int, error) {
	rows, err := s.db.Query(`
		SELECT gr.rank
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND `+sqlSeasonalGames+`
		ORDER BY g.completed_at DESC, g.id DESC`, chatID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var r int
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlayedTimes returns completed_at timestamps (UTC, "2006-01-02 15:04:05") of a
// player's completed non-duel games, newest first — for play-day streaks.
func (s *Store) PlayedTimes(chatID, playerID int64) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT g.completed_at
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND `+sqlSeasonalGames+`
		  AND g.completed_at IS NOT NULL
		ORDER BY g.completed_at DESC`, chatID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CareerStats returns a player's cross-season totals: wins (rank=1), games
// played (any rank incl. DNF), and best solve seconds (0 if no timed games).
func (s *Store) CareerStats(chatID, playerID int64) (wins, games, bestSecs int, err error) {
	var best, winsN sql.NullInt64
	err = s.db.QueryRow(`
		SELECT
		  SUM(CASE WHEN gr.rank = 1 THEN 1 ELSE 0 END),
		  COUNT(*),
		  MIN(gr.duration_secs)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND `+sqlSeasonalGames,
		chatID, playerID).Scan(&winsN, &games, &best)
	if err != nil {
		return 0, 0, 0, err
	}
	if winsN.Valid {
		wins = int(winsN.Int64)
	}
	if best.Valid {
		bestSecs = int(best.Int64)
	}
	return wins, games, bestSecs, nil
}

// SeasonsWon counts archived seasons this player won.
func (s *Store) SeasonsWon(chatID, playerID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM seasons WHERE chat_id = ? AND winner_id = ?`,
		chatID, playerID).Scan(&n)
	return n, err
}

// BestSecsBefore returns a player's fastest recorded solve in seasonal games
// strictly before the given game id (0 when none). Empty difficulty means any
// difficulty. Used to detect personal bests and freshly earned speed badges.
func (s *Store) BestSecsBefore(chatID, playerID int64, difficulty string, beforeGameID int64) (int, error) {
	q := `
		SELECT MIN(gr.duration_secs)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ? AND g.id < ?
		  AND ` + sqlSeasonalGames + `
		  AND gr.duration_secs IS NOT NULL`
	args := []any{chatID, playerID, beforeGameID}
	if difficulty != "" {
		q += ` AND g.difficulty = ?`
		args = append(args, difficulty)
	}
	var best sql.NullInt64
	if err := s.db.QueryRow(q, args...).Scan(&best); err != nil {
		return 0, err
	}
	if !best.Valid {
		return 0, nil
	}
	return int(best.Int64), nil
}

// FastestInSeason returns the single fastest auto-recorded solve of a season
// (nil if none) — for the season-end awards.
func (s *Store) FastestInSeason(chatID, seasonID int64) (*RecordRow, error) {
	var r RecordRow
	err := s.db.QueryRow(`
		SELECT COALESCE(g.difficulty,''), gr.duration_secs, p.name
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id=? AND g.season_id=? AND `+sqlSeasonalGames+`
		  AND gr.duration_secs IS NOT NULL
		ORDER BY gr.duration_secs ASC LIMIT 1`, chatID, seasonID).
		Scan(&r.Difficulty, &r.Secs, &r.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SeasonRank is one player's finishing rank in one game of a season, ordered
// oldest game first — for computing the season's longest win run.
type SeasonRank struct {
	PlayerID int64
	Name     string
	Rank     int
}

// SeasonRanks returns every (player, rank) of a season's completed non-duel
// games in game order.
func (s *Store) SeasonRanks(chatID, seasonID int64) ([]SeasonRank, error) {
	rows, err := s.db.Query(`
		SELECT gr.player_id, p.name, gr.rank
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id = ? AND g.season_id = ? AND `+sqlSeasonalGames+`
		ORDER BY g.id, gr.rank`, chatID, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeasonRank
	for rows.Next() {
		var r SeasonRank
		if err := rows.Scan(&r.PlayerID, &r.Name, &r.Rank); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
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

// GamesSince counts completed non-duel games on/after a UTC timestamp.
func (s *Store) GamesSince(chatID int64, sinceUTC string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games g
		WHERE g.chat_id=? AND `+sqlSeasonalGames+`
		  AND g.completed_at >= ?`, chatID, sinceUTC).Scan(&n)
	return n, err
}

// FastestSince returns the single fastest auto-recorded solve on/after a UTC
// timestamp (nil if none).
func (s *Store) FastestSince(chatID int64, sinceUTC string) (*RecordRow, error) {
	var r RecordRow
	err := s.db.QueryRow(`
		SELECT g.difficulty, gr.duration_secs, p.name
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id=? AND `+sqlSeasonalGames+`
		  AND gr.duration_secs IS NOT NULL
		  AND g.completed_at >= ?
		ORDER BY gr.duration_secs ASC LIMIT 1`, chatID, sinceUTC).
		Scan(&r.Difficulty, &r.Secs, &r.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
