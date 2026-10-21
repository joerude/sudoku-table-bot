package storage

// Queries behind /metrics, the habit mechanics' scoreboard. Windows are
// [fromUTC, toUTC) in DB time ("2006-01-02 15:04:05").

// AbandonedRooms counts season rooms created in the window and deleted before
// anyone played them (a withdrawn or declined duel is not an empty room).
func (s *Store) AbandonedRooms(chatID int64, fromUTC, toUTC string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games g
		WHERE g.chat_id = ? AND g.deleted = 1 AND g.status = 'pending'
		  AND g.duel_target_id IS NULL
		  AND g.created_at >= ? AND g.created_at < ?`, chatID, fromUTC, toUTC).Scan(&n)
	return n, err
}

// EventsBetween returns a chat's events of one kind in the window, oldest first.
func (s *Store) EventsBetween(chatID int64, kind, fromUTC, toUTC string) ([]Event, error) {
	rows, err := s.db.Query(`
		SELECT chat_id, COALESCE(game_id, 0), COALESCE(player_id, 0), kind, COALESCE(detail, ''), at
		FROM bot_events WHERE chat_id = ? AND kind = ? AND at >= ? AND at < ?
		ORDER BY at, id`, chatID, kind, fromUTC, toUTC)
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

// MeetOutcomes counts the games that got a «Буду через…» promise in the window
// and how many of them were then played (completed and not deleted).
func (s *Store) MeetOutcomes(chatID int64, fromUTC, toUTC string) (set, played int, err error) {
	err = s.db.QueryRow(`
		SELECT COUNT(DISTINCT e.game_id),
		       COUNT(DISTINCT CASE WHEN `+sqlCompletedGames+` THEN g.id END)
		FROM bot_events e JOIN games g ON g.id = e.game_id
		WHERE e.chat_id = ? AND e.kind = 'meet_set' AND e.at >= ? AND e.at < ?`,
		chatID, fromUTC, toUTC).Scan(&set, &played)
	return set, played, err
}

// DisabledFeatures lists the mechanics a chat switched off, by key.
func (s *Store) DisabledFeatures(chatID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT feature FROM chat_features
		WHERE chat_id = ? AND enabled = 0 ORDER BY feature`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
