package bot

import (
	"log"
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
		WinStreak:  domain.LongestWinRun(ranks), // best of the career: badges are earned once
		DayStreak:  domain.LongestDayStreak(dates),
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
	pace := b.featureOn(game.ChatID, featPace)
	best := winnerSolve(rows)
	for _, r := range rows {
		if r.Rank < 1 {
			// DNF still counts as a played game, so the career-games
			// milestone applies; badges and streaks do not.
			if _, games, _, err := b.st.CareerStats(game.ChatID, r.PlayerID); err == nil && gamesMilestones[games] {
				lines = append(lines, milestoneGamesLine(r.Name, games))
			}
			continue
		}
		record := false
		if r.Duration > 0 && game.Difficulty.Valid && game.Difficulty.String != "" {
			prev, err := b.st.BestSecsBefore(game.ChatID, r.PlayerID, game.Difficulty.String, game.ID)
			if err != nil {
				log.Printf("celebrations.pb: %v", err)
			} else if prev > 0 && r.Duration < prev {
				lines = append(lines, personalBestLine(r.Name, game.Difficulty.String, r.Duration, prev))
				record = true
			}
		}
		if pace && !record { // a record says it louder: no pace line on top
			if line, ok := b.paceLine(game, r, best); ok {
				lines = append(lines, line)
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
			before.WinStreak = domain.LongestWinRun(ranks[1:])
		}
		before.DayStreak = domain.LongestDayStreak(removeOneToday(dates, today))

		earned := newBadges(domain.Badges(before), domain.Badges(after))
		if cur := domain.WinStreak(ranks); r.Rank == 1 && cur >= 3 && !containsBadge(earned, "🔥") {
			lines = append(lines, winStreakLine(r.Name, cur))
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

// duelsPanel builds the full duels view: leaderboard (by wins) with
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
	streaks := domain.DuelStreaks(dp)
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
	challenges, err := b.st.ChallengeStats(chatID)
	if err != nil {
		return "", err
	}
	return duelsText(rows, h2h, speed, streaks, recent, challenges, b.chatTZ(chatID)), nil
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

// duelStakes returns both players' current rating on the eternal ladder and
// what each would gain by winning the upcoming duel: the stakes shown on a
// challenge post. Same ratings as the result footer, no separate duel rating.
func (b *Bot) duelStakes(chatID, challengerID, targetID int64) (rc, rt, gainC, gainT int, err error) {
	games, err := b.st.GamesForRating(chatID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	rc, rt, gainC, gainT = domain.DuelStakes(games, challengerID, targetID)
	return rc, rt, gainC, gainT, nil
}

// duelUpsetGain returns the rating points an underdog winner took from a duel
// (0 when the winner was the favourite or the game has no rating impact).
func (b *Bot) duelUpsetGain(game *storage.Game, winnerID, loserID int64) int {
	games, err := b.st.GamesForRating(game.ChatID)
	if err != nil {
		log.Printf("duelUpsetGain: %v", err)
		return 0
	}
	for _, gr := range domain.ComputeRatings(games).PerGame {
		if gr.GameID == game.ID {
			gain, _ := domain.UpsetGain(gr, winnerID, loserID)
			return gain
		}
	}
	return 0
}

// activePlayerSet returns the chat's active player ids for the rating's crown
// and ladder. nil (everyone counts) on a storage error, so a hiccup never
// blanks the ladder.
func (b *Bot) activePlayerSet(chatID int64) map[int64]bool {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		log.Printf("activePlayerSet: %v", err)
		return nil
	}
	set := make(map[int64]bool, len(players))
	for _, p := range players {
		set[p.ID] = true
	}
	return set
}
