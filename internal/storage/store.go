// Package storage is the SQLite persistence layer: schema bootstrap + queries.
package storage

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)
)

//go:embed schema.sql
var schemaSQL string

// Store wraps the database connection.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite handles a single writer; one connection avoids "database is locked".
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	migrate(db)
	return &Store{db: db}, nil
}

// migrate applies best-effort, idempotent column additions for databases created
// by older schema versions. "duplicate column" errors are expected and ignored.
func migrate(db *sql.DB) {
	for _, stmt := range []string{
		`ALTER TABLE players ADD COLUMN usdoku_nick TEXT`,
		`ALTER TABLE games ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE chats ADD COLUMN min_players INTEGER NOT NULL DEFAULT 2`,
		`ALTER TABLE game_results ADD COLUMN duration_secs INTEGER`,
		`ALTER TABLE players ADD COLUMN username TEXT`,
		`ALTER TABLE games ADD COLUMN duel_target_id INTEGER`,
		`UPDATE seasons SET status='archived'
		 WHERE status='active' AND id NOT IN (
		     SELECT id FROM (
		         SELECT s.id,
		                ROW_NUMBER() OVER (PARTITION BY s.chat_id
		                                   ORDER BY COUNT(g.id) DESC, s.id ASC) rn
		         FROM seasons s
		         LEFT JOIN games g
		                ON g.season_id = s.id AND g.status='completed' AND g.deleted=0
		         WHERE s.status='active'
		         GROUP BY s.id
		     ) WHERE rn = 1
		 )`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_season
		     ON seasons(chat_id) WHERE status='active'`,
		`ALTER TABLE chats ADD COLUMN weekly_digest INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE chats ADD COLUMN last_weekly TEXT`,
	} {
		_, _ = db.Exec(stmt)
	}
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
