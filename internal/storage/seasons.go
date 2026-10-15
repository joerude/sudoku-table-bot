package storage

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// ErrSeasonNotActive is returned by CloseSeason for a season that is not
// active (already archived), so it was left untouched.
var ErrSeasonNotActive = errors.New("season is not active")

// Season is a points race that ends when someone reaches Target.
type Season struct {
	ID          int64
	ChatID      int64
	Number      int
	Target      int
	PointsTable []int
	Status      string
	// MinGames is how many seasonal games must be played before Deadline is
	// allowed to close the season. Reaching Target ignores it.
	MinGames int
	// Deadline is when the season ends by the calendar (UTC "2006-01-02 15:04:05").
	// Invalid = not assigned yet; the bot's reminder tick fills it in.
	Deadline sql.NullString
}

// seasonCols is the column list every season read shares, in scanSeason order.
const seasonCols = "id, chat_id, number, target, points_table, status, min_games, deadline"

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
		`INSERT INTO seasons(chat_id, number, target, min_games, points_table, status)
		 VALUES(?, 1, ?, ?, ?, 'active')
		 ON CONFLICT(chat_id) WHERE status='active' DO NOTHING`,
		chatID, domain.DefaultTarget, domain.DefaultMinGames, string(pts)); err != nil {
		return nil, err
	}
	return s.activeSeasonRow(chatID)
}

// activeSeasonRow reads the chat's active season (sql.ErrNoRows if none).
func (s *Store) activeSeasonRow(chatID int64) (*Season, error) {
	return s.scanSeason(s.db.QueryRow(
		`SELECT `+seasonCols+` FROM seasons
		 WHERE chat_id=? AND status='active' ORDER BY number DESC LIMIT 1`, chatID))
}

// SeasonByNumber returns a chat's season with the given display number, any
// status (nil when absent). Numbers are unique per chat after the legacy
// import; ORDER BY id DESC picks the newest if history ever produced a dup.
func (s *Store) SeasonByNumber(chatID int64, number int) (*Season, error) {
	se, err := s.scanSeason(s.db.QueryRow(
		`SELECT `+seasonCols+` FROM seasons
		 WHERE chat_id=? AND number=? ORDER BY id DESC LIMIT 1`, chatID, number))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return se, err
}

// SeasonMeta returns a season's completed-game count, first/last game dates
// (YYYY-MM-DD, empty when no games) and the winner's name ("" when unset).
func (s *Store) SeasonMeta(chatID, seasonID int64) (games int, first, last, winner string, err error) {
	err = s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(MIN(date(g.completed_at)),''), COALESCE(MAX(date(g.completed_at)),'')
		FROM games g WHERE g.chat_id=? AND g.season_id=? AND `+sqlSeasonalGames,
		chatID, seasonID).Scan(&games, &first, &last)
	if err != nil {
		return 0, "", "", "", err
	}
	var w sql.NullString
	err = s.db.QueryRow(`
		SELECT p.name FROM seasons se JOIN players p ON p.id = se.winner_id
		WHERE se.id=?`, seasonID).Scan(&w)
	if err != nil && err != sql.ErrNoRows {
		return 0, "", "", "", err
	}
	return games, first, last, w.String, nil
}

// ArchivedNumbers lists a chat's archived season numbers in ascending order.
func (s *Store) ArchivedNumbers(chatID int64) ([]int, error) {
	rows, err := s.db.Query(
		`SELECT number FROM seasons WHERE chat_id=? AND status='archived' ORDER BY number`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SeasonByID fetches a season by id.
func (s *Store) SeasonByID(id int64) (*Season, error) {
	return s.scanSeason(s.db.QueryRow(
		`SELECT `+seasonCols+` FROM seasons WHERE id=?`, id))
}

// CreateSeason inserts a new active season with the default games floor.
func (s *Store) CreateSeason(chatID int64, number, target int, table []int) (*Season, error) {
	pts, _ := json.Marshal(table)
	res, err := s.db.Exec(
		`INSERT INTO seasons(chat_id, number, target, min_games, points_table, status)
		 VALUES(?,?,?,?,?, 'active')`, chatID, number, target, domain.DefaultMinGames, string(pts))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Season{ID: id, ChatID: chatID, Number: number, Target: target,
		MinGames: domain.DefaultMinGames, PointsTable: table, Status: "active"}, nil
}

// CloseSeason archives a season and records its winner, then opens the next one
// inheriting the same target, games floor and points table. Returns the new season.
// Only an active season can be closed: for an already archived one (a game of
// it was edited later) it returns ErrSeasonNotActive and changes nothing, so
// the archive keeps its winner and no extra season is opened.
func (s *Store) CloseSeason(se *Season, winnerID int64) (*Season, error) {
	res, err := s.db.Exec(
		`UPDATE seasons SET status='archived', winner_id=?, ended_at=datetime('now')
		 WHERE id=? AND status='active'`, winnerID, se.ID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, ErrSeasonNotActive
	}
	minGames := se.MinGames
	if minGames < 1 {
		minGames = domain.DefaultMinGames
	}
	pts, _ := json.Marshal(se.PointsTable)
	if _, err := s.db.Exec(
		`INSERT INTO seasons(chat_id, number, target, min_games, points_table, status)
		 VALUES(?, ?, ?, ?, ?, 'active')
		 ON CONFLICT(chat_id) WHERE status='active' DO NOTHING`,
		se.ChatID, se.Number+1, se.Target, minGames, string(pts)); err != nil {
		return nil, err
	}
	return s.activeSeasonRow(se.ChatID)
}

// UpdateSeasonTarget changes the active season's points target.
func (s *Store) UpdateSeasonTarget(seasonID int64, target int) error {
	_, err := s.db.Exec(`UPDATE seasons SET target=? WHERE id=?`, target, seasonID)
	return err
}

// UpdateSeasonMinGames changes how many games the season needs before its
// deadline may close it.
func (s *Store) UpdateSeasonMinGames(seasonID int64, minGames int) error {
	_, err := s.db.Exec(`UPDATE seasons SET min_games=? WHERE id=?`, minGames, seasonID)
	return err
}

// SeasonGameCount returns how many games count toward the season's games floor:
// completed, not deleted, not duels (the same set Standings scores).
func (s *Store) SeasonGameCount(chatID, seasonID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM games g WHERE g.chat_id=? AND g.season_id=? AND `+sqlSeasonalGames,
		chatID, seasonID).Scan(&n)
	return n, err
}

// SetSeasonDeadline stores when the season ends by the calendar (UTC
// "2006-01-02 15:04:05").
func (s *Store) SetSeasonDeadline(seasonID int64, deadline string) error {
	_, err := s.db.Exec(`UPDATE seasons SET deadline=? WHERE id=?`, deadline, seasonID)
	return err
}

// HasOpenSeasonGame reports whether the season has a live pending game (not a
// duel) that was started before createdBefore (UTC datetime) and is younger
// than maxAgeMinutes. Such a game was begun inside the season and may still be
// recorded into it, so closing the season has to wait for it.
func (s *Store) HasOpenSeasonGame(seasonID int64, createdBefore string, maxAgeMinutes int) (bool, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games g
		WHERE g.season_id=? AND g.status='pending' AND g.deleted=0
		  AND g.duel_target_id IS NULL
		  AND g.created_at < ? AND g.created_at > datetime('now', ?)`,
		seasonID, createdBefore, negMinutes(maxAgeMinutes)).Scan(&n)
	return n > 0, err
}

// SeasonNudge returns the marker of the last mid-season post sent for the
// season ("" = none yet). The bot keys it by kind and deadline so a nudge fires
// once per deadline and survives a restart.
func (s *Store) SeasonNudge(seasonID int64) (string, error) {
	var v sql.NullString
	err := s.db.QueryRow(`SELECT last_nudge FROM seasons WHERE id=?`, seasonID).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v.String, err
}

// SetSeasonNudge records the last mid-season post sent for the season.
func (s *Store) SetSeasonNudge(seasonID int64, marker string) error {
	_, err := s.db.Exec(`UPDATE seasons SET last_nudge=? WHERE id=?`, marker, seasonID)
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
	if err := row.Scan(&se.ID, &se.ChatID, &se.Number, &se.Target, &ptsJSON, &se.Status,
		&se.MinGames, &se.Deadline); err != nil {
		return nil, err
	}
	if se.MinGames < 1 {
		se.MinGames = domain.DefaultMinGames
	}
	if err := json.Unmarshal([]byte(ptsJSON), &se.PointsTable); err != nil {
		se.PointsTable = domain.DefaultPointsTable
	}
	return &se, nil
}

// SeasonStartedAt returns when a season started (UTC "2006-01-02 15:04:05").
func (s *Store) SeasonStartedAt(seasonID int64) (string, error) {
	var at string
	err := s.db.QueryRow(`SELECT started_at FROM seasons WHERE id=?`, seasonID).Scan(&at)
	return at, err
}

// SeasonGameTimes returns completed_at (UTC "2006-01-02 15:04:05") of every
// game that counts toward a season (completed, not deleted, not a duel),
// oldest first.
func (s *Store) SeasonGameTimes(chatID, seasonID int64) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT g.completed_at FROM games g
		WHERE g.chat_id=? AND g.season_id=? AND `+sqlSeasonalGames+`
		ORDER BY g.completed_at, g.id`, chatID, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, at)
	}
	return out, rows.Err()
}
