package bot

import (
	"log"
	"sort"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// badgeState gathers the career inputs a player's badges are computed from,
// plus the raw ranks (newest first), play dates, and "today" in the chat's
// timezone — so callers can also derive the pre-game state.
func (b *Bot) badgeState(chatID, playerID int64, tz string) (in domain.BadgeInput, ranks []int, dates []string, today string, err error) {
	ranks, err = b.st.RecentRanks(chatID, playerID)
	if err != nil {
		return
	}
	times, terr := b.st.PlayedTimes(chatID, playerID)
	if terr != nil {
		err = terr
		return
	}
	loc := loadLoc(tz)
	today = time.Now().In(loc).Format("2006-01-02")
	for _, t := range times {
		if parsed, e := parseDBTime(t); e == nil {
			dates = append(dates, parsed.UTC().In(loc).Format("2006-01-02"))
		}
	}
	wins, games, best, cerr := b.st.CareerStats(chatID, playerID)
	if cerr != nil {
		err = cerr
		return
	}
	seasonsWon, serr := b.st.SeasonsWon(chatID, playerID)
	if serr != nil {
		err = serr
		return
	}
	in = domain.BadgeInput{
		Wins: wins, Games: games, BestSecs: best,
		WinStreak:  domain.WinStreak(ranks),
		DayStreak:  domain.DayStreak(dates, today),
		SeasonsWon: seasonsWon,
	}
	return
}

// removeOneToday drops one occurrence of today from dates — the pre-game date
// set when the just-recorded game is today's only one. If today repeats, the
// set (and thus the day streak) is unchanged, so dates return as-is.
func removeOneToday(dates []string, today string) []string {
	n := 0
	for _, d := range dates {
		if d == today {
			n++
		}
	}
	if n != 1 {
		return dates
	}
	out := make([]string, 0, len(dates)-1)
	for _, d := range dates {
		if d != today {
			out = append(out, d)
		}
	}
	return out
}

// celebrations builds hype lines for a just-scored seasonal game: personal
// bests, win streaks, and newly earned badges. Best-effort: storage errors
// log and skip a line, never blocking the result post.
func (b *Bot) celebrations(game *storage.Game, rows []storage.ResultRow) []string {
	tz := b.chatTZ(game.ChatID)
	var lines []string
	if total, err := b.st.TotalGames(game.ChatID); err != nil {
		log.Printf("celebrations.total: %v", err)
	} else if leagueMilestones[total] {
		lines = append(lines, milestoneLeagueLine(total))
	}
	for _, r := range rows {
		if r.Rank < 1 {
			// DNF still counts as a played game, so the career-games
			// milestone applies; badges and streaks do not.
			if _, games, _, err := b.st.CareerStats(game.ChatID, r.PlayerID); err == nil && gamesMilestones[games] {
				lines = append(lines, milestoneGamesLine(r.Name, games))
			}
			continue
		}
		if r.Duration > 0 && game.Difficulty.Valid && game.Difficulty.String != "" {
			prev, err := b.st.BestSecsBefore(game.ChatID, r.PlayerID, game.Difficulty.String, game.ID)
			if err != nil {
				log.Printf("celebrations.pb: %v", err)
			} else if prev > 0 && r.Duration < prev {
				lines = append(lines, personalBestLine(r.Name, game.Difficulty.String, r.Duration, prev))
			}
		}
		after, ranks, dates, today, err := b.badgeState(game.ChatID, r.PlayerID, tz)
		if err != nil {
			log.Printf("celebrations.badges: %v", err)
			continue
		}
		// Reconstruct the pre-game badge input by removing this game's contribution.
		before := after
		before.Games--
		if r.Rank == 1 {
			before.Wins--
		}
		if bestB, berr := b.st.BestSecsBefore(game.ChatID, r.PlayerID, "", game.ID); berr == nil {
			before.BestSecs = bestB
		}
		if len(ranks) > 0 {
			before.WinStreak = domain.WinStreak(ranks[1:])
		}
		before.DayStreak = domain.DayStreak(removeOneToday(dates, today), today)

		earned := newBadges(domain.Badges(before), domain.Badges(after))
		if r.Rank == 1 && after.WinStreak >= 3 && !containsBadge(earned, "🔥") {
			lines = append(lines, winStreakLine(r.Name, after.WinStreak))
		}
		if len(earned) > 0 {
			lines = append(lines, badgeLine(r.Name, earned))
		}
		if gamesMilestones[after.Games] {
			lines = append(lines, milestoneGamesLine(r.Name, after.Games))
		}
		if r.Rank == 1 && winsMilestones[after.Wins] {
			lines = append(lines, milestoneWinsLine(r.Name, after.Wins))
		}
	}
	return lines
}

func containsBadge(badges []string, badge string) bool {
	for _, b := range badges {
		if b == badge {
			return true
		}
	}
	return false
}

// seasonAwards builds the end-of-season nomination lines from the final
// standings. Champion is announced separately, so it is skipped here.
func (b *Bot) seasonAwards(chatID, seasonID int64, standings []storage.Standing) []string {
	var out []string
	// Wins award only when a single player holds the top count — a tie would
	// otherwise silently go to standings[0] (the champion). "Самый активный"
	// (games) is dropped: in a fixed group everyone plays the same games, so it
	// never discriminates.
	if name, wins, ok := uniqueTopWins(standings); ok {
		out = append(out, awardWinsLine(name, wins))
	}
	if fastest, err := b.st.FastestInSeason(chatID, seasonID); err != nil {
		log.Printf("seasonAwards.fastest: %v", err)
	} else if fastest != nil {
		out = append(out, awardFastestLine(fastest.Name, fastest.Secs, fastest.Difficulty))
	}
	if name, n, err := b.longestSeasonRun(chatID, seasonID); err != nil {
		log.Printf("seasonAwards.streak: %v", err)
	} else if n >= 2 {
		out = append(out, awardStreakLine(name, n))
	}
	return out
}

// uniqueTopWins returns the sole wins leader when exactly one player holds the
// season's top win count (>0). On a tie it returns ok=false so the award is
// skipped instead of defaulting to standings[0].
func uniqueTopWins(standings []storage.Standing) (name string, wins int, ok bool) {
	for _, s := range standings {
		switch {
		case s.Wins > wins:
			wins, name, ok = s.Wins, s.Name, true
		case s.Wins == wins && wins > 0:
			ok = false
		}
	}
	return name, wins, ok
}

// longestSeasonRun finds the season's longest consecutive-wins run and its holder.
func (b *Bot) longestSeasonRun(chatID, seasonID int64) (string, int, error) {
	rows, err := b.st.SeasonRanks(chatID, seasonID)
	if err != nil {
		return "", 0, err
	}
	ranksBy := map[int64][]int{}
	names := map[int64]string{}
	var order []int64
	for _, r := range rows {
		if _, ok := ranksBy[r.PlayerID]; !ok {
			order = append(order, r.PlayerID)
		}
		ranksBy[r.PlayerID] = append(ranksBy[r.PlayerID], r.Rank)
		names[r.PlayerID] = r.Name
	}
	bestName, best := "", 0
	for _, id := range order {
		if n := domain.LongestWinRun(ranksBy[id]); n > best {
			best, bestName = n, names[id]
		}
	}
	return bestName, best, nil
}

// duelsPanel builds the full duels view: leaderboard (Elo-sorted) with
// head-to-head, duel solve-time, win-streak markers, and the recent log.
func (b *Bot) duelsPanel(chatID int64) (string, error) {
	rows, err := b.st.DuelLeaderboard(chatID)
	if err != nil {
		return "", err
	}
	pairs, err := b.st.DuelPairs(chatID)
	if err != nil {
		return "", err
	}
	dp := make([][2]int64, len(pairs))
	for i, p := range pairs {
		dp[i] = [2]int64{p.WinnerID, p.LoserID}
	}
	ratings := domain.EloRatings(dp)
	streaks := domain.DuelStreaks(dp)
	for i := range rows {
		if r, ok := ratings[rows[i].PlayerID]; ok {
			rows[i].Elo = r
		} else {
			rows[i].Elo = domain.EloStart
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Elo > rows[j].Elo })
	h2h, err := b.st.HeadToHeadAll(chatID)
	if err != nil {
		return "", err
	}
	speed, err := b.st.DuelSpeed(chatID)
	if err != nil {
		return "", err
	}
	recent, err := b.st.RecentDuels(chatID, 8)
	if err != nil {
		return "", err
	}
	return duelsText(rows, h2h, speed, streaks, recent, b.chatTZ(chatID)), nil
}

// duelExtras returns a player's duel solve summary and win-streak, for /me.
func (b *Bot) duelExtras(chatID, playerID int64) (*storage.SpeedStat, domain.Streak, error) {
	sp, err := b.st.DuelSpeedFor(chatID, playerID)
	if err != nil {
		return nil, domain.Streak{}, err
	}
	pairs, err := b.st.DuelPairs(chatID)
	if err != nil {
		return nil, domain.Streak{}, err
	}
	dp := make([][2]int64, len(pairs))
	for i, p := range pairs {
		dp[i] = [2]int64{p.WinnerID, p.LoserID}
	}
	return sp, domain.DuelStreaks(dp)[playerID], nil
}

// duelStakes returns both players' current Elo and what each would gain by
// winning the upcoming duel — the "stakes" shown on a challenge post.
func (b *Bot) duelStakes(chatID, challengerID, targetID int64) (rc, rt, gainC, gainT int, err error) {
	pairs, err := b.st.DuelPairs(chatID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	dp := make([][2]int64, len(pairs))
	for i, p := range pairs {
		dp[i] = [2]int64{p.WinnerID, p.LoserID}
	}
	ratings := domain.EloRatings(dp)
	get := func(id int64) int {
		if v, ok := ratings[id]; ok {
			return v
		}
		return domain.EloStart
	}
	rc, rt = get(challengerID), get(targetID)
	wc, _ := domain.EloUpdate(rc, rt)
	wt, _ := domain.EloUpdate(rt, rc)
	return rc, rt, wc - rc, wt - rt, nil
}

// winnerEloAt replays duel history and returns the winner's rating right after
// the given game plus the change that game caused. ok=false when the game is
// not in the pair history (e.g. the opponent's result row is missing), so the
// value is stable across re-renders even after later duels.
func winnerEloAt(pairs []storage.DuelPair, gameID, winnerID int64) (rating, delta int, ok bool) {
	r := map[int64]int{}
	get := func(id int64) int {
		if v, found := r[id]; found {
			return v
		}
		return domain.EloStart
	}
	for _, p := range pairs {
		w0, l0 := get(p.WinnerID), get(p.LoserID)
		w1, l1 := domain.EloUpdate(w0, l0)
		r[p.WinnerID], r[p.LoserID] = w1, l1
		if p.GameID == gameID {
			if p.WinnerID != winnerID {
				return 0, 0, false
			}
			return w1, w1 - w0, true
		}
	}
	return 0, 0, false
}
