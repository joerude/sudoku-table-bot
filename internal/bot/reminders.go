package bot

import (
	"log"
	"time"

	tele "gopkg.in/telebot.v3"
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
