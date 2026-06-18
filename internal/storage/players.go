package storage

import "database/sql"

// Player is a league participant within a chat.
type Player struct {
	ID     int64
	ChatID int64
	TgID   sql.NullInt64
	Name   string
}

// EnsureChat inserts a chat row on first contact, recording the admin.
// Existing chats are left untouched.
func (s *Store) EnsureChat(chatID, adminID int64) error {
	_, err := s.db.Exec(
		`INSERT INTO chats(chat_id, admin_id) VALUES(?, ?)
		 ON CONFLICT(chat_id) DO NOTHING`, chatID, adminID)
	return err
}

// RegisterPlayer registers or updates a player by Telegram id within a chat.
// Returns the player and whether it was newly created.
func (s *Store) RegisterPlayer(chatID, tgID int64, name string) (*Player, bool, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM players WHERE chat_id=? AND tg_id=?`, chatID, tgID).Scan(&id)
	switch {
	case err == nil:
		if _, err := s.db.Exec(
			`UPDATE players SET name=?, active=1 WHERE id=?`, name, id); err != nil {
			return nil, false, err
		}
		return &Player{ID: id, ChatID: chatID, TgID: nullInt(tgID), Name: name}, false, nil
	case err == sql.ErrNoRows:
		res, err := s.db.Exec(
			`INSERT INTO players(chat_id, tg_id, name) VALUES(?,?,?)`, chatID, tgID, name)
		if err != nil {
			return nil, false, err
		}
		id, _ := res.LastInsertId()
		return &Player{ID: id, ChatID: chatID, TgID: nullInt(tgID), Name: name}, true, nil
	default:
		return nil, false, err
	}
}

// PlayerByTg looks up an active player by Telegram id; nil if not registered.
func (s *Store) PlayerByTg(chatID, tgID int64) (*Player, error) {
	var p Player
	err := s.db.QueryRow(
		`SELECT id, chat_id, tg_id, name FROM players
		 WHERE chat_id=? AND tg_id=? AND active=1`, chatID, tgID).
		Scan(&p.ID, &p.ChatID, &p.TgID, &p.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetNick stores a player's usdoku nickname (used for auto-record matching).
func (s *Store) SetNick(playerID int64, nick string) error {
	_, err := s.db.Exec(`UPDATE players SET usdoku_nick=? WHERE id=?`, nick, playerID)
	return err
}

// PlayerByNick finds an active player by usdoku nickname (case-insensitive).
func (s *Store) PlayerByNick(chatID int64, nick string) (*Player, error) {
	var p Player
	err := s.db.QueryRow(
		`SELECT id, chat_id, tg_id, name FROM players
		 WHERE chat_id=? AND active=1 AND usdoku_nick IS NOT NULL
		   AND lower(usdoku_nick)=lower(?)`, chatID, nick).
		Scan(&p.ID, &p.ChatID, &p.TgID, &p.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPlayers returns active players of a chat, ordered by name.
func (s *Store) ListPlayers(chatID int64) ([]Player, error) {
	rows, err := s.db.Query(
		`SELECT id, chat_id, tg_id, name FROM players
		 WHERE chat_id=? AND active=1 ORDER BY name COLLATE NOCASE`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.ChatID, &p.TgID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func nullInt(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}
