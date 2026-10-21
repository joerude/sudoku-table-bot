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
// At is the UTC time ("2006-01-02 15:04:05"); empty means now.
type Event struct {
	ChatID   int64
	GameID   int64
	PlayerID int64
	Kind     string
	Detail   string
	At       string
}

// LogEvent appends an event to the engagement log.
func (s *Store) LogEvent(e Event) error {
	_, err := s.db.Exec(`INSERT INTO bot_events(chat_id, game_id, player_id, kind, detail, at)
		VALUES(?, ?, ?, ?, ?, COALESCE(?, datetime('now')))`,
		e.ChatID, nullID(e.GameID), nullID(e.PlayerID), e.Kind, nullStr(e.Detail), nullStr(e.At))
	return err
}

// RecentEvents returns a chat's newest events of one kind, newest first.
func (s *Store) RecentEvents(chatID int64, kind string, limit int) ([]Event, error) {
	rows, err := s.db.Query(`
		SELECT chat_id, COALESCE(game_id, 0), COALESCE(player_id, 0), kind, COALESCE(detail, ''), at
		FROM bot_events WHERE chat_id=? AND kind=?
		ORDER BY at DESC, id DESC LIMIT ?`, chatID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ChatID, &e.GameID, &e.PlayerID, &e.Kind, &e.Detail, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
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

// EventGamesByPlayerSince counts, per player, the distinct games with an event
// of one kind since sinceUTC. Only events of a game's current recording count
// (logged at or after its completion: an edit re-records and re-logs), and
// only for games that still stand (not deleted, not reopened).
func (s *Store) EventGamesByPlayerSince(chatID int64, kind, sinceUTC string) (map[int64]int, error) {
	rows, err := s.db.Query(`
		SELECT e.player_id, COUNT(DISTINCT e.game_id) FROM bot_events e
		JOIN games g ON g.id = e.game_id
		WHERE e.chat_id=? AND e.kind=? AND e.at >= ? AND e.player_id IS NOT NULL
		  AND `+sqlCompletedGames+` AND e.at >= g.completed_at
		GROUP BY e.player_id`, chatID, kind, sinceUTC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var p int64
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			return nil, err
		}
		out[p] = n
	}
	return out, rows.Err()
}
