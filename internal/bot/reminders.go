package bot

import (
	"log"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// stalePendingMinutes is how long a pending game waits before the bot nudges.
const stalePendingMinutes = 45

// minSeasonPointsToClose is the leader's point floor for a calendar-deadline
// close. Below it, the month is treated as too quiet to crown anyone.
const minSeasonPointsToClose = 10

// runReminders ticks once a minute, sending event-driven and daily nudges.
func (b *Bot) runReminders() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		b.remindStalePending()
		b.remindDaily()
		b.remindWeekly()
		b.closeExpiredSeasons()
	}
}

// remindStalePending nudges about pending games with no result yet.
func (b *Bot) remindStalePending() {
	stale, err := b.st.StalePendingGames(stalePendingMinutes)
	if err != nil {
		log.Printf("remindStalePending: %v", err)
		return
	}
	for _, sp := range stale {
		_, err := b.tb.Send(tele.ChatID(sp.ChatID),
			"⏰ Есть незаписанная игра. Кто победил?",
			recordKeyboard(sp.GameID))
		if err != nil {
			log.Printf("remindStalePending send: %v", err)
			continue
		}
		if err := b.st.MarkReminded(sp.GameID); err != nil {
			log.Printf("remindStalePending mark: %v", err)
		}
	}
}

// remindDaily sends the catch-all evening nudge if nothing was recorded today.
func (b *Bot) remindDaily() {
	chats, err := b.st.AllChats()
	if err != nil {
		log.Printf("remindDaily: %v", err)
		return
	}
	for _, ch := range chats {
		if !ch.DailyReminder {
			continue
		}
		loc, err := time.LoadLocation(ch.TZ)
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		date := now.Format("2006-01-02")
		if ch.LastDaily == date || now.Format("15:04") < ch.DailyTime {
			continue
		}

		// Mark handled for today regardless, so we nudge at most once.
		if err := b.st.SetLastDaily(ch.ChatID, date); err != nil {
			log.Printf("remindDaily mark: %v", err)
		}
		cnt, err := b.st.CompletedToday(ch.ChatID, date)
		if err != nil {
			log.Printf("remindDaily count: %v", err)
			continue
		}
		if cnt > 0 {
			continue // already played & recorded today, no need to nag
		}
		if _, err := b.tb.Send(tele.ChatID(ch.ChatID),
			"🌙 Сегодня играли в судоку? Не забудьте записать результат: /result"); err != nil {
			log.Printf("remindDaily send: %v", err)
		}
	}
}

// buildWeeklyDigest assembles the trailing-7-day digest for a chat. hasGames is
// false when no games were played in the window (caller decides what to show).
func (b *Bot) buildWeeklyDigest(chatID int64) (text string, hasGames bool, err error) {
	sinceUTC := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	weekGames, err := b.st.GamesSince(chatID, sinceUTC)
	if err != nil {
		return "", false, err
	}
	if weekGames == 0 {
		return "", false, nil
	}
	season, err := b.st.ActiveSeason(chatID)
	if err != nil {
		return "", false, err
	}
	standings, err := b.st.Standings(chatID, season.ID)
	if err != nil {
		return "", false, err
	}
	top := standings
	if len(top) > 3 {
		top = top[:3]
	}
	fastest, err := b.st.FastestSince(chatID, sinceUTC)
	if err != nil {
		log.Printf("buildWeeklyDigest fastest: %v", err)
	}
	streakName, streakLen := b.longestWinStreak(chatID)
	return digestText(season, top, fastest, streakName, streakLen, weekGames), true, nil
}

// remindWeekly posts a Monday digest at the chat's daily_time (chat tz),
// at most once per day, skipped when no games were played in the trailing week.
func (b *Bot) remindWeekly() {
	chats, err := b.st.AllChats()
	if err != nil {
		log.Printf("remindWeekly: %v", err)
		return
	}
	for _, ch := range chats {
		if !ch.WeeklyDigest {
			continue
		}
		loc := loadLoc(ch.TZ)
		now := time.Now().In(loc)
		date := now.Format("2006-01-02")
		if now.Weekday() != time.Monday || now.Format("15:04") < ch.DailyTime || ch.LastWeekly == date {
			continue
		}
		// Mark handled regardless, so we post at most once.
		if err := b.st.SetLastWeekly(ch.ChatID, date); err != nil {
			log.Printf("remindWeekly mark: %v", err)
		}
		text, hasGames, err := b.buildWeeklyDigest(ch.ChatID)
		if err != nil {
			log.Printf("remindWeekly build: %v", err)
			continue
		}
		if !hasGames {
			continue // quiet week, skip the digest
		}
		if _, err := b.tb.Send(tele.ChatID(ch.ChatID), text); err != nil {
			log.Printf("remindWeekly send: %v", err)
		}
	}
}

// longestWinStreak finds the active player with the longest current win streak.
func (b *Bot) longestWinStreak(chatID int64) (name string, length int) {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		log.Printf("longestWinStreak: %v", err)
		return "", 0
	}
	for _, p := range players {
		ranks, err := b.st.RecentRanks(chatID, p.ID)
		if err != nil {
			continue
		}
		if ws := domain.WinStreak(ranks); ws > length {
			length, name = ws, p.Name
		}
	}
	return name, length
}

// closeExpiredSeasons ends seasons whose calendar deadline has passed, and
// assigns a deadline to seasons that have none yet (older rows, and the season
// opened by CloseSeason). A season whose month produced no points is extended
// silently instead of crowning a champion with nothing — that also keeps the
// bot quiet in chats that no longer play.
func (b *Bot) closeExpiredSeasons() {
	chats, err := b.st.AllChats()
	if err != nil {
		log.Printf("closeExpiredSeasons: %v", err)
		return
	}
	for _, ch := range chats {
		if err := b.closeExpiredSeason(ch); err != nil {
			log.Printf("closeExpiredSeasons %d: %v", ch.ChatID, err)
		}
	}
}

// closeExpiredSeason handles one chat; separated so a failure in one chat
// doesn't skip the others.
func (b *Bot) closeExpiredSeason(ch storage.ChatSettings) error {
	season, err := b.st.ActiveSeason(ch.ChatID)
	if err != nil {
		return err
	}
	loc := loadLoc(ch.TZ)
	now := time.Now()

	if !season.Deadline.Valid || season.Deadline.String == "" {
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(domain.SeasonDeadline(now, loc)))
	}
	deadline, err := parseDBTime(season.Deadline.String)
	if err != nil { // unreadable value: rewrite it rather than retry every minute
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(domain.SeasonDeadline(now, loc)))
	}
	if now.UTC().Before(deadline) {
		return nil
	}

	standings, err := b.st.Standings(ch.ChatID, season.ID)
	if err != nil {
		return err
	}
	if len(standings) == 0 || standings[0].Points < minSeasonPointsToClose {
		// Too quiet to crown anyone: roll the deadline forward instead. Historical
		// seasons in this chat ran 65-84 games and hit the 100-point target on
		// their own in 15-30 days, so a leader still under the threshold at
		// deadline means the month was effectively dead, not just slow.
		ext := domain.ExtendSeasonDeadline(deadline, now.UTC(), loc)
		log.Printf("📅 chat %d: quiet season %d (leader %d pts) extended to %s",
			ch.ChatID, season.Number, standings[0].Points, ext)
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(ext))
	}

	awards := b.seasonAwards(ch.ChatID, season.ID, standings)
	newSeason, err := b.st.CloseSeason(season, standings[0].PlayerID)
	if err != nil {
		return err
	}
	nextDeadline := domain.SeasonDeadline(now, loc)
	if err := b.st.SetSeasonDeadline(newSeason.ID, fmtDBTime(nextDeadline)); err != nil {
		log.Printf("closeExpiredSeason.setdeadline: %v", err) // next tick fills it in
	}
	log.Printf("🏁 season %d closed by deadline, winner=%s (%d pts) → season %d",
		season.Number, standings[0].Name, standings[0].Points, newSeason.Number)
	_, err = b.tb.Send(tele.ChatID(ch.ChatID),
		seasonDeadlineEndText(season.Number, standings[0].Name, standings[0].Points,
			awards, newSeason.Number, newSeason.Target, nextDeadline, loc))
	return err
}
