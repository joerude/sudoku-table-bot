package domain

import (
	"sort"
	"time"
)

// Weekly titles: nicknames for the Monday digest, each earned by the week's
// best in its category. A player holds at most one; a title whose best is
// already taken is skipped rather than handed to the runner-up, so every
// title stays true. Growth and hunter come first: the strongest player
// cannot win them, so the rest of the league usually gets a title too.
const (
	TitleGrowth   = "growth"   // most games under one's usual lag (pace lines)
	TitleFinisher = "finisher" // finished every game joined, at least titleMinFinished
	TitleHunter   = "hunter"   // most games of the season finished ahead of its leader
	TitleBreaker  = "breaker"  // stopped somebody's season win streak of titleStreak+
	TitleOwl      = "owl"      // most games finished late at night
	TitleSprinter = "sprinter" // fastest solve on the week's usual board
)

// titleOrder is the award order (see the package note above).
var titleOrder = []string{TitleGrowth, TitleFinisher, TitleHunter, TitleBreaker, TitleOwl, TitleSprinter}

const (
	titleMax         = 3
	titleMinGrowth   = 2
	titleMinFinished = 5
	titleMinHunter   = 2
	titleMinOwl      = 3
	titleStreak      = 3
	owlFromHour      = 22 // local hours [22, 4) count as late
	owlToHour        = 4
)

// TitleResult is one player's line in a game: rank 0 = did not finish, Secs 0
// = no solve time.
type TitleResult struct {
	PlayerID int64
	Rank     int
	Secs     int
}

// TitleGame is one completed game with its results in finish order.
type TitleGame struct {
	At         time.Time // completion
	SeasonID   int64
	Duel       bool
	Difficulty string
	Mode       string
	Results    []TitleResult
}

// TitleInput is what the week's titles are computed from.
type TitleInput struct {
	Games    []TitleGame // chronological; games before Since only feed the win streaks
	Since    time.Time
	Loc      *time.Location
	LeaderID int64          // leader of the active season, 0 = none
	SeasonID int64          // the active season: the hunter counts its games only
	PaceWins map[int64]int  // games with a pace line this week, per player
	Active   map[int64]bool // only active players get titles
}

// TitleAward is one title: Score is the count behind it (seconds for the
// sprinter), Total the games joined (finisher), Difficulty the board (sprinter).
type TitleAward struct {
	Kind       string
	PlayerID   int64
	Score      int
	Total      int
	Difficulty string
}

// WeekTitles awards up to titleMax titles, one per player.
func WeekTitles(in TitleInput) []TitleAward {
	cands := titleCandidates(in)
	taken := map[int64]bool{}
	var out []TitleAward
	for _, kind := range titleOrder {
		if len(out) == titleMax {
			break
		}
		for _, a := range cands[kind] { // only the category's best, ties included
			if !taken[a.PlayerID] {
				taken[a.PlayerID] = true
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// titleCandidates returns, per title, the active players sharing the best
// qualifying score, lowest id first.
func titleCandidates(in TitleInput) map[string][]TitleAward {
	week := make([]TitleGame, 0, len(in.Games))
	for _, g := range in.Games {
		if !g.At.Before(in.Since) {
			week = append(week, g)
		}
	}
	all := map[string][]TitleAward{}
	add := func(kind string, p int64, score int, a TitleAward) {
		if !in.Active[p] {
			return
		}
		a.Kind, a.PlayerID, a.Score = kind, p, score
		all[kind] = append(all[kind], a)
	}

	for p, n := range in.PaceWins {
		if n >= titleMinGrowth {
			add(TitleGrowth, p, n, TitleAward{})
		}
	}

	joined, dnf, ahead, late := map[int64]int{}, map[int64]int{}, map[int64]int{}, map[int64]int{}
	for _, g := range week {
		h := g.At.In(in.Loc).Hour()
		lateGame := h >= owlFromHour || h < owlToHour
		var leader *TitleResult
		for i := range g.Results {
			if g.Results[i].PlayerID == in.LeaderID {
				leader = &g.Results[i]
			}
		}
		for _, r := range g.Results {
			joined[r.PlayerID]++
			if r.Rank == 0 {
				dnf[r.PlayerID]++
			}
			if lateGame {
				late[r.PlayerID]++
			}
			if !g.Duel && g.SeasonID == in.SeasonID && leader != nil && r.PlayerID != in.LeaderID && r.Rank > 0 &&
				(leader.Rank == 0 || r.Rank < leader.Rank) {
				ahead[r.PlayerID]++
			}
		}
	}
	for p, n := range joined {
		if n >= titleMinFinished && dnf[p] == 0 {
			add(TitleFinisher, p, n, TitleAward{Total: n})
		}
	}
	for p, n := range ahead {
		if n >= titleMinHunter {
			add(TitleHunter, p, n, TitleAward{})
		}
	}
	for p, n := range late {
		if n >= titleMinOwl {
			add(TitleOwl, p, n, TitleAward{})
		}
	}
	for p, n := range streakBreaks(in.Games, in.Since) {
		add(TitleBreaker, p, n, TitleAward{})
	}
	if ps, secs, diff, ok := fastestOnUsualBoard(week); ok {
		for _, p := range ps {
			add(TitleSprinter, p, secs, TitleAward{Difficulty: diff})
		}
	}

	best := map[string][]TitleAward{}
	for kind, as := range all {
		sort.Slice(as, func(i, j int) bool {
			if as[i].Score != as[j].Score {
				if kind == TitleSprinter {
					return as[i].Score < as[j].Score
				}
				return as[i].Score > as[j].Score
			}
			return as[i].PlayerID < as[j].PlayerID
		})
		n := 1
		for n < len(as) && as[n].Score == as[0].Score {
			n++
		}
		best[kind] = as[:n]
	}
	return best
}

// streakBreaks counts, per player, season wins that ended somebody else's run
// of titleStreak+ wins, inside the week. Earlier games only build the runs.
func streakBreaks(games []TitleGame, since time.Time) map[int64]int {
	run := map[int64]int{}
	breaks := map[int64]int{}
	for _, g := range games {
		if g.Duel {
			continue
		}
		var winner int64
		for _, r := range g.Results {
			if r.Rank == 1 {
				winner = r.PlayerID
			}
		}
		if winner == 0 {
			continue
		}
		for _, r := range g.Results {
			if r.PlayerID != winner && run[r.PlayerID] >= titleStreak && !g.At.Before(since) {
				breaks[winner]++
			}
		}
		for _, r := range g.Results {
			if r.PlayerID == winner {
				run[r.PlayerID]++
			} else {
				run[r.PlayerID] = 0
			}
		}
	}
	return breaks
}

// fastestOnUsualBoard finds the week's fastest solve on its most played
// difficulty and mode (solve times on different boards do not compare), with
// every player who reached it.
func fastestOnUsualBoard(week []TitleGame) (players []int64, secs int, difficulty string, ok bool) {
	type board struct{ diff, mode string }
	count := map[board]int{}
	for _, g := range week {
		for _, r := range g.Results {
			if r.Rank > 0 && r.Secs > 0 {
				count[board{g.Difficulty, g.Mode}]++
				break
			}
		}
	}
	var usual board
	most := 0
	for b, n := range count {
		if n > most || (n == most && (b.diff+b.mode) < (usual.diff+usual.mode)) {
			usual, most = b, n
		}
	}
	if most == 0 {
		return nil, 0, "", false
	}
	at := map[int64]bool{}
	for _, g := range week {
		if g.Difficulty != usual.diff || g.Mode != usual.mode {
			continue
		}
		for _, r := range g.Results {
			if r.Rank < 1 || r.Secs <= 0 || (ok && r.Secs > secs) {
				continue
			}
			if !ok || r.Secs < secs {
				at, secs, ok = map[int64]bool{}, r.Secs, true
			}
			at[r.PlayerID] = true
		}
	}
	for p := range at {
		players = append(players, p)
	}
	return players, secs, usual.diff, ok
}
