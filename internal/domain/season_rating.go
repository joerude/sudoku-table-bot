package domain

import (
	"math"
	"sort"
)

// The season rating blends three smoothed rates. Each rate is
// (hits + prior*k) / (tries + k) with k = ratingPriorWeight: a player with few
// tries is pulled toward a neutral prior, so one lucky game cannot top the
// table, and the pull fades as games accumulate.
const (
	// Weights of the three parts; they sum to 1.
	SeasonWinWeight    = 0.50 // season games won
	SeasonFinishWeight = 0.25 // games finished instead of dropped (DNF), duels included
	SeasonDuelWeight   = 0.25 // duels won

	ratingPriorWeight = 3.0 // pseudo-games of the neutral prior in every rate

	// Neutral priors, as hits per ratingPriorWeight pseudo-games: a win is
	// rarer than a finish, so its prior sits at 1 in 3; a duel is a coin
	// flip; a finish is expected most of the time.
	winPriorHits    = 1.0
	finishPriorHits = 2.4
	duelPriorHits   = 1.5
)

// SeasonRatingInput holds one player's season counts.
type SeasonRatingInput struct {
	PlayerID    int64
	SeasonGames int // season games joined, DNF included
	SeasonWins  int // rank 1 in season games
	Played      int // season games + duels of the season joined
	Finished    int // rank > 0 across both
	Duels       int // duels of the season joined
	DuelWins    int // rank 1 in those duels
}

// SeasonRatingRow is one ranked player: the counts plus the 0..100 score.
type SeasonRatingRow struct {
	SeasonRatingInput
	Score int
}

// SeasonRating scores every player who joined at least one game or duel of the
// season (Played > 0) and orders them by score, then season wins, then player
// id. It is a pure function of the season's counts, so it is recomputed from
// scratch each season and never carried over.
func SeasonRating(in []SeasonRatingInput) []SeasonRatingRow {
	out := make([]SeasonRatingRow, 0, len(in))
	for _, p := range in {
		if p.Played <= 0 {
			continue
		}
		win := (float64(p.SeasonWins) + winPriorHits) / (float64(p.SeasonGames) + ratingPriorWeight)
		fin := (float64(p.Finished) + finishPriorHits) / (float64(p.Played) + ratingPriorWeight)
		duel := (float64(p.DuelWins) + duelPriorHits) / (float64(p.Duels) + ratingPriorWeight)
		score := 100 * (SeasonWinWeight*win + SeasonFinishWeight*fin + SeasonDuelWeight*duel)
		out = append(out, SeasonRatingRow{SeasonRatingInput: p, Score: int(math.Round(score))})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.SeasonWins != b.SeasonWins {
			return a.SeasonWins > b.SeasonWins
		}
		return a.PlayerID < b.PlayerID
	})
	return out
}
