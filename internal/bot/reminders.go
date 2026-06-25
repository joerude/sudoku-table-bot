package bot

import (
	"log"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// stalePendingMinutes is how long a pending game waits before the bot nudges.
const stalePendingMinutes = 45

// runReminders ticks once a minute, sending event-driven and daily nudges.
func (b *Bot) runReminders() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		b.remindStalePending()
		b.remindDaily()
		b.remindWeekly()
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
		sinceUTC := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
		weekGames, err := b.st.GamesSince(ch.ChatID, sinceUTC)
		if err != nil {
			log.Printf("remindWeekly games: %v", err)
			continue
		}
		if weekGames == 0 {
			continue // quiet week, skip the digest
		}
		season, err := b.st.ActiveSeason(ch.ChatID)
		if err != nil {
			log.Printf("remindWeekly season: %v", err)
			continue
		}
		standings, err := b.st.Standings(ch.ChatID, season.ID)
		if err != nil {
			log.Printf("remindWeekly standings: %v", err)
			continue
		}
		top := standings
		if len(top) > 3 {
			top = top[:3]
		}
		fastest, err := b.st.FastestSince(ch.ChatID, sinceUTC)
		if err != nil {
			log.Printf("remindWeekly fastest: %v", err)
		}
		streakName, streakLen := b.longestWinStreak(ch.ChatID)
		if _, err := b.tb.Send(tele.ChatID(ch.ChatID),
			digestText(season, top, fastest, streakName, streakLen, weekGames)); err != nil {
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
