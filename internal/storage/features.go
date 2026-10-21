package storage

import (
	"database/sql"
	"errors"
)

// FeatureEnabled reports whether an optional engagement mechanic is on for a
// chat. Mechanics start enabled, so a chat without a stored switch gets true.
func (s *Store) FeatureEnabled(chatID int64, feature string) (bool, error) {
	var on int
	err := s.db.QueryRow(`SELECT enabled FROM chat_features WHERE chat_id=? AND feature=?`,
		chatID, feature).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return on != 0, nil
}

// SetFeature switches an optional mechanic on or off for a chat.
func (s *Store) SetFeature(chatID int64, feature string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := s.db.Exec(`INSERT INTO chat_features(chat_id, feature, enabled) VALUES(?, ?, ?)
		ON CONFLICT(chat_id, feature) DO UPDATE SET enabled=excluded.enabled`, chatID, feature, v)
	return err
}

// Event is one row of the engagement log: something a habit mechanic showed
// or a player did with it. Zero ids and an empty detail are stored as NULL.
type Event struct {
	ChatID   int64
	GameID   int64
	PlayerID int64
	Kind     string
	Detail   string
}

// LogEvent appends an event to the engagement log.
func (s *Store) LogEvent(e Event) error {
	_, err := s.db.Exec(`INSERT INTO bot_events(chat_id, game_id, player_id, kind, detail)
		VALUES(?, ?, ?, ?, ?)`,
		e.ChatID, nullID(e.GameID), nullID(e.PlayerID), e.Kind, nullStr(e.Detail))
	return err
}

// CountEvents counts a chat's logged events of one kind.
func (s *Store) CountEvents(chatID int64, kind string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM bot_events WHERE chat_id=? AND kind=?`,
		chatID, kind).Scan(&n)
	return n, err
}

// nullID stores a zero id as NULL.
func nullID(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: v != 0}
}
