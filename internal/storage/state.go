package storage

import "database/sql"

// State returns a bot-wide marker, "" when it was never set.
func (s *Store) State(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM bot_state WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetState stores a bot-wide marker, replacing the previous value.
func (s *Store) SetState(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO bot_state(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
