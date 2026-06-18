// Package domain holds pure scoring/league logic with no I/O dependencies.
package domain

// DefaultPointsTable is the historical scheme: 1st = 3, 2nd = 1, 3rd+ = 0.
var DefaultPointsTable = []int{3, 1, 0}

// DefaultTarget is the points needed to win a season.
const DefaultTarget = 100

// PointsForRank returns the points awarded for a 1-based finishing rank given a
// season's points table. Ranks past the end of the table (and non-finishers)
// score 0. This is what makes the scheme scale to any number of players.
func PointsForRank(table []int, rank int) int {
	if rank < 1 || rank > len(table) {
		return 0
	}
	return table[rank-1]
}
