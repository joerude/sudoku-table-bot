package domain

// Streak is a player's duel win run: Current is the number of wins since their
// most recent loss (0 right after a loss), Best is the longest such run ever.
type Streak struct {
	Current int
	Best    int
}

// DuelStreaks computes each player's current and best consecutive-win run from
// duels in chronological order (oldest first). Each element is a finished duel
// as {winnerID, loserID} — the same shape EloRatings consumes (from
// storage.DuelPairs). A loss resets that player's current run to 0. Players who
// only ever lost appear with a zero Streak.
func DuelStreaks(games [][2]int64) map[int64]Streak {
	cur := map[int64]int{}
	best := map[int64]int{}
	for _, g := range games {
		w, l := g[0], g[1]
		cur[w]++
		if cur[w] > best[w] {
			best[w] = cur[w]
		}
		cur[l] = 0
		if _, ok := best[l]; !ok {
			best[l] = 0 // ensure the loser is tracked even if they never won
		}
	}
	out := make(map[int64]Streak, len(best))
	for id := range best {
		out[id] = Streak{Current: cur[id], Best: best[id]}
	}
	return out
}
