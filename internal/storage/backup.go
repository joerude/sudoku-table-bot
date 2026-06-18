package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// BackupTo writes a clean, single-file copy of the database to path (which must
// not already exist). Uses SQLite's VACUUM INTO, so the copy has no WAL sidecar
// and is safe to transfer to another machine.
func (s *Store) BackupTo(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// ValidateDB checks that path is a SQLite database that looks like ours.
func ValidateDB(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='players'`).
		Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errors.New("not a sudoku database (no players table)")
	}
	return nil
}

// ApplyPendingRestore swaps in a database uploaded via /restore. The bot writes
// the incoming file to "<dbPath>.incoming" and exits; on the next startup this
// validates and atomically moves it into place (clearing stale WAL/SHM), so the
// swap never happens while the database is open.
func ApplyPendingRestore(dbPath string) error {
	incoming := dbPath + ".incoming"
	if _, err := os.Stat(incoming); err != nil {
		return nil // nothing pending
	}
	if err := ValidateDB(incoming); err != nil {
		_ = os.Remove(incoming)
		return fmt.Errorf("pending restore invalid, discarded: %w", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}
	if err := os.Rename(incoming, dbPath); err != nil {
		return err
	}
	return nil
}
