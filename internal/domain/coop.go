package domain

import "math"

// coopTierShares are the fractions of a season's games floor at which the
// shared goal earns its three stars.
var coopTierShares = [...]float64{0.20, 0.35, 0.50}

// CoopTiers returns how many clean boards earn each star of the season's
// shared goal: 20%, 35% and 50% of the games floor, each at least 1 and
// strictly above the one before. Nil when the season has no floor.
func CoopTiers(minGames int) []int {
	if minGames < 1 {
		return nil
	}
	tiers := make([]int, len(coopTierShares))
	for i, share := range coopTierShares {
		v := int(math.Round(float64(minGames) * share))
		if v < 1 {
			v = 1
		}
		if i > 0 && v <= tiers[i-1] {
			v = tiers[i-1] + 1
		}
		tiers[i] = v
	}
	return tiers
}

// CoopStars counts the tiers n clean boards have reached.
func CoopStars(n int, tiers []int) int {
	stars := 0
	for _, t := range tiers {
		if n >= t {
			stars++
		}
	}
	return stars
}
