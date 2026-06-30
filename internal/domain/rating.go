package domain

import (
	"math"
	"sort"
)

// Rating tuning. The eternal ELO ladder is a pure function of game history.
const (
	StartRating      = 1000
	ProvisionalGames = 10
	KProvisional     = 64
	KEstablished     = 32
)

// RatingResult is one participant's finish: Rank 0 = DNF, >=1 = finishing place.
type RatingResult struct {
	PlayerID int64
	Rank     int
}

// RatingGame is one completed game feeding the rating, in chronological order.
type RatingGame struct {
	ID           int64
	Participants []RatingResult
}

// PlayerRating is a player's standing on the eternal ladder.
type PlayerRating struct {
	PlayerID    int64
	Rating      int
	Peak        int
	Games       int
	Provisional bool
}

// GameRating is the rating impact of a single game (drives the result-post footer).
type GameRating struct {
	GameID      int64
	Delta       map[int64]int // playerID -> rating change this game
	NewRating   map[int64]int // playerID -> rating after this game
	CrownBefore int64         // top playerID before this game (0 = none)
	CrownAfter  int64         // top playerID after this game (0 = none)
}

// Ratings is the full result of replaying a chat's game history.
type Ratings struct {
	Players map[int64]PlayerRating
	Ladder  []PlayerRating // rating desc, peak desc, id asc
	Crown   int64          // top playerID (0 = none)
	PerGame []GameRating   // one entry per game with rating impact, chronological
}

func kFactor(gamesPlayed int) float64 {
	if gamesPlayed < ProvisionalGames {
		return KProvisional
	}
	return KEstablished
}

func expectedScore(rA, rB int) float64 {
	return 1 / (1 + math.Pow(10, float64(rB-rA)/400))
}

// outcome returns {winnerID, loserID} and ok. No pair when both DNF (ignored).
func outcome(a, b RatingResult) (win, lose int64, ok bool) {
	aF, bF := a.Rank >= 1, b.Rank >= 1
	switch {
	case aF && bF:
		if a.Rank == b.Rank {
			return 0, 0, false // ranks are unique in practice
		}
		if a.Rank < b.Rank {
			return a.PlayerID, b.PlayerID, true
		}
		return b.PlayerID, a.PlayerID, true
	case aF && !bF:
		return a.PlayerID, b.PlayerID, true // finisher beats DNF
	case !aF && bF:
		return b.PlayerID, a.PlayerID, true
	default:
		return 0, 0, false // both DNF
	}
}

// topPlayer returns the highest-rated player. On an exact tie the incumbent keeps
// the crown; otherwise ties break by lowest playerID for determinism.
func topPlayer(rating map[int64]int, incumbent int64) int64 {
	if len(rating) == 0 {
		return 0
	}
	bestR := math.MinInt
	for _, r := range rating {
		if r > bestR {
			bestR = r
		}
	}
	if incumbent != 0 && rating[incumbent] == bestR {
		return incumbent
	}
	best := int64(0)
	for id, r := range rating {
		if r == bestR && (best == 0 || id < best) {
			best = id
		}
	}
	return best
}

// ComputeRatings replays games in order, deriving current ratings, peaks, the
// crown, and per-game deltas. Pure and deterministic: same input -> same output.
func ComputeRatings(games []RatingGame) Ratings {
	type pair struct{ win, lose int64 }

	rating := map[int64]int{}
	peak := map[int64]int{}
	played := map[int64]int{}
	ensure := func(id int64) {
		if _, ok := rating[id]; !ok {
			rating[id] = StartRating
			peak[id] = StartRating
		}
	}

	var perGame []GameRating
	crown := int64(0)

	for _, g := range games {
		var pairs []pair
		for i := 0; i < len(g.Participants); i++ {
			for j := i + 1; j < len(g.Participants); j++ {
				if w, l, ok := outcome(g.Participants[i], g.Participants[j]); ok {
					pairs = append(pairs, pair{win: w, lose: l})
				}
			}
		}
		if len(pairs) == 0 {
			continue // solo / all-DNF: no impact, no footer entry
		}

		opp := map[int64]int{}
		for _, pr := range pairs {
			ensure(pr.win)
			ensure(pr.lose)
			opp[pr.win]++
			opp[pr.lose]++
		}

		before := make(map[int64]int, len(opp))
		for id := range opp {
			before[id] = rating[id]
		}
		crownBefore := crown

		delta := make(map[int64]float64, len(opp))
		for _, pr := range pairs {
			kW := kFactor(played[pr.win]) / float64(opp[pr.win])
			kL := kFactor(played[pr.lose]) / float64(opp[pr.lose])
			eW := expectedScore(before[pr.win], before[pr.lose])
			eL := expectedScore(before[pr.lose], before[pr.win])
			delta[pr.win] += kW * (1 - eW)
			delta[pr.lose] += kL * (0 - eL)
		}

		gr := GameRating{
			GameID:      g.ID,
			Delta:       make(map[int64]int, len(opp)),
			NewRating:   make(map[int64]int, len(opp)),
			CrownBefore: crownBefore,
		}
		for id := range opp {
			d := int(math.Round(delta[id]))
			rating[id] = before[id] + d
			if rating[id] > peak[id] {
				peak[id] = rating[id]
			}
			played[id]++
			gr.Delta[id] = d
			gr.NewRating[id] = rating[id]
		}
		crown = topPlayer(rating, crown)
		gr.CrownAfter = crown
		perGame = append(perGame, gr)
	}

	players := make(map[int64]PlayerRating, len(rating))
	ladder := make([]PlayerRating, 0, len(rating))
	for id, r := range rating {
		pr := PlayerRating{
			PlayerID:    id,
			Rating:      r,
			Peak:        peak[id],
			Games:       played[id],
			Provisional: played[id] < ProvisionalGames,
		}
		players[id] = pr
		ladder = append(ladder, pr)
	}
	sort.Slice(ladder, func(i, j int) bool {
		if ladder[i].Rating != ladder[j].Rating {
			return ladder[i].Rating > ladder[j].Rating
		}
		if ladder[i].Peak != ladder[j].Peak {
			return ladder[i].Peak > ladder[j].Peak
		}
		return ladder[i].PlayerID < ladder[j].PlayerID
	})

	return Ratings{Players: players, Ladder: ladder, Crown: crown, PerGame: perGame}
}
