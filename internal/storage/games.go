package storage

import (
	"database/sql"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// Game is one recorded (or in-progress) sudoku round.
type Game struct {
	ID         int64
	ChatID     int64
	SeasonID   int64
	Status     string
	Difficulty sql.NullString
	Mode       sql.NullString
	UsdokuCode sql.NullString
	Deleted    bool
}

// CreatePendingGame opens a new pending game for the chat's active season.
func (s *Store) CreatePendingGame(chatID, seasonID, createdBy int64, difficulty, mode string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO games(chat_id, season_id, status, difficulty, mode, created_by)
		 VALUES(?,?, 'pending', ?, ?, ?)`,
		chatID, seasonID, nullStr(difficulty), nullStr(mode), createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ActivePendingGame returns the chat's latest pending game, or nil.
func (s *Store) ActivePendingGame(chatID int64) (*Game, error) {
	var g Game
	err := s.db.QueryRow(
		`SELECT id, chat_id, season_id, status, difficulty, mode, usdoku_code
		 FROM games WHERE chat_id=? AND status='pending' AND deleted=0
		 ORDER BY id DESC LIMIT 1`, chatID).
		Scan(&g.ID, &g.ChatID, &g.SeasonID, &g.Status, &g.Difficulty, &g.Mode, &g.UsdokuCode)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GameByID fetches a single game.
func (s *Store) GameByID(id int64) (*Game, error) {
	var g Game
	var deleted int
	err := s.db.QueryRow(
		`SELECT id, chat_id, season_id, status, difficulty, mode, usdoku_code, deleted
		 FROM games WHERE id=?`, id).
		Scan(&g.ID, &g.ChatID, &g.SeasonID, &g.Status, &g.Difficulty, &g.Mode, &g.UsdokuCode, &deleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.Deleted = deleted != 0
	return &g, nil
}

// WatchableGame is a pending game that has a usdoku code to poll for results.
type WatchableGame struct {
	ID     int64
	ChatID int64
	Code   string
}

// PendingGamesWithCode lists pending games that carry a usdoku code, so polling
// can be resumed after a restart.
func (s *Store) PendingGamesWithCode() ([]WatchableGame, error) {
	rows, err := s.db.Query(
		`SELECT id, chat_id, usdoku_code FROM games
		 WHERE status='pending' AND deleted=0
		   AND usdoku_code IS NOT NULL AND usdoku_code <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchableGame
	for rows.Next() {
		var g WatchableGame
		if err := rows.Scan(&g.ID, &g.ChatID, &g.Code); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PickedPlayerIDs returns player ids already recorded for a game, in finish order.
func (s *Store) PickedPlayerIDs(gameID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT player_id FROM game_results WHERE game_id=? ORDER BY rank`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AddPick appends a player to a game's finish order at the next rank.
func (s *Store) AddPick(gameID, playerID int64) error {
	return s.addPick(gameID, playerID, 0)
}

// AddPickTimed is AddPick with a known solve time in seconds; 0 stores NULL.
func (s *Store) AddPickTimed(gameID, playerID, durationSecs int64) error {
	return s.addPick(gameID, playerID, durationSecs)
}

func (s *Store) addPick(gameID, playerID, durationSecs int64) error {
	var next int
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(rank),0)+1 FROM game_results WHERE game_id=?`, gameID).
		Scan(&next); err != nil {
		return err
	}
	var dur any
	if durationSecs > 0 {
		dur = durationSecs
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_results(game_id, player_id, rank, duration_secs) VALUES(?,?,?,?)`,
		gameID, playerID, next, dur)
	return err
}

// RemoveLastPick deletes the most recently added finisher (highest rank).
func (s *Store) RemoveLastPick(gameID int64) error {
	_, err := s.db.Exec(
		`DELETE FROM game_results WHERE game_id=? AND rank=(
			SELECT MAX(rank) FROM game_results WHERE game_id=?)`, gameID, gameID)
	return err
}

// ClearResults removes all picks for a game (used by reset / edit).
func (s *Store) ClearResults(gameID int64) error {
	_, err := s.db.Exec(`DELETE FROM game_results WHERE game_id=?`, gameID)
	return err
}

// FinalizeGame assigns points by rank from the table and marks the game completed.
func (s *Store) FinalizeGame(gameID int64, table []int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id, rank FROM game_results WHERE game_id=?`, gameID)
	if err != nil {
		return err
	}
	type rr struct{ id, rank int64 }
	var all []rr
	for rows.Next() {
		var r rr
		if err := rows.Scan(&r.id, &r.rank); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range all {
		pts := domain.PointsForRank(table, int(r.rank))
		if _, err := tx.Exec(`UPDATE game_results SET points=? WHERE id=?`, pts, r.id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`UPDATE games SET status='completed', completed_at=datetime('now') WHERE id=?`,
		gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReopenGame moves a completed game back to pending and clears its picks.
func (s *Store) ReopenGame(gameID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM game_results WHERE game_id=?`, gameID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE games SET status='pending', completed_at=NULL WHERE id=?`, gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// SoftDeleteGame marks a game as deleted so it stops counting toward standings,
// but keeps the row and its results so it can be restored.
func (s *Store) SoftDeleteGame(gameID int64) error {
	_, err := s.db.Exec(`UPDATE games SET deleted=1 WHERE id=?`, gameID)
	return err
}

// RestoreGame un-deletes a previously soft-deleted game.
func (s *Store) RestoreGame(gameID int64) error {
	_, err := s.db.Exec(`UPDATE games SET deleted=0 WHERE id=?`, gameID)
	return err
}

// ResultRow is one finisher of a game, joined with the player name.
type ResultRow struct {
	PlayerID int64
	Name     string
	Rank     int
	Points   int
	Duration int // solve time in seconds; 0 = unknown (manual entry)
}

// AddDNF records a player who joined but did not finish: rank 0 (scores 0,
// marks a non-finisher) with no solve time. Safe to call after finishers.
func (s *Store) AddDNF(gameID, playerID int64) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_results(game_id, player_id, rank, duration_secs) VALUES(?,?,0,NULL)`,
		gameID, playerID)
	return err
}

// GameResults returns a game's finishers in rank order with names and points.
// DNF rows (rank 0) sort after all finishers.
func (s *Store) GameResults(gameID int64) ([]ResultRow, error) {
	rows, err := s.db.Query(`
		SELECT gr.player_id, p.name, gr.rank, gr.points, gr.duration_secs
		FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		WHERE gr.game_id=? ORDER BY (gr.rank=0), gr.rank`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResultRow
	for rows.Next() {
		var (
			r   ResultRow
			dur sql.NullInt64
		)
		if err := rows.Scan(&r.PlayerID, &r.Name, &r.Rank, &r.Points, &dur); err != nil {
			return nil, err
		}
		if dur.Valid {
			r.Duration = int(dur.Int64)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetPickDuration fills a finisher's solve time (seconds) if not already set.
func (s *Store) SetPickDuration(gameID, playerID, durationSecs int64) error {
	_, err := s.db.Exec(
		`UPDATE game_results SET duration_secs=?
		 WHERE game_id=? AND player_id=? AND duration_secs IS NULL`,
		durationSecs, gameID, playerID)
	return err
}

// SetUsdokuCode attaches an optional usdoku game code to a game.
func (s *Store) SetUsdokuCode(gameID int64, code string) error {
	_, err := s.db.Exec(`UPDATE games SET usdoku_code=? WHERE id=?`, code, gameID)
	return err
}

// SetDuelTarget records which player a duel game challenges (NULL = open invite).
func (s *Store) SetDuelTarget(gameID, targetPlayerID int64) error {
	_, err := s.db.Exec(`UPDATE games SET duel_target_id=? WHERE id=?`, targetPlayerID, gameID)
	return err
}

// DuelTargetID returns the challenged player id and true, or (0, false) when the
// game is not a duel.
func (s *Store) DuelTargetID(gameID int64) (int64, bool, error) {
	var t sql.NullInt64
	err := s.db.QueryRow(`SELECT duel_target_id FROM games WHERE id=?`, gameID).Scan(&t)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if !t.Valid {
		return 0, false, nil
	}
	return t.Int64, true, nil
}

// ToggleRsvp flips a player's "in" state for a game; returns whether they are now in.
func (s *Store) ToggleRsvp(gameID, playerID int64) (bool, error) {
	var exists int
	if err := s.db.QueryRow(
		`SELECT 1 FROM game_rsvp WHERE game_id=? AND player_id=?`, gameID, playerID).
		Scan(&exists); err == nil {
		_, err := s.db.Exec(`DELETE FROM game_rsvp WHERE game_id=? AND player_id=?`, gameID, playerID)
		return false, err
	} else if err != sql.ErrNoRows {
		return false, err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO game_rsvp(game_id, player_id) VALUES(?,?)`, gameID, playerID)
	return true, err
}

// RsvpPlayers returns the active players who said they're in, ordered by name.
func (s *Store) RsvpPlayers(gameID int64) ([]Player, error) {
	rows, err := s.db.Query(
		`SELECT p.id, p.chat_id, p.tg_id, p.name, p.username, p.usdoku_nick
		 FROM game_rsvp r JOIN players p ON p.id = r.player_id
		 WHERE r.game_id=? AND p.active=1 ORDER BY p.name COLLATE NOCASE`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.ChatID, &p.TgID, &p.Name, &p.Username, &p.UsdokuNick); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
