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
		 FROM games WHERE chat_id=? AND status='pending'
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
	err := s.db.QueryRow(
		`SELECT id, chat_id, season_id, status, difficulty, mode, usdoku_code
		 FROM games WHERE id=?`, id).
		Scan(&g.ID, &g.ChatID, &g.SeasonID, &g.Status, &g.Difficulty, &g.Mode, &g.UsdokuCode)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
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
		 WHERE status='pending' AND usdoku_code IS NOT NULL AND usdoku_code <> ''`)
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
	var next int
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(rank),0)+1 FROM game_results WHERE game_id=?`, gameID).
		Scan(&next); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_results(game_id, player_id, rank) VALUES(?,?,?)`,
		gameID, playerID, next)
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

// DeleteGame removes a game and its results.
func (s *Store) DeleteGame(gameID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM game_results WHERE game_id=?`, gameID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM games WHERE id=?`, gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// ResultRow is one finisher of a game, joined with the player name.
type ResultRow struct {
	Name   string
	Rank   int
	Points int
}

// GameResults returns a game's finishers in rank order with names and points.
func (s *Store) GameResults(gameID int64) ([]ResultRow, error) {
	rows, err := s.db.Query(`
		SELECT p.name, gr.rank, gr.points
		FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		WHERE gr.game_id=? ORDER BY gr.rank`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResultRow
	for rows.Next() {
		var r ResultRow
		if err := rows.Scan(&r.Name, &r.Rank, &r.Points); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetUsdokuCode attaches an optional usdoku game code to a game.
func (s *Store) SetUsdokuCode(gameID int64, code string) error {
	_, err := s.db.Exec(`UPDATE games SET usdoku_code=? WHERE id=?`, code, gameID)
	return err
}

func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
