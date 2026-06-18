package storage

import (
	"database/sql"
	"strconv"
)

// StalePending is a pending game old enough to warrant a nudge.
type StalePending struct {
	GameID int64
	ChatID int64
}

// StalePendingGames returns pending games created more than minutes ago that
// have not yet been reminded about.
func (s *Store) StalePendingGames(minutes int) ([]StalePending, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id FROM games
		WHERE status='pending' AND reminded=0
		  AND created_at <= datetime('now', ?)`, negMinutes(minutes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StalePending
	for rows.Next() {
		var sp StalePending
		if err := rows.Scan(&sp.GameID, &sp.ChatID); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// MarkReminded flags a pending game as already nudged.
func (s *Store) MarkReminded(gameID int64) error {
	_, err := s.db.Exec(`UPDATE games SET reminded=1 WHERE id=?`, gameID)
	return err
}

// ChatSettings is per-chat configuration used by the reminder loop.
type ChatSettings struct {
	ChatID        int64
	TZ            string
	DailyReminder bool
	DailyTime     string // HH:MM
	LastDaily     string // YYYY-MM-DD or empty
}

// GetChat returns a single chat's settings.
func (s *Store) GetChat(chatID int64) (*ChatSettings, error) {
	var c ChatSettings
	var daily int
	err := s.db.QueryRow(`
		SELECT chat_id, tz, daily_reminder, daily_time, COALESCE(last_daily,'')
		FROM chats WHERE chat_id=?`, chatID).
		Scan(&c.ChatID, &c.TZ, &daily, &c.DailyTime, &c.LastDaily)
	if err != nil {
		return nil, err
	}
	c.DailyReminder = daily != 0
	return &c, nil
}

// ChatAdmin returns the recorded admin user id (0 if unset).
func (s *Store) ChatAdmin(chatID int64) (int64, error) {
	var admin sql.NullInt64
	err := s.db.QueryRow(`SELECT admin_id FROM chats WHERE chat_id=?`, chatID).Scan(&admin)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return admin.Int64, nil
}

// AllChats returns settings for every known chat.
func (s *Store) AllChats() ([]ChatSettings, error) {
	rows, err := s.db.Query(`
		SELECT chat_id, tz, daily_reminder, daily_time, COALESCE(last_daily,'')
		FROM chats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatSettings
	for rows.Next() {
		var c ChatSettings
		var daily int
		if err := rows.Scan(&c.ChatID, &c.TZ, &daily, &c.DailyTime, &c.LastDaily); err != nil {
			return nil, err
		}
		c.DailyReminder = daily != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CompletedGamesOn counts games completed on a given local date (YYYY-MM-DD).
// The comparison uses the chat timezone offset applied by the caller via date.
func (s *Store) CompletedToday(chatID int64, date string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games
		WHERE chat_id=? AND status='completed' AND date(completed_at)=?`,
		chatID, date).Scan(&n)
	return n, err
}

// SetLastDaily records that the daily nudge fired for a chat on a date.
func (s *Store) SetLastDaily(chatID int64, date string) error {
	_, err := s.db.Exec(`UPDATE chats SET last_daily=? WHERE chat_id=?`, date, chatID)
	return err
}

// SetDailyReminder toggles the daily nudge and optionally its time.
func (s *Store) SetDailyReminder(chatID int64, on bool, hhmm string) error {
	b := 0
	if on {
		b = 1
	}
	if hhmm != "" {
		_, err := s.db.Exec(
			`UPDATE chats SET daily_reminder=?, daily_time=? WHERE chat_id=?`, b, hhmm, chatID)
		return err
	}
	_, err := s.db.Exec(`UPDATE chats SET daily_reminder=? WHERE chat_id=?`, b, chatID)
	return err
}

// negMinutes formats a negative minute offset for SQLite's datetime modifier.
func negMinutes(minutes int) string {
	return "-" + strconv.Itoa(minutes) + " minutes"
}
