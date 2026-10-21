package storage

import "database/sql"

// SetMeet records that a player will come to a pending game's room at a later
// time (UTC "2006-01-02 15:04:05"). The first agreement stands: false when the
// game already has one, or is no longer a live pending game.
func (s *Store) SetMeet(gameID int64, at string, playerID int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE games SET meet_at=?, meet_by=?
		WHERE id=? AND meet_at IS NULL AND status='pending' AND deleted=0`, at, playerID, gameID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// DueMeet is an agreed meet whose time has come and whose ping is still owed.
type DueMeet struct {
	GameID    int64
	ChatID    int64
	Code      string
	CreatedBy int64 // tg id of the game's creator, 0 if unknown
	MeetBy    int64 // player id of who promised to come
	MeetAt    string
	PostMsgID int64 // the game's card, 0 if none
}

// meetPingGrace is how late a meet may still be pinged. Past it the ping is
// noise: a bot outage over the meet, or a game reopened by an edit days later.
const meetPingGrace = "-15 minutes"

// DueMeets lists live pending games whose meet time is at or before now (UTC),
// at most meetPingGrace ago, and not pinged yet.
func (s *Store) DueMeets(now string) ([]DueMeet, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, COALESCE(usdoku_code, ''), COALESCE(created_by, 0), meet_by,
		       meet_at, COALESCE(post_msg_id, 0)
		FROM games
		WHERE meet_at IS NOT NULL AND meet_at <= ? AND meet_at >= datetime(?, ?)
		  AND meet_done = 0
		  AND status = 'pending' AND deleted = 0
		ORDER BY meet_at`, now, now, meetPingGrace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueMeet
	for rows.Next() {
		var m DueMeet
		var by sql.NullInt64
		if err := rows.Scan(&m.GameID, &m.ChatID, &m.Code, &m.CreatedBy, &by, &m.MeetAt, &m.PostMsgID); err != nil {
			return nil, err
		}
		m.MeetBy = by.Int64
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkMeetDone claims a meet's ping: true only for the first caller, so the
// ping goes out at most once even if sending it fails.
func (s *Store) MarkMeetDone(gameID int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE games SET meet_done=1 WHERE id=? AND meet_done=0`, gameID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
