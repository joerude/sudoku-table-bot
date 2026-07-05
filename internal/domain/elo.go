package domain

import "math"

// Elo parameters for the duel rating. Everyone starts at EloStart; eloK caps
// how far a single duel can move a rating. Ratings are derived from the full
// duel history on every read, so there is nothing to migrate or backfill.
const (
	EloStart = 1000
	eloK     = 32.0
)

// EloUpdate returns both players' ratings after winner beats loser.
func EloUpdate(winner, loser int) (newWinner, newLoser int) {
	expected := 1 / (1 + math.Pow(10, float64(loser-winner)/400))
	d := int(math.Round(eloK * (1 - expected)))
	return winner + d, loser - d
}

// EloRatings folds chronological (winnerID, loserID) duels into final ratings.
func EloRatings(pairs [][2]int64) map[int64]int {
	r := make(map[int64]int)
	get := func(id int64) int {
		if v, ok := r[id]; ok {
			return v
		}
		return EloStart
	}
	for _, p := range pairs {
		w, l := EloUpdate(get(p[0]), get(p[1]))
		r[p[0]], r[p[1]] = w, l
	}
	return r
}
