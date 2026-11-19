package storage

import (
	"database/sql"
	"strconv"
)

// Series is a best-of duel: two players, one difficulty, games played one by
// one until somebody reaches WinsNeeded. Each game is an ordinary duel game
// (games.series_id points here), so Elo and every duel stat count per game;
// the series only adds its own score and result.
type Series struct {
	ID         int64
	ChatID     int64
	AID        int64 // challenger player id
	BID        int64 // target player id
	BestOf     int   // 3 or 5
	Difficulty string
	// Status: active | done | abandoned (nobody played on) | cancelled (the
	// first challenge was declined, withdrawn or burnt).
	Status   string
	WinnerID sql.NullInt64
}

// WinsNeeded is how many game wins take a best-of series.
func WinsNeeded(bestOf int) int { return bestOf/2 + 1 }

// SeriesGame is one non-deleted game of a series, in play order. WinnerID is
// the rank-1 player of a completed game; 0 while pending or when nobody finished.
type SeriesGame struct {
	GameID   int64
	Status   string
	WinnerID int64
}

// SeriesScore counts the game wins of players a and b.
func SeriesScore(games []SeriesGame, aID, bID int64) (aWins, bWins int) {
	for _, g := range games {
		switch g.WinnerID {
		case aID:
			aWins++
		case bID:
			bWins++
		}
	}
	return aWins, bWins
}

// CompletedCount is how many of the games were recorded (void ones included):
// the next game's number is CompletedCount+1.
func CompletedCount(games []SeriesGame) int {
	n := 0
	for _, g := range games {
		if g.Status == "completed" {
			n++
		}
	}
	return n
}

const seriesCols = `id, chat_id, a_id, b_id, best_of, difficulty, status, winner_id`

func scanSeries(row interface{ Scan(...any) error }) (*Series, error) {
	var s Series
	err := row.Scan(&s.ID, &s.ChatID, &s.AID, &s.BID, &s.BestOf, &s.Difficulty, &s.Status, &s.WinnerID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateSeries opens an active series between challenger a and target b.
func (s *Store) CreateSeries(chatID, aID, bID int64, bestOf int, difficulty string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO duel_series(chat_id, a_id, b_id, best_of, difficulty) VALUES(?,?,?,?,?)`,
		chatID, aID, bID, bestOf, difficulty)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetGameSeries attaches a game to a series.
func (s *Store) SetGameSeries(gameID, seriesID int64) error {
	_, err := s.db.Exec(`UPDATE games SET series_id=? WHERE id=?`, seriesID, gameID)
	return err
}

// SeriesByID fetches a series, nil when there is none.
func (s *Store) SeriesByID(id int64) (*Series, error) {
	return scanSeries(s.db.QueryRow(`SELECT `+seriesCols+` FROM duel_series WHERE id=?`, id))
}

// SeriesOfGame returns the series a game belongs to, nil for a single duel or
// a regular game.
func (s *Store) SeriesOfGame(gameID int64) (*Series, error) {
	return scanSeries(s.db.QueryRow(`SELECT `+seriesCols+` FROM duel_series
		WHERE id=(SELECT series_id FROM games WHERE id=?)`, gameID))
}

// SeriesGames lists a series' non-deleted games in play order.
func (s *Store) SeriesGames(seriesID int64) ([]SeriesGame, error) {
	rows, err := s.db.Query(`
		SELECT g.id, g.status,
		       COALESCE((SELECT gr.player_id FROM game_results gr
		                 WHERE gr.game_id=g.id AND gr.rank=1 AND g.status='completed'), 0)
		FROM games g
		WHERE g.series_id=? AND g.deleted=0
		ORDER BY g.id`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeriesGame
	for rows.Next() {
		var g SeriesGame
		if err := rows.Scan(&g.GameID, &g.Status, &g.WinnerID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SettleSeries recomputes an active or done series from its games: done with
// a winner once somebody has WinsNeeded, active otherwise (an edit or delete
// can take a win back). Abandoned and cancelled series are left alone. Returns
// the series as stored afterwards together with its games.
func (s *Store) SettleSeries(seriesID int64) (*Series, []SeriesGame, error) {
	se, err := s.SeriesByID(seriesID)
	if err != nil || se == nil {
		return se, nil, err
	}
	games, err := s.SeriesGames(seriesID)
	if err != nil {
		return nil, nil, err
	}
	if se.Status != "active" && se.Status != "done" {
		return se, games, nil
	}
	aWins, bWins := SeriesScore(games, se.AID, se.BID)
	need := WinsNeeded(se.BestOf)
	status, winner := "active", sql.NullInt64{}
	switch {
	case aWins >= need:
		status, winner = "done", sql.NullInt64{Int64: se.AID, Valid: true}
	case bWins >= need:
		status, winner = "done", sql.NullInt64{Int64: se.BID, Valid: true}
	}
	if status != se.Status || winner != se.WinnerID {
		if _, err := s.db.Exec(`UPDATE duel_series SET status=?, winner_id=? WHERE id=?
			AND status IN ('active','done')`, status, winner, seriesID); err != nil {
			return nil, nil, err
		}
		se.Status, se.WinnerID = status, winner
	}
	return se, games, nil
}

// SeriesPendingGame returns the series' game that is waiting to be played, nil
// when there is none.
func (s *Store) SeriesPendingGame(seriesID int64) (*Game, error) {
	return scanGame(s.db.QueryRow(`SELECT `+gameCols+` FROM games
		WHERE series_id=? AND status='pending' AND deleted=0 ORDER BY id DESC LIMIT 1`, seriesID))
}

// CancelSeriesOfGame closes the series of a challenge that never got played
// (declined, withdrawn, burnt). A series that already has a recorded game is
// left alone.
func (s *Store) CancelSeriesOfGame(gameID int64) error {
	_, err := s.db.Exec(`
		UPDATE duel_series SET status='cancelled'
		WHERE id=(SELECT series_id FROM games WHERE id=?) AND status='active'
		  AND NOT EXISTS (SELECT 1 FROM games g WHERE g.series_id=duel_series.id
		                  AND g.status='completed' AND g.deleted=0)`, gameID)
	return err
}

// AbandonStaleSeries marks abandoned every active series nobody touched for
// idleMinutes: no game of it created or recorded since, and none waiting to be
// played (a pending game ends by its own result, delete or expiry). Returns the
// ones this call closed.
func (s *Store) AbandonStaleSeries(idleMinutes int) ([]Series, error) {
	rows, err := s.db.Query(`SELECT `+seriesCols+` FROM duel_series ds
		WHERE status='active'
		  AND NOT EXISTS (SELECT 1 FROM games p WHERE p.series_id=ds.id
		                  AND p.status='pending' AND p.deleted=0)
		  AND COALESCE(
		    (SELECT MAX(COALESCE(g.completed_at, g.created_at)) FROM games g
		     WHERE g.series_id=ds.id AND g.deleted=0),
		    ds.created_at) < datetime('now', ?)`,
		"-"+strconv.Itoa(idleMinutes)+" minutes")
	if err != nil {
		return nil, err
	}
	var cands []Series
	for rows.Next() {
		se, err := scanSeries(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, *se)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []Series
	for _, se := range cands {
		ok, err := s.execOne(`UPDATE duel_series SET status='abandoned' WHERE id=? AND status='active'`, se.ID)
		if err != nil {
			return out, err
		}
		if ok {
			se.Status = "abandoned"
			out = append(out, se)
		}
	}
	return out, nil
}

// SeriesHeadToHead returns how many finished series each of a and b won
// against the other.
func (s *Store) SeriesHeadToHead(chatID, aID, bID int64) (aWins, bWins int, err error) {
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN winner_id=? THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN winner_id=? THEN 1 ELSE 0 END),0)
		FROM duel_series
		WHERE chat_id=? AND status='done'
		  AND ((a_id=? AND b_id=?) OR (a_id=? AND b_id=?))`,
		aID, bID, chatID, aID, bID, bID, aID).Scan(&aWins, &bWins)
	return aWins, bWins, err
}

// SeriesPairs returns the finished-series record of every pair of active
// players that finished at least one series, most series first.
func (s *Store) SeriesPairs(chatID int64) ([]H2HPair, error) {
	rows, err := s.db.Query(`
		SELECT MIN(ds.a_id, ds.b_id) AS lo, pa.name, MAX(ds.a_id, ds.b_id) AS hi, pb.name,
		       SUM(CASE WHEN ds.winner_id=MIN(ds.a_id, ds.b_id) THEN 1 ELSE 0 END),
		       SUM(CASE WHEN ds.winner_id=MAX(ds.a_id, ds.b_id) THEN 1 ELSE 0 END)
		FROM duel_series ds
		JOIN players pa ON pa.id = MIN(ds.a_id, ds.b_id)
		JOIN players pb ON pb.id = MAX(ds.a_id, ds.b_id)
		WHERE ds.chat_id=? AND ds.status='done' AND pa.active=1 AND pb.active=1
		GROUP BY lo, hi
		ORDER BY COUNT(*) DESC, pa.name COLLATE NOCASE, pb.name COLLATE NOCASE`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []H2HPair
	for rows.Next() {
		var p H2HPair
		if err := rows.Scan(&p.AID, &p.AName, &p.BID, &p.BName, &p.AWins, &p.BWins); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// settleSeriesOf re-settles the series of a game that was deleted or restored,
// so the series result follows its games. Best-effort: the next result render
// settles it again anyway.
func (s *Store) settleSeriesOf(gameID int64) {
	var sid sql.NullInt64
	if err := s.db.QueryRow(`SELECT series_id FROM games WHERE id=?`, gameID).Scan(&sid); err != nil || !sid.Valid {
		return
	}
	_, _, _ = s.SettleSeries(sid.Int64)
}

// StaleSeriesGame is a later series game nobody started in time.
type StaleSeriesGame struct {
	GameID    int64
	ChatID    int64
	PostMsgID sql.NullInt64
}

// ExpireStaleSeriesGames soft-deletes later series games (started by the
// «Партия N» button, so already accepted) that are still pending after the
// given minutes with no result, freeing the chat's pending slot. The series
// stays active: the button creates the game again. keep (may be nil) spares a
// game the caller knows is being played. Returns the games this call closed.
func (s *Store) ExpireStaleSeriesGames(minutes int, keep func(StaleSeriesGame) bool) ([]StaleSeriesGame, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, post_msg_id FROM games
		WHERE series_id IS NOT NULL AND status='pending' AND deleted=0
		  AND completed_at IS NULL AND created_at <= datetime('now', ?)
		  AND id > (SELECT MIN(o.id) FROM games o WHERE o.series_id = games.series_id)
		  AND NOT EXISTS (SELECT 1 FROM game_results gr WHERE gr.game_id = games.id)
		ORDER BY id`, "-"+strconv.Itoa(minutes)+" minutes")
	if err != nil {
		return nil, err
	}
	var cands []StaleSeriesGame
	for rows.Next() {
		var g StaleSeriesGame
		if err := rows.Scan(&g.GameID, &g.ChatID, &g.PostMsgID); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // one connection: release it before the UPDATEs below
	var out []StaleSeriesGame
	for _, g := range cands {
		if keep != nil && keep(g) {
			continue
		}
		ok, err := s.execOne(`UPDATE games SET deleted=1 WHERE id=? AND status='pending' AND deleted=0
			AND NOT EXISTS (SELECT 1 FROM game_results gr WHERE gr.game_id = games.id)`, g.GameID)
		if err != nil {
			return out, err
		}
		if ok {
			out = append(out, g)
		}
	}
	return out, nil
}
