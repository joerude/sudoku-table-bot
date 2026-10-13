package storage

import (
	"database/sql"
	"errors"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// ErrGameNotCompleted is returned by ReopenGame when the game is not a live
// completed one (already reopened, or soft-deleted), so nothing was changed.
var ErrGameNotCompleted = errors.New("game is not completed")

// ErrGameNotPending is returned by FinalizeGame when the game is already
// completed (or soft-deleted): someone else closed it first, nothing was changed.
var ErrGameNotPending = errors.New("game is not pending")

// Game is one recorded (or in-progress) sudoku round.
type Game struct {
	ID         int64
	ChatID     int64
	SeasonID   int64
	Status     string
	Difficulty sql.NullString
	Mode       sql.NullString
	UsdokuCode sql.NullString
	CreatedBy  sql.NullInt64 // tg id of who started the game
	CreatedAt  string        // UTC datetime "2006-01-02 15:04:05"
	Deleted    bool
	// The game's single chat card: its message id, the base text the live
	// updates append to, and the card kind ("invite" adds the RSVP button).
	PostMsgID sql.NullInt64
	PostText  sql.NullString
	PostKind  sql.NullString
}

// gameCols is the column list every Game scan shares (see scanGame).
const gameCols = `id, chat_id, season_id, status, difficulty, mode, usdoku_code, created_by,
	created_at, deleted, post_msg_id, post_text, post_kind`

func scanGame(row interface{ Scan(...any) error }) (*Game, error) {
	var g Game
	var deleted int
	err := row.Scan(&g.ID, &g.ChatID, &g.SeasonID, &g.Status, &g.Difficulty, &g.Mode,
		&g.UsdokuCode, &g.CreatedBy, &g.CreatedAt, &deleted, &g.PostMsgID, &g.PostText, &g.PostKind)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.Deleted = deleted != 0
	return &g, nil
}

// CreatePendingGame opens a new pending game for the chat's active season.
func (s *Store) CreatePendingGame(chatID, seasonID, createdBy int64, difficulty, mode string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO games(chat_id, season_id, status, difficulty, mode, created_by)
		 VALUES(?,?, 'pending', ?, ?, ?)`,
		chatID, seasonID, nullStr(difficulty), nullStr(mode), createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ActivePendingGame returns the chat's latest pending game, or nil.
func (s *Store) ActivePendingGame(chatID int64) (*Game, error) {
	return scanGame(s.db.QueryRow(
		`SELECT `+gameCols+` FROM games WHERE chat_id=? AND status='pending' AND deleted=0
		 ORDER BY id DESC LIMIT 1`, chatID))
}

// GameByID fetches a single game.
func (s *Store) GameByID(id int64) (*Game, error) {
	return scanGame(s.db.QueryRow(`SELECT `+gameCols+` FROM games WHERE id=?`, id))
}

// SetPostMsg remembers which chat message is the game's card.
func (s *Store) SetPostMsg(gameID, msgID int64) error {
	_, err := s.db.Exec(`UPDATE games SET post_msg_id=? WHERE id=?`, msgID, gameID)
	return err
}

// SetPostCard stores the card's message id together with its base text and kind,
// so live edits can be rebuilt after a restart.
func (s *Store) SetPostCard(gameID, msgID int64, text, kind string) error {
	_, err := s.db.Exec(
		`UPDATE games SET post_msg_id=?, post_text=?, post_kind=? WHERE id=?`,
		msgID, nullStr(text), nullStr(kind), gameID)
	return err
}

// SetPostText replaces the card's stored base text (e.g. after an RSVP refresh).
func (s *Store) SetPostText(gameID int64, text string) error {
	_, err := s.db.Exec(`UPDATE games SET post_text=? WHERE id=?`, nullStr(text), gameID)
	return err
}

// WatchableGame is a pending game that has a usdoku code to poll for results.
type WatchableGame struct {
	ID     int64
	ChatID int64
	Code   string
}

// PendingGamesWithCode lists pending games that carry a usdoku code, so polling
// can be resumed after a restart.
func (s *Store) PendingGamesWithCode() ([]WatchableGame, error) {
	rows, err := s.db.Query(
		`SELECT id, chat_id, usdoku_code FROM games
		 WHERE status='pending' AND deleted=0
		   AND usdoku_code IS NOT NULL AND usdoku_code <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchableGame
	for rows.Next() {
		var g WatchableGame
		if err := rows.Scan(&g.ID, &g.ChatID, &g.Code); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PickedPlayerIDs returns player ids already recorded for a game, in finish order.
func (s *Store) PickedPlayerIDs(gameID int64) ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT player_id FROM game_results WHERE game_id=? ORDER BY rank`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// HasResults reports whether the game has any recorded result row, finisher or
// DNF: a manual recording is under way (or auto-record already wrote its rows).
func (s *Store) HasResults(gameID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM game_results WHERE game_id=?`, gameID).Scan(&n)
	return n > 0, err
}

// AddPick appends a player to a game's finish order at the next rank.
func (s *Store) AddPick(gameID, playerID int64) error {
	return s.addPick(gameID, playerID, 0)
}

// AddPickTimed is AddPick with a known solve time in seconds; 0 stores NULL.
func (s *Store) AddPickTimed(gameID, playerID, durationSecs int64) error {
	return s.addPick(gameID, playerID, durationSecs)
}

// addPick takes the next rank and inserts in one statement, so two concurrent
// taps cannot both read the same MAX(rank). It only inserts into a live pending
// game (not completed, not soft-deleted); otherwise it is a no-op.
func (s *Store) addPick(gameID, playerID, durationSecs int64) error {
	var dur any
	if durationSecs > 0 {
		dur = durationSecs
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_results(game_id, player_id, rank, duration_secs)
		 SELECT g.id, ?,
		        COALESCE((SELECT MAX(rank) FROM game_results WHERE game_id=g.id), 0)+1, ?
		 FROM games g WHERE g.id=? AND g.status='pending' AND g.deleted=0`,
		playerID, dur, gameID)
	return err
}

// RemoveLastPick deletes the most recently added finisher (highest rank). It
// only touches a pending game, so a stale picker cannot edit a closed one.
func (s *Store) RemoveLastPick(gameID int64) error {
	_, err := s.db.Exec(
		`DELETE FROM game_results WHERE game_id=?
		   AND rank=(SELECT MAX(rank) FROM game_results WHERE game_id=?)
		   AND game_id IN (SELECT id FROM games WHERE id=? AND status='pending')`,
		gameID, gameID, gameID)
	return err
}

// ClearResults removes all picks of a pending game (used by reset / cancel).
// A completed game is left alone: use ReopenGame to edit it.
func (s *Store) ClearResults(gameID int64) error {
	_, err := s.db.Exec(
		`DELETE FROM game_results WHERE game_id=?
		   AND game_id IN (SELECT id FROM games WHERE id=? AND status='pending')`,
		gameID, gameID)
	return err
}

// FinalizeGame assigns points by rank from the table and marks the game
// completed. A duel closed this way counts as accepted (accepted_at is stamped
// when missing), so reopening it never turns it back into an open challenge the
// expiry ticker would burn. The status flip comes first and is conditional on
// 'pending', so of two concurrent finalizes (double tap, or manual vs
// auto-record) exactly one wins; the other gets ErrGameNotPending and changes
// nothing.
func (s *Store) FinalizeGame(gameID int64, table []int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`UPDATE games SET status='completed', completed_at=datetime('now'),
		        accepted_at=CASE WHEN duel_target_id IS NOT NULL
		                         THEN COALESCE(accepted_at, datetime('now')) ELSE accepted_at END
		 WHERE id=? AND status='pending' AND deleted=0`, gameID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrGameNotPending
	}

	rows, err := tx.Query(`SELECT id, rank FROM game_results WHERE game_id=?`, gameID)
	if err != nil {
		return err
	}
	type rr struct{ id, rank int64 }
	var all []rr
	for rows.Next() {
		var r rr
		if err := rows.Scan(&r.id, &r.rank); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range all {
		pts := domain.PointsForRank(table, int(r.rank))
		if _, err := tx.Exec(`UPDATE game_results SET points=? WHERE id=?`, pts, r.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReopenGame moves a completed game back to pending and clears its picks. The
// game is marked reminded: the creator is editing it right now, so the stale
// nudge must not rewrite the picker they are using. Returns
// ErrGameNotCompleted (changing nothing) if the game is not completed or is
// soft-deleted, so a repeated or stale "edit" cannot wipe a game that is
// already being re-recorded.
func (s *Store) ReopenGame(gameID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`UPDATE games SET status='pending', completed_at=NULL, reminded=1
		 WHERE id=? AND status='completed' AND deleted=0`, gameID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrGameNotCompleted
	}
	if _, err := tx.Exec(`DELETE FROM game_results WHERE game_id=?`, gameID); err != nil {
		return err
	}
	return tx.Commit()
}

// SoftDeleteGame marks a game as deleted so it stops counting toward standings,
// but keeps the row and its results so it can be restored.
func (s *Store) SoftDeleteGame(gameID int64) error {
	_, err := s.db.Exec(`UPDATE games SET deleted=1 WHERE id=?`, gameID)
	return err
}

// RestoreGame un-deletes a previously soft-deleted game. A pending game is not
// brought back while the chat has another live pending game (the league holds
// one at a time): restored=false then, and nothing changes. Checked in the same
// statement, so two restores cannot both win.
func (s *Store) RestoreGame(gameID int64) (restored bool, err error) {
	return s.execOne(
		`UPDATE games SET deleted=0
		 WHERE id=? AND (status<>'pending' OR NOT EXISTS (
		     SELECT 1 FROM games o
		     WHERE o.chat_id=games.chat_id AND o.status='pending' AND o.deleted=0 AND o.id<>games.id))`,
		gameID)
}

// ResultRow is one finisher of a game, joined with the player name.
type ResultRow struct {
	PlayerID int64
	Name     string
	Rank     int
	Points   int
	Duration int // solve time in seconds; 0 = unknown (manual entry)
}

// AddDNF records a player who joined but did not finish: rank 0 (scores 0,
// marks a non-finisher) with no solve time. Safe to call after finishers; a
// no-op unless the game is a live pending one.
func (s *Store) AddDNF(gameID, playerID int64) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO game_results(game_id, player_id, rank, duration_secs)
		 SELECT g.id, ?, 0, NULL FROM games g
		 WHERE g.id=? AND g.status='pending' AND g.deleted=0`,
		playerID, gameID)
	return err
}

// GameResults returns a game's finishers in rank order with names and points.
// DNF rows (rank 0) sort after all finishers.
func (s *Store) GameResults(gameID int64) ([]ResultRow, error) {
	rows, err := s.db.Query(`
		SELECT gr.player_id, p.name, gr.rank, gr.points, gr.duration_secs
		FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		WHERE gr.game_id=? ORDER BY (gr.rank=0), gr.rank`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResultRow
	for rows.Next() {
		var (
			r   ResultRow
			dur sql.NullInt64
		)
		if err := rows.Scan(&r.PlayerID, &r.Name, &r.Rank, &r.Points, &dur); err != nil {
			return nil, err
		}
		if dur.Valid {
			r.Duration = int(dur.Int64)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetPickDuration fills a finisher's solve time (seconds) if not already set.
func (s *Store) SetPickDuration(gameID, playerID, durationSecs int64) error {
	_, err := s.db.Exec(
		`UPDATE game_results SET duration_secs=?
		 WHERE game_id=? AND player_id=? AND duration_secs IS NULL`,
		durationSecs, gameID, playerID)
	return err
}

// SetUsdokuCode attaches an optional usdoku game code to a game.
func (s *Store) SetUsdokuCode(gameID int64, code string) error {
	_, err := s.db.Exec(`UPDATE games SET usdoku_code=? WHERE id=?`, code, gameID)
	return err
}

// AttachUsdokuCode binds a usdoku room to a live pending game that has none yet.
// False means nothing changed: the game already has a room, or is closed or
// deleted. Conditional so two /code calls cannot start two watchers.
func (s *Store) AttachUsdokuCode(gameID int64, code string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE games SET usdoku_code=?
		 WHERE id=? AND status='pending' AND deleted=0
		   AND (usdoku_code IS NULL OR usdoku_code='')`, code, gameID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetGameParams overwrites a game's difficulty and mode; an empty value keeps
// the current one. Used to align a game with the room it was bound to.
func (s *Store) SetGameParams(gameID int64, difficulty, mode string) error {
	_, err := s.db.Exec(
		`UPDATE games SET difficulty=COALESCE(NULLIF(?, ''), difficulty),
		                  mode=COALESCE(NULLIF(?, ''), mode)
		 WHERE id=?`, difficulty, mode, gameID)
	return err
}

// SetDuelTarget records which player a duel game challenges (NULL = open invite).
func (s *Store) SetDuelTarget(gameID, targetPlayerID int64) error {
	_, err := s.db.Exec(`UPDATE games SET duel_target_id=? WHERE id=?`, targetPlayerID, gameID)
	return err
}

// DuelPlayerIDs resolves a duel's two participants: the creator (created_by
// tg id → player) first, then the challenge target. Returns nil when the game
// is not a duel, or when the creator can't be mapped to a player (old rows) —
// callers then treat the game as open to everyone.
func (s *Store) DuelPlayerIDs(gameID int64) ([]int64, error) {
	var target, creator sql.NullInt64
	err := s.db.QueryRow(`
		SELECT g.duel_target_id,
		       (SELECT p.id FROM players p WHERE p.chat_id=g.chat_id AND p.tg_id=g.created_by)
		FROM games g WHERE g.id=?`, gameID).Scan(&target, &creator)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !target.Valid || !creator.Valid || target.Int64 == creator.Int64 {
		return nil, nil
	}
	return []int64{creator.Int64, target.Int64}, nil
}

// DuelTargetID returns the challenged player id and true, or (0, false) when the
// game is not a duel.
func (s *Store) DuelTargetID(gameID int64) (int64, bool, error) {
	var t sql.NullInt64
	err := s.db.QueryRow(`SELECT duel_target_id FROM games WHERE id=?`, gameID).Scan(&t)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if !t.Valid {
		return 0, false, nil
	}
	return t.Int64, true, nil
}

// ToggleRsvp flips a player's "in" state for a game; returns whether they are now in.
func (s *Store) ToggleRsvp(gameID, playerID int64) (bool, error) {
	var exists int
	if err := s.db.QueryRow(
		`SELECT 1 FROM game_rsvp WHERE game_id=? AND player_id=?`, gameID, playerID).
		Scan(&exists); err == nil {
		_, err := s.db.Exec(`DELETE FROM game_rsvp WHERE game_id=? AND player_id=?`, gameID, playerID)
		return false, err
	} else if err != sql.ErrNoRows {
		return false, err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO game_rsvp(game_id, player_id) VALUES(?,?)`, gameID, playerID)
	return true, err
}

// RsvpPlayers returns the active players who said they're in, ordered by name.
func (s *Store) RsvpPlayers(gameID int64) ([]Player, error) {
	rows, err := s.db.Query(
		`SELECT p.id, p.chat_id, p.tg_id, p.name, p.username, p.usdoku_nick
		 FROM game_rsvp r JOIN players p ON p.id = r.player_id
		 WHERE r.game_id=? AND p.active=1 ORDER BY p.name COLLATE NOCASE`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.ChatID, &p.TgID, &p.Name, &p.Username, &p.UsdokuNick); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
