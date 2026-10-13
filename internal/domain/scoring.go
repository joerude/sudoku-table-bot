// Package domain holds pure scoring/league logic with no I/O dependencies.
package domain

// DefaultPointsTable is the historical scheme: 1st = 3, 2nd = 1, 3rd+ = 0.
var DefaultPointsTable = []int{3, 1, 0}

// DefaultTarget is the points needed to win a season.
const DefaultTarget = 100

// DefaultMinGames is how many seasonal games must be played before the calendar
// deadline is allowed to crown anyone. It sets the league's tempo: 66 games
// across SeasonMonths months is ~1.1 games/day. Reaching the target still ends
// a season at any time; this floor guards the deadline exit only, so a month
// that was barely played extends instead of producing a hollow champion.
const DefaultMinGames = 66

// PointsForRank returns the points awarded for a 1-based finishing rank given a
// season's points table. Ranks past the end of the table (and non-finishers)
// score 0. This is what makes the scheme scale to any number of players.
func PointsForRank(table []int, rank int) int {
	if rank < 1 || rank > len(table) {
		return 0
	}
	return table[rank-1]
}
