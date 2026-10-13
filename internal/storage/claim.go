package storage

// ClaimNick binds a usdoku nick to a player unless another active player of the
// same chat already uses it (case-insensitive). One statement, so two people
// tapping «Это я» for the same nick cannot both win. False = the nick is taken.
func (s *Store) ClaimNick(playerID int64, nick string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE players SET usdoku_nick=?
		 WHERE id=? AND NOT EXISTS (
		     SELECT 1 FROM players o
		     WHERE o.chat_id=players.chat_id AND o.active=1 AND o.id<>players.id
		       AND o.usdoku_nick IS NOT NULL AND lower(o.usdoku_nick)=lower(?))`,
		nick, playerID, nick)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
