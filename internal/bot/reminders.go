package bot

import (
	"log"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// stalePendingMinutes is how long a pending game waits before the bot nudges.
const stalePendingMinutes = 45

// openGameGraceMinutes is how long past the deadline a season waits for a game
// that was started before it (3 hours covers a long hardcore session).
const openGameGraceMinutes = 180

// runReminders ticks once a minute, sending event-driven and daily nudges.
func (b *Bot) runReminders() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	steps := []struct {
		name string
		run  func()
	}{
		{"remindStalePending", b.remindStalePending},
		{"remindDaily", b.remindDaily},
		{"remindWeekly", b.remindWeekly},
		{"closeExpiredSeasons", b.closeExpiredSeasons},
		{"expireStaleDuels", b.expireStaleDuels},
		{"remindMeets", b.remindMeets},
	}
	for range ticker.C {
		for _, st := range steps {
			runStep(st.name, st.run)
		}
	}
}

// runStep runs one ticker step and survives its panic: the panic is logged and
// the next step (and the next tick) still run.
func runStep(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("reminders %s panic: %v", name, r)
		}
	}()
	fn()
}

// remindStalePending nudges about pending games with no result yet. A game with
// a usdoku room whose watcher is still running is left alone: the bot will
// record it by itself. The nudge edits the game's card; a new message (with a
// ping to the creator) goes out only when the game has no card.
func (b *Bot) remindStalePending() {
	stale, err := b.st.StalePendingGames(stalePendingMinutes)
	if err != nil {
		log.Printf("remindStalePending: %v", err)
		return
	}
	for _, sp := range stale {
		if sp.Code != "" && isWatching(sp.GameID) {
			continue
		}
		if !b.nudgeStale(sp) {
			continue
		}
		if err := b.st.MarkReminded(sp.GameID); err != nil {
			log.Printf("remindStalePending mark: %v", err)
		}
	}
}

// dailyNudgeText is the evening nudge when nothing was played today.
const dailyNudgeText = "🌙 Сегодня ещё без судоку. Партию? /play"

// localDayBounds returns the UTC instants [from, to) of the chat-local calendar
// day containing now.
func localDayBounds(now time.Time, loc *time.Location) (from, to time.Time) {
	n := now.In(loc)
	from = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	to = time.Date(n.Year(), n.Month(), n.Day()+1, 0, 0, 0, 0, loc)
	return from.UTC(), to.UTC()
}

// dailyNudge returns the evening nudge for a chat, or "" when it should stay
// quiet: no active players, a game is already waiting for its result, or
// something was already played today (chat-local day, duels included).
func (b *Bot) dailyNudge(chatID int64, now time.Time, loc *time.Location) (string, error) {
	players, err := b.st.ListPlayers(chatID)
	if err != nil || len(players) == 0 {
		return "", err
	}
	pending, err := b.st.ActivePendingGame(chatID)
	if err != nil || pending != nil {
		return "", err
	}
	from, to := localDayBounds(now, loc)
	cnt, err := b.st.CompletedBetween(chatID, fmtDBTime(from), fmtDBTime(to))
	if err != nil || cnt > 0 {
		return "", err
	}
	return dailyNudgeText, nil
}

// nudgeStale shows the «запишем?» prompt for one game: on its card when it has
// one, else as a new message pinging the creator. False when nothing was shown.
func (b *Bot) nudgeStale(sp storage.StalePending) bool {
	kb := recordKeyboard(sp.GameID)
	if sp.PostMsgID.Valid && b.tryCardNote(sp.GameID, staleNudgeText(sp.Code, ""), kb) {
		return true
	}
	ping := ""
	if sp.CreatedBy.Valid {
		if p, err := b.st.PlayerByTg(sp.ChatID, sp.CreatedBy.Int64); err == nil && p != nil {
			ping = mention(*p)
		}
	}
	if _, err := b.tb.Send(tele.ChatID(sp.ChatID), staleNudgeText(sp.Code, ping), kb); err != nil {
		log.Printf("remindStalePending send: %v", err)
		return false
	}
	return true
}

// remindDaily sends the daily nudge if nothing was played today.
func (b *Bot) remindDaily() { b.remindDailyAt(time.Now()) }

// normalizeHHMM parses a clock time such as "9:00" or "21:00" and returns it
// zero-padded as HH:MM, the form the daily time is stored and compared in.
func normalizeHHMM(s string) (string, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return t.Format("15:04"), true
}

// dailyDue reports whether the chat-local clock (HH:MM) has reached the chat's
// daily time. The stored value is normalised first, so a legacy "9:00" is read
// as 09:00 and not compared as a string against "21:00". An unreadable stored
// value falls back to the default evening time.
func dailyDue(nowHHMM, dailyTime string) bool {
	at, ok := normalizeHHMM(dailyTime)
	if !ok {
		at = "21:00"
	}
	return nowHHMM >= at
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
	if len(standings) == 0 {
		return "", false, nil // no active players: nobody to address
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
	text = digestText(season, top, fastest, streakName, streakLen, weekGames) +
		digestFinalLine(season, standings, time.Now(), loadLoc(b.chatTZ(chatID)))
	return text, true, nil
}

// weeklyDigestTime is the chat-local time of day the Monday digest goes out. It
// is fixed (not daily_time) so the digest never lands in the same minute as the
// evening nudge.
const weeklyDigestTime = "10:00"

// remindWeekly posts a Monday digest at weeklyDigestTime (chat tz), at most
// once per day, skipped when no games were played in the trailing week.
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
		if now.Weekday() != time.Monday || now.Format("15:04") < weeklyDigestTime || ch.LastWeekly == date {
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
		b.postChat(ch.ChatID, text)
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
// opened by CloseSeason). A season that hasn't been played enough (fewer than
// MinGames games) is extended a month instead of crowning a champion out of a
// half-played season; the shortfall is announced so the chat can close it.
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

// seasonNudgeTime is the local time of day the "last week" and "final day"
// posts go out (not before it, so nobody is pinged at midnight).
const seasonNudgeTime = "10:30"

// seasonNudgeKind decides which one-off season post is due: "week" during the
// last seven calendar days, "final" on the last playable day, "" otherwise.
// deadline and last form the once-per-deadline marker (see nudgeMarker): a
// post that already went out for this deadline is never repeated, and an
// extended season (new deadline) gets a fresh pair of posts.
func seasonNudgeKind(daysLeft int, hhmm, deadline, last string) string {
	if hhmm < seasonNudgeTime {
		return ""
	}
	kind := ""
	switch {
	case daysLeft == 0:
		kind = "final"
	case daysLeft >= 1 && daysLeft <= 6:
		kind = "week"
	default:
		return ""
	}
	if last == nudgeMarker(kind, deadline) || last == nudgeMarker("final", deadline) {
		return ""
	}
	return kind
}

// nudgeMarker is the value stored in seasons.last_nudge for a sent post.
func nudgeMarker(kind, deadline string) string { return kind + "@" + deadline }

// postChat sends a plain-text post to a chat, logging failures; false when
// the send failed. A nil bot client (unit tests) sends nothing and reports
// success, so callers' bookkeeping still runs under test.
func (b *Bot) postChat(chatID int64, text string) bool {
	if b.tb == nil {
		return true
	}
	if _, err := b.tb.Send(tele.ChatID(chatID), text); err != nil {
		log.Printf("postChat %d: %v", chatID, err)
		return false
	}
	return true
}

// nudgeSeason sends the one-off "last week" / "final day" posts for an active
// season whose deadline is still ahead. Chats without active players or
// without any points in the season stay silent. The final-day post only goes
// out once the games floor is met: short of it the deadline extends the season
// instead, so there is no final to announce (it is not marked either, so it
// still fires if the floor is reached later that day).
func (b *Bot) nudgeSeason(season *storage.Season, deadline, now time.Time, loc *time.Location) error {
	last, err := b.st.SeasonNudge(season.ID)
	if err != nil {
		return err
	}
	kind := seasonNudgeKind(domain.SeasonDaysLeft(deadline, now, loc),
		now.In(loc).Format("15:04"), season.Deadline.String, last)
	if kind == "" {
		return nil
	}
	games, err := b.st.SeasonGameCount(season.ChatID, season.ID)
	if err != nil {
		return err
	}
	if kind == "final" && games < season.MinGames {
		return nil
	}
	// Mark first so the post goes out at most once, even if sending fails.
	if err := b.st.SetSeasonNudge(season.ID, nudgeMarker(kind, season.Deadline.String)); err != nil {
		return err
	}
	standings, err := b.st.Standings(season.ChatID, season.ID)
	if err != nil {
		return err
	}
	if len(standings) == 0 || standings[0].Points == 0 {
		return nil
	}
	if kind == "final" {
		b.postChat(season.ChatID, seasonFinalDayText(season.Number, standings))
	} else {
		b.postChat(season.ChatID, seasonLastWeekText(season.Number, standings, games, season.MinGames))
	}
	return nil
}

// closeExpiredSeason handles one chat; separated so a failure in one chat
// doesn't skip the others.
func (b *Bot) closeExpiredSeason(ch storage.ChatSettings) error {
	return b.closeExpiredSeasonAt(ch, time.Now())
}

// closeExpiredSeasonAt is closeExpiredSeason with an explicit clock.
func (b *Bot) closeExpiredSeasonAt(ch storage.ChatSettings, now time.Time) error {
	season, err := b.st.ActiveSeason(ch.ChatID)
	if err != nil {
		return err
	}
	loc := loadLoc(ch.TZ)

	if !season.Deadline.Valid || season.Deadline.String == "" {
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(domain.SeasonDeadline(now, loc)))
	}
	deadline, err := parseDBTime(season.Deadline.String)
	if err != nil { // unreadable value: rewrite it rather than retry every minute
		return b.st.SetSeasonDeadline(season.ID, fmtDBTime(domain.SeasonDeadline(now, loc)))
	}
	if now.UTC().Before(deadline) {
		return b.nudgeSeason(season, deadline, now, loc)
	}

	standings, err := b.st.Standings(ch.ChatID, season.ID)
	if err != nil {
		return err
	}
	games, err := b.st.SeasonGameCount(ch.ChatID, season.ID)
	if err != nil {
		return err
	}
	if len(standings) == 0 || games < season.MinGames {
		// Not played enough to crown anyone: roll the deadline forward a month
		// and say so out loud. Silence is what let season 5 drift to a hollow
		// finish; a visible counter is the only pressure the bot can apply.
		ext := domain.ExtendSeasonDeadline(deadline, now.UTC(), loc)
		if err := b.st.SetSeasonDeadline(season.ID, fmtDBTime(ext)); err != nil {
			return err
		}
		log.Printf("📅 chat %d: season %d under-played (%d/%d games) extended to %s",
			ch.ChatID, season.Number, games, season.MinGames, ext)
		if games == 0 || len(standings) == 0 {
			return nil // nothing played or no active players: extend silently, don't nag a dead chat
		}
		b.postChat(ch.ChatID, seasonExtendedText(season, games, &standings[0], ext, now, loc))
		return nil
	}

	// The floor is met and the season will close. A game started before the
	// deadline would be recorded into the successor (or lost), so hold the
	// close until it is recorded, cancelled, or too old to still be played.
	open, err := b.st.HasOpenSeasonGame(season.ID, season.Deadline.String, openGameGraceMinutes)
	if err != nil {
		return err
	}
	if open {
		return nil
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
	b.postChat(ch.ChatID,
		seasonDeadlineEndText(season.Number, standings[0].Name, standings[0].Points,
			awards, newSeason.Number, newSeason.Target, nextDeadline, loc))
	b.postSeasonPoster(ch.ChatID, season.ID)
	return nil
}
