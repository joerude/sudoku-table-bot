package storage

// SQL predicate fragments for the games table aliased as `g`. Centralised so the
// "which games count" rules live in one place — forgetting a clause silently
// corrupts scores. Every stats query selecting games MUST use one of these.
const (
	// sqlCompletedGames: a game that actually happened and was not removed.
	sqlCompletedGames = "g.status = 'completed' AND g.deleted = 0"
	// sqlSeasonalGames: a completed game that counts toward the season (not a duel).
	sqlSeasonalGames = sqlCompletedGames + " AND g.duel_target_id IS NULL"
	// sqlDuelGames: a completed duel game.
	sqlDuelGames = sqlCompletedGames + " AND g.duel_target_id IS NOT NULL"
)
