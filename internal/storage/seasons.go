package storage

import (
	"database/sql"
	"encoding/json"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// Season is a points race that ends when someone reaches Target.
type Season struct {
	ID          int64
	ChatID      int64
	Number      int
	Target      int
	PointsTable []int
	Status      string
}

// ActiveSeason returns the chat's current active season, creating season #1 if
// none exists yet. The create is race-safe: a partial unique index guarantees
// one active season per chat, so a concurrent creator's insert no-ops and we
// read the winner's season.
func (s *Store) ActiveSeason(chatID int64) (*Season, error) {
	se, err := s.activeSeasonRow(chatID)
	if err == nil {
		return se, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	pts, _ := json.Marshal(domain.DefaultPointsTable)
	if _, err := s.db.Exec(
		`INSERT INTO seasons(chat_id, number, target, points_table, status)
		 VALUES(?, 1, ?, ?, 'active')
		 ON CONFLICT(chat_id) WHERE status='active' DO NOTHING`,
		chatID, domain.DefaultTarget, string(pts)); err != nil {
		return nil, err
	}
	return s.activeSeasonRow(chatID)
}

// activeSeasonRow reads the chat's active season (sql.ErrNoRows if none).
func (s *Store) activeSeasonRow(chatID int64) (*Season, error) {
	return s.scanSeason(s.db.QueryRow(
		`SELECT id, chat_id, number, target, points_table, status FROM seasons
		 WHERE chat_id=? AND status='active' ORDER BY number DESC LIMIT 1`, chatID))
}

// SeasonByID fetches a season by id.
func (s *Store) SeasonByID(id int64) (*Season, error) {
	return s.scanSeason(s.db.QueryRow(
		`SELECT id, chat_id, number, target, points_table, status FROM seasons WHERE id=?`, id))
}

// CreateSeason inserts a new active season.
func (s *Store) CreateSeason(chatID int64, number, target int, table []int) (*Season, error) {
	pts, _ := json.Marshal(table)
	res, err := s.db.Exec(
		`INSERT INTO seasons(chat_id, number, target, points_table, status)
		 VALUES(?,?,?,?, 'active')`, chatID, number, target, string(pts))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Season{ID: id, ChatID: chatID, Number: number, Target: target, PointsTable: table, Status: "active"}, nil
}

// CloseSeason archives a season and records its winner, then opens the next one
// inheriting the same target and points table. Returns the new season.
func (s *Store) CloseSeason(se *Season, winnerID int64) (*Season, error) {
	if _, err := s.db.Exec(
		`UPDATE seasons SET status='archived', winner_id=?, ended_at=datetime('now')
		 WHERE id=?`, winnerID, se.ID); err != nil {
		return nil, err
	}
	return s.CreateSeason(se.ChatID, se.Number+1, se.Target, se.PointsTable)
}

// UpdateSeasonTarget changes the active season's points target.
func (s *Store) UpdateSeasonTarget(seasonID int64, target int) error {
	_, err := s.db.Exec(`UPDATE seasons SET target=? WHERE id=?`, target, seasonID)
	return err
}

// UpdateSeasonPoints changes the active season's points table.
func (s *Store) UpdateSeasonPoints(seasonID int64, table []int) error {
	pts, _ := json.Marshal(table)
	_, err := s.db.Exec(`UPDATE seasons SET points_table=? WHERE id=?`, string(pts), seasonID)
	return err
}

func (s *Store) scanSeason(row *sql.Row) (*Season, error) {
	var se Season
	var ptsJSON string
	if err := row.Scan(&se.ID, &se.ChatID, &se.Number, &se.Target, &ptsJSON, &se.Status); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(ptsJSON), &se.PointsTable); err != nil {
		se.PointsTable = domain.DefaultPointsTable
	}
	return &se, nil
}
