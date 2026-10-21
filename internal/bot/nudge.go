package bot

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featNudge is the smart daily nudge: it comes at the hour this chat usually
// starts playing on that weekday, skips weekdays the chat hardly plays, goes
// quiet after being ignored twice in a row, and words itself from the season.
// Off: the classic nudge at the fixed daily time.
const featNudge = "nudge"

// evNudgeSent logs a sent daily nudge; detail is "smart" or "fixed".
const evNudgeSent = "nudge_sent"

const (
	nudgeLookbackDays = 42  // history the usual time is read from
	nudgeMinDays      = 5   // fewer days with a game: keep the configured time
	nudgeMinWeekday   = 3   // fewer such days on a weekday: not this chat's habit
	nudgeActiveDays   = 15  // an active chat skips its quiet weekdays; a sparser one is not left alone
	nudgeStaleMinutes = 180 // later than this past the time (bot was down): skip
	nudgePauseDays    = 2   // quiet days after two ignored nudges in a row
	// nudgeDayStart: a game before 05:00 belongs to the previous day's session,
	// so a Friday that runs past midnight does not look like a Saturday at 00:20.
	nudgeDayStart = 5 * time.Hour
)

// nudgeTexts rotate by day, so the nudge does not read the same every time.
var nudgeTexts = []string{
	"☕ Обычно в это время уже играете. Партию? /play",
	"🧩 Свежая доска ждёт. Кто первый? /play",
	"⏱ Пять минут на судоку? /play",
}

// firstPerDay turns game start times (DB UTC) into the first start of each
// chat-local playing day (which runs to 05:00 the next morning), oldest first.
func firstPerDay(starts []string, loc *time.Location) []time.Time {
	first := map[string]time.Time{}
	for _, s := range starts {
		t, err := parseDBTime(s)
		if err != nil {
			continue
		}
		lt := t.In(loc)
		day := lt.Add(-nudgeDayStart).Format("2006-01-02")
		if cur, ok := first[day]; !ok || lt.Before(cur) {
			first[day] = lt
		}
	}
	out := make([]time.Time, 0, len(first))
	for _, t := range first {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// nudgeClock picks today's nudge time (HH:MM local) from the first game of
// each playing day: the median on this weekday, rounded up to five minutes
// (an after-midnight start counts as late on the day before, capped at 23:55).
// With too little history it keeps fallback. A weekday played on fewer than
// nudgeMinWeekday days is skipped (ok=false) in an active chat, and gets the
// median over all days in a sparser one: a chat drifting away is the one
// that still needs the nudge.
func nudgeClock(first []time.Time, wd time.Weekday, fallback string) (string, bool) {
	if len(first) < nudgeMinDays {
		return fallback, true
	}
	var all, day []int
	for _, t := range first {
		m := t.Hour()*60 + t.Minute()
		if time.Duration(t.Hour())*time.Hour < nudgeDayStart {
			m += 24 * 60
		}
		all = append(all, m)
		if t.Add(-nudgeDayStart).Weekday() == wd {
			day = append(day, m)
		}
	}
	mins := day
	if len(day) < nudgeMinWeekday {
		if len(first) >= nudgeActiveDays {
			return "", false
		}
		mins = all
	}
	sort.Ints(mins)
	m := mins[(len(mins)-1)/2] // the lower middle: a time somebody really played
	m = (m + 4) / 5 * 5
	if m > 23*60+55 {
		m = 23*60 + 55
	}
	return minutesClock(m), true
}

// minutesClock renders minutes since midnight as HH:MM.
func minutesClock(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

// avoidDigest moves a Monday nudge that would land within a quarter of an
// hour of the weekly digest to just after it.
func avoidDigest(at string, wd time.Weekday) string {
	if wd != time.Monday {
		return at
	}
	digest := clockMinutes(weeklyDigestTime)
	if d := clockMinutes(at) - digest; d >= -15 && d < 15 {
		return minutesClock(digest + 15)
	}
	return at
}

// nudgeDue compares the local clock with the nudge time: due once it is
// reached, stale when more than nudgeStaleMinutes have passed since.
func nudgeDue(nowHHMM, at string) (due, stale bool) {
	if !dailyDue(nowHHMM, at) {
		return false, false
	}
	now, _ := normalizeHHMM(nowHHMM)
	when, ok := normalizeHHMM(at)
	if !ok {
		when = "21:00"
	}
	return true, clockMinutes(now)-clockMinutes(when) > nudgeStaleMinutes
}

func clockMinutes(hhmm string) int {
	t, _ := time.Parse("15:04", hhmm)
	return t.Hour()*60 + t.Minute()
}

// nudgeRecord is one past nudge and whether a game followed it that day.
type nudgeRecord struct {
	At     time.Time
	Played bool
}

// nudgeSilenced: the two latest nudges (newest first) were both ignored, and
// the latest is at most nudgePauseDays local days old. Being ignored is the
// one signal the bot gets that it is becoming noise, short of a mute.
func nudgeSilenced(recs []nudgeRecord, now time.Time, loc *time.Location) bool {
	if len(recs) < 2 || recs[0].Played || recs[1].Played {
		return false
	}
	day := func(t time.Time) time.Time {
		l := t.In(loc)
		return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
	}
	days := int(day(now).Sub(day(recs[0].At)) / (24 * time.Hour))
	return days <= nudgePauseDays
}

// smartNudgeText leads with a live season stake when there is one, else
// rotates the plain invitations by day.
func smartNudgeText(stake string, now time.Time) string {
	if stake != "" {
		return stake + "\nПартию? /play"
	}
	return nudgeTexts[now.YearDay()%len(nudgeTexts)]
}

// smartNudgeTime reads today's nudge time from the chat's history: whole
// days before today, so the answer stays the same all day long.
func (b *Bot) smartNudgeTime(chatID int64, now time.Time, loc *time.Location, fallback string) (string, bool) {
	today, _ := localDayBounds(now, loc)
	starts, err := b.st.GameStartsSince(chatID, fmtDBTime(today.AddDate(0, 0, -nudgeLookbackDays)))
	if err != nil {
		log.Printf("smartNudgeTime: %v", err)
		return fallback, true
	}
	var first []time.Time
	for _, t := range firstPerDay(starts, loc) {
		if t.Before(today) {
			first = append(first, t)
		}
	}
	wd := now.In(loc).Weekday()
	at, ok := nudgeClock(first, wd, fallback)
	return avoidDigest(at, wd), ok
}

// nudgeRecords loads the two latest smart nudges (a classic one from before
// the switch says nothing about this one) and whether anyone played later
// that playing day, which runs to 05:00 the next morning.
func (b *Bot) nudgeRecords(chatID int64, loc *time.Location) []nudgeRecord {
	evs, err := b.st.RecentEvents(chatID, evNudgeSent, 10)
	if err != nil {
		log.Printf("nudgeRecords: %v", err)
		return nil
	}
	recs := make([]nudgeRecord, 0, 2)
	for _, e := range evs {
		if e.Detail != "smart" || len(recs) == 2 {
			continue
		}
		t, err := parseDBTime(e.At)
		if err != nil {
			continue
		}
		_, end := localDayBounds(t, loc)
		n, err := b.st.CompletedBetween(chatID, e.At, fmtDBTime(end.Add(nudgeDayStart)))
		if err != nil {
			log.Printf("nudgeRecords.played: %v", err)
			return nil
		}
		recs = append(recs, nudgeRecord{At: t, Played: n > 0})
	}
	return recs
}

// nudgeStake is the season stake for the nudge's first line, "" when none.
func (b *Bot) nudgeStake(chatID int64) string {
	season, err := b.st.ActiveSeason(chatID)
	if err != nil {
		return ""
	}
	standings, err := b.st.Standings(chatID, season.ID)
	if err != nil {
		return ""
	}
	return stakeLine(standings, season.PointsTable, season.Target)
}

// markDaily records that today's nudge is handled (sent or skipped).
func (b *Bot) markDaily(chatID int64, date string) {
	if err := b.st.SetLastDaily(chatID, date); err != nil {
		log.Printf("remindDaily mark: %v", err)
	}
}

// remindDailyAt is remindDaily with an explicit clock.
func (b *Bot) remindDailyAt(clock time.Time) {
	chats, err := b.st.AllChats()
	if err != nil {
		log.Printf("remindDaily: %v", err)
		return
	}
	for _, ch := range chats {
		if !ch.DailyReminder {
			continue
		}
		loc := loadLoc(ch.TZ)
		now := clock.In(loc)
		date := now.Format("2006-01-02")
		if ch.LastDaily == date {
			continue
		}
		smart := b.featureOn(ch.ChatID, featNudge)
		at := ch.DailyTime
		if smart {
			var ok bool
			if at, ok = b.smartNudgeTime(ch.ChatID, now, loc, ch.DailyTime); !ok {
				b.markDaily(ch.ChatID, date) // a weekday this chat hardly plays
				continue
			}
		}
		due, stale := nudgeDue(now.Format("15:04"), at)
		if !due {
			continue
		}
		// Mark handled for today regardless, so we nudge at most once.
		b.markDaily(ch.ChatID, date)
		if smart && (stale || nudgeSilenced(b.nudgeRecords(ch.ChatID, loc), now, loc)) {
			continue
		}
		text, err := b.dailyNudge(ch.ChatID, now, loc)
		if err != nil {
			log.Printf("remindDaily: %v", err)
			continue
		}
		if text == "" {
			continue
		}
		detail := "fixed"
		if smart {
			text, detail = smartNudgeText(b.nudgeStake(ch.ChatID), now), "smart"
		}
		if b.postChat(ch.ChatID, text) {
			b.logEvent(storage.Event{ChatID: ch.ChatID, Kind: evNudgeSent, Detail: detail, At: fmtDBTime(now)})
		}
	}
}
