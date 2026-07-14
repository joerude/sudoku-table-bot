package storage

import "database/sql"

// DuelStanding is one player's all-time duel record.
type DuelStanding struct {
	PlayerID int64
	Name     string
	Wins     int
	Losses   int
	Elo      int // filled by the bot layer from duel history; 0 = not computed
}

// DuelRecord returns a player's all-time duel wins (rank 1) and losses (rank > 1)
// across completed, non-deleted duel games in a chat.
func (s *Store) DuelRecord(chatID, playerID int64) (wins, losses int, err error) {
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN gr.rank=1 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN gr.rank<>1 THEN 1 ELSE 0 END),0)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE gr.player_id=? AND g.chat_id=? AND `+sqlDuelGames,
		playerID, chatID).Scan(&wins, &losses)
	return wins, losses, err
}

// DuelLeaderboard ranks active players by all-time duel wins (then win rate).
func (s *Store) DuelLeaderboard(chatID int64) ([]DuelStanding, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name,
		       COALESCE(SUM(CASE WHEN gr.rank=1 THEN 1 ELSE 0 END),0) AS wins,
		       COALESCE(SUM(CASE WHEN gr.rank<>1 THEN 1 ELSE 0 END),0) AS losses
		FROM players p
		JOIN game_results gr ON gr.player_id = p.id
		JOIN games g ON g.id = gr.game_id
		WHERE p.chat_id=? AND p.active=1 AND `+sqlDuelGames+`
		GROUP BY p.id, p.name
		HAVING (wins + losses) > 0
		ORDER BY wins DESC, (CAST(wins AS REAL)/(wins+losses)) DESC, p.name COLLATE NOCASE`,
		chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuelStanding
	for rows.Next() {
		var d DuelStanding
		if err := rows.Scan(&d.PlayerID, &d.Name, &d.Wins, &d.Losses); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkDuelAccepted stamps when the target accepted the challenge. Keeps the
// first stamp — re-taps and later edits don't move it.
func (s *Store) MarkDuelAccepted(gameID int64) error {
	_, err := s.db.Exec(
		`UPDATE games SET accepted_at=datetime('now') WHERE id=? AND accepted_at IS NULL`, gameID)
	return err
}

// MarkDuelDeclined stamps when the target declined the challenge (the game is
// soft-deleted right after; the stamp is what tells a decline from a cancel).
func (s *Store) MarkDuelDeclined(gameID int64) error {
	_, err := s.db.Exec(
		`UPDATE games SET declined_at=datetime('now') WHERE id=? AND declined_at IS NULL`, gameID)
	return err
}

// ChallengeStat is one player's duel challenge behaviour. Issued/Received
// count live, completed and declined challenges (challenger-cancelled ones
// don't count); Declined counts challenges the player turned down.
type ChallengeStat struct {
	PlayerID int64
	Name     string
	Issued   int
	Received int
	Declined int
}

// sqlChallenge: a duel challenge that was really made — anything except a
// challenge its creator deleted before an answer (deleted without a decline).
const sqlChallenge = `g.duel_target_id IS NOT NULL AND (g.deleted=0 OR g.declined_at IS NOT NULL)`

// ChallengeStats returns per-player challenge behaviour for a chat's active
// players, most challenges issued first. Players with no counted challenge
// are omitted. Issued matches on created_by (a Telegram id), so challenges
// created before created_by existed count only toward Received.
func (s *Store) ChallengeStats(chatID int64) ([]ChallengeStat, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.created_by=p.tg_id AND `+sqlChallenge+`) AS issued,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.duel_target_id=p.id AND `+sqlChallenge+`) AS received,
		       (SELECT COUNT(*) FROM games g WHERE g.chat_id=p.chat_id
		          AND g.duel_target_id=p.id AND g.declined_at IS NOT NULL) AS declined
		FROM players p
		WHERE p.chat_id=? AND p.active=1
		ORDER BY issued DESC, p.name COLLATE NOCASE`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChallengeStat
	for rows.Next() {
		var c ChallengeStat
		if err := rows.Scan(&c.PlayerID, &c.Name, &c.Issued, &c.Received, &c.Declined); err != nil {
			return nil, err
		}
		if c.Issued == 0 && c.Received == 0 && c.Declined == 0 {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DuelPair is one finished duel with both participants known, for Elo replay.
type DuelPair struct {
	GameID   int64
	WinnerID int64
	LoserID  int64
}

// DuelPairs returns all finished duels of a chat in chronological order
// (oldest first). Duels where either side is missing from the results (e.g.
// only the winner was recorded) are skipped — Elo needs both players.
func (s *Store) DuelPairs(chatID int64) ([]DuelPair, error) {
	rows, err := s.db.Query(`
		SELECT g.id,
		       MAX(CASE WHEN gr.rank=1 THEN gr.player_id END) AS winner,
		       MAX(CASE WHEN gr.rank<>1 THEN gr.player_id END) AS loser
		FROM games g
		JOIN game_results gr ON gr.game_id = g.id
		WHERE g.chat_id=? AND `+sqlDuelGames+`
		GROUP BY g.id
		ORDER BY g.id`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuelPair
	for rows.Next() {
		var (
			p             DuelPair
			winner, loser sql.NullInt64
		)
		if err := rows.Scan(&p.GameID, &winner, &loser); err != nil {
			return nil, err
		}
		if !winner.Valid || !loser.Valid {
			continue
		}
		p.WinnerID, p.LoserID = winner.Int64, loser.Int64
		out = append(out, p)
	}
	return out, rows.Err()
}

// DuelMatch is one finished duel for the recent-duels log.
type DuelMatch struct {
	CompletedAt string // UTC datetime "2006-01-02 15:04:05" (render converts to chat TZ)
	Winner      string
	Loser       string // "" if the opponent did not finish / wasn't recorded
}

// RecentDuels returns the most recent finished duels (newest first), with the
// winner (rank 1) and loser (rank 2, may be absent).
func (s *Store) RecentDuels(chatID int64, n int) ([]DuelMatch, error) {
	rows, err := s.db.Query(`
		SELECT g.completed_at AS d,
		       MAX(CASE WHEN gr.rank=1 THEN p.name END) AS winner,
		       MAX(CASE WHEN gr.rank<>1 THEN p.name END) AS loser
		FROM games g
		JOIN game_results gr ON gr.game_id = g.id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id=? AND `+sqlDuelGames+`
		GROUP BY g.id
		ORDER BY g.id DESC
		LIMIT ?`, chatID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuelMatch
	for rows.Next() {
		var (
			m             DuelMatch
			winner, loser sql.NullString
		)
		if err := rows.Scan(&m.CompletedAt, &winner, &loser); err != nil {
			return nil, err
		}
		m.Winner = winner.String
		m.Loser = loser.String
		out = append(out, m)
	}
	return out, rows.Err()
}

// H2HPair is one pair's mutual duel record — wins in duels where BOTH played.
// A is the lower player id, B the higher (deterministic pairing).
type H2HPair struct {
	AID, BID     int64
	AName, BName string
	AWins, BWins int
}

// HeadToHeadAll returns the head-to-head record of every pair of active players
// that has played at least one completed duel against each other. Ordered by
// games played (desc) then names. Solo duels (only one side recorded) don't
// contribute — a pair needs both players in the same game.
func (s *Store) HeadToHeadAll(chatID int64) ([]H2HPair, error) {
	rows, err := s.db.Query(`
		SELECT gra.player_id, pa.name, grb.player_id, pb.name,
		       COALESCE(SUM(CASE WHEN gra.rank=1 THEN 1 ELSE 0 END),0) AS awins,
		       COALESCE(SUM(CASE WHEN grb.rank=1 THEN 1 ELSE 0 END),0) AS bwins
		FROM game_results gra
		JOIN game_results grb ON grb.game_id = gra.game_id AND grb.player_id > gra.player_id
		JOIN games g   ON g.id = gra.game_id
		JOIN players pa ON pa.id = gra.player_id
		JOIN players pb ON pb.id = grb.player_id
		WHERE g.chat_id=? AND `+sqlDuelGames+`
		  AND pa.active=1 AND pb.active=1
		GROUP BY gra.player_id, grb.player_id
		ORDER BY COUNT(*) DESC, pa.name COLLATE NOCASE, pb.name COLLATE NOCASE`,
		chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []H2HPair
	for rows.Next() {
		var p H2HPair
		if err := rows.Scan(&p.AID, &p.AName, &p.BID, &p.BName, &p.AWins, &p.BWins); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DuelSpeed ranks active players by average solve time in duels (auto-recorded
// only; NULL durations excluded). Fastest first. Players with no timed duel do
// not appear. Aggregates across difficulties — duels are few.
func (s *Store) DuelSpeed(chatID int64) ([]SpeedRow, error) {
	rows, err := s.db.Query(`
		SELECT p.name,
		       AVG(gr.duration_secs) AS avg_secs,
		       MIN(gr.duration_secs) AS best_secs,
		       COUNT(gr.duration_secs) AS games
		FROM players p
		JOIN game_results gr ON gr.player_id = p.id
		JOIN games g ON g.id = gr.game_id
		WHERE p.chat_id=? AND p.active=1 AND `+sqlDuelGames+`
		  AND gr.duration_secs IS NOT NULL
		GROUP BY p.id, p.name
		ORDER BY avg_secs ASC, p.name COLLATE NOCASE`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpeedRow
	for rows.Next() {
		var (
			r    SpeedRow
			avg  float64
			best int
		)
		if err := rows.Scan(&r.Name, &avg, &best, &r.Games); err != nil {
			return nil, err
		}
		r.AvgSecs = int(avg + 0.5)
		r.BestSecs = best
		out = append(out, r)
	}
	return out, rows.Err()
}

// DuelSpeedFor returns one player's duel solve summary (Games == 0 when they
// have no timed duel).
func (s *Store) DuelSpeedFor(chatID, playerID int64) (*SpeedStat, error) {
	var (
		avg  sql.NullFloat64
		best sql.NullInt64
		n    int
	)
	err := s.db.QueryRow(`
		SELECT AVG(gr.duration_secs), MIN(gr.duration_secs), COUNT(gr.duration_secs)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE gr.player_id=? AND g.chat_id=? AND `+sqlDuelGames+`
		  AND gr.duration_secs IS NOT NULL`,
		playerID, chatID).Scan(&avg, &best, &n)
	if err != nil {
		return nil, err
	}
	st := &SpeedStat{Games: n}
	if avg.Valid {
		st.AvgSecs = int(avg.Float64 + 0.5)
	}
	if best.Valid {
		st.BestSecs = int(best.Int64)
	}
	return st, nil
}

// HeadToHead returns each player's duel wins in games where BOTH played.
func (s *Store) HeadToHead(chatID, aID, bID int64) (aWins, bWins int, err error) {
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(CASE WHEN gr.player_id=? AND gr.rank=1 THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN gr.player_id=? AND gr.rank=1 THEN 1 ELSE 0 END),0)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id=? AND `+sqlDuelGames+`
		  AND gr.game_id IN (
		      SELECT game_id FROM game_results WHERE player_id=?
		      INTERSECT
		      SELECT game_id FROM game_results WHERE player_id=?
		  )`,
		aID, bID, chatID, aID, bID).Scan(&aWins, &bWins)
	return aWins, bWins, err
}
