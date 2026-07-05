package bot

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// meExtra computes a player's streak lines + badge row (best-effort; returns ""
// on any storage error so the core /me view still renders).
func (b *Bot) meExtra(chatID, playerID int64, tz string) string {
	in, _, _, _, err := b.badgeState(chatID, playerID, tz)
	if err != nil {
		log.Printf("meExtra: %v", err)
		return ""
	}
	return streakBadgeText(in.WinStreak, in.DayStreak, in.SeasonsWon, domain.Badges(in))
}

// chatTZ returns the chat's timezone string, defaulting to UTC on error.
func (b *Bot) chatTZ(chatID int64) string {
	if ch, err := b.st.GetChat(chatID); err == nil && ch.TZ != "" {
		return ch.TZ
	}
	return "UTC"
}

// onExport sends the current season's games + points as a CSV document,
// mirroring the old Google Sheet (a points column per player).
func (b *Bot) onExport(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onExport.ensure", err)
	}
	chatID := c.Chat().ID
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		return b.fail(c, "onExport.players", err)
	}
	games, err := b.st.ExportGames(chatID, season.ID)
	if err != nil {
		return b.fail(c, "onExport.games", err)
	}
	if len(games) == 0 {
		return b.ephemeral(c, "В этом сезоне ещё нет сыгранных игр для экспорта.")
	}

	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM so Excel reads Cyrillic correctly
	w := csv.NewWriter(&buf)

	header := []string{"date", "difficulty", "mode", "code"}
	for _, p := range players {
		header = append(header, p.Name)
	}
	_ = w.Write(header)

	totals := make([]int, len(players))
	for _, g := range games {
		row := []string{g.Date, g.Difficulty, g.Mode, g.Code}
		for i, p := range players {
			pts := g.Points[p.ID]
			totals[i] += pts
			row = append(row, strconv.Itoa(pts))
		}
		_ = w.Write(row)
	}
	total := []string{"ИТОГО", "", "", ""}
	for _, t := range totals {
		total = append(total, strconv.Itoa(t))
	}
	_ = w.Write(total)

	w.Flush()
	if err := w.Error(); err != nil {
		return b.fail(c, "onExport.csv", err)
	}

	doc := &tele.Document{
		File:     tele.FromReader(bytes.NewReader(buf.Bytes())),
		FileName: fmt.Sprintf("sudoku-season-%d.csv", season.Number),
		Caption:  fmt.Sprintf("📊 Экспорт сезона %d — игр: %d", season.Number, len(games)),
	}
	return c.Send(doc)
}

func (b *Bot) onStatus(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onStatus.ensure", err)
	}
	standings, err := b.st.Standings(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onStatus.standings", err)
	}
	return c.Send(standingsText(season, standings))
}

// onSeason without args shows the active season; "/season N" shows any
// season's summary, including archived ones.
func (b *Bot) onSeason(c tele.Context) error {
	if n, err := strconv.Atoi(argAt(c.Args(), 0)); err == nil && n > 0 {
		return b.seasonSummary(c, n)
	}
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSeason.ensure", err)
	}
	leader, err := b.st.Leader(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onSeason.leader", err)
	}
	text := seasonText(season, leader)
	if nums, err := b.st.ArchivedNumbers(c.Chat().ID); err == nil {
		text += archiveHint(nums)
	}
	return c.Send(text)
}

// seasonSummary renders one season (by display number) with its final table,
// dates, winner, and nominations.
func (b *Bot) seasonSummary(c tele.Context, number int) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "seasonSummary.ensure", err)
	}
	chatID := c.Chat().ID
	se, err := b.st.SeasonByNumber(chatID, number)
	if err != nil {
		return b.fail(c, "seasonSummary.season", err)
	}
	if se == nil {
		nums, _ := b.st.ArchivedNumbers(chatID)
		return b.ephemeral(c, noSuchSeasonText(number, nums))
	}
	standings, err := b.st.Standings(chatID, se.ID)
	if err != nil {
		return b.fail(c, "seasonSummary.standings", err)
	}
	games, first, last, winner, err := b.st.SeasonMeta(chatID, se.ID)
	if err != nil {
		return b.fail(c, "seasonSummary.meta", err)
	}
	awards := b.seasonAwards(chatID, se.ID, standings)
	return c.Send(seasonSummaryText(se, standings, games, first, last, winner, awards))
}

func (b *Bot) onWeekly(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onWeekly.ensure", err)
	}
	text, hasGames, err := b.buildWeeklyDigest(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onWeekly.build", err)
	}
	if !hasGames {
		return b.ephemeral(c, "📅 За последнюю неделю ещё нет сыгранных игр.")
	}
	return c.Send(text)
}

func (b *Bot) onMe(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onMe.ensure", err)
	}
	if c.Sender() == nil {
		return b.ephemeral(c, "Не удалось определить пользователя.")
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, c.Sender().ID)
	if err != nil {
		return b.fail(c, "onMe.player", err)
	}
	if player == nil {
		return b.ephemeral(c, "Ты ещё не в игре. Жми /join")
	}
	stat, err := b.st.StatFor(c.Chat().ID, season.ID, player.ID)
	if err != nil {
		return b.fail(c, "onMe.stat", err)
	}
	sp, err := b.st.SpeedFor(c.Chat().ID, season.ID, player.ID, "medium")
	if err != nil {
		return b.fail(c, "onMe.speed", err)
	}
	duelW, duelL, err := b.st.DuelRecord(c.Chat().ID, player.ID)
	if err != nil {
		return b.fail(c, "onMe.duel", err)
	}
	return c.Send(meText(player.Name, stat, sp, duelW, duelL, season) +
		b.meExtra(c.Chat().ID, player.ID, b.chatTZ(c.Chat().ID)))
}

// historyDefault is the page size for the History tab / bare /history.
// historyMax caps "/history all" so the message stays under Telegram's 4096 limit.
const (
	historyDefault = 8
	historyMax     = 50
)

// onHistory shows recent games. "/history" → last 8; "/history N" → last N;
// "/history all" → up to historyMax.
func (b *Bot) onHistory(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onHistory.ensure", err)
	}
	n := historyDefault
	if args := c.Args(); len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "all", "все", "всё":
			n = historyMax
		default:
			if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
				n = min(v, historyMax)
			}
		}
	}
	games, err := b.st.RecentGames(c.Chat().ID, n)
	if err != nil {
		return b.fail(c, "onHistory.recent", err)
	}
	text := historyText(games, b.chatTZ(c.Chat().ID))
	// Hint at the full list only when the view is likely truncated.
	if n == historyDefault && len(games) >= historyDefault {
		text += "\n<i>/history all — весь список</i>"
	}
	return c.Send(text)
}

const settingsUsage = `⚙️ <b>Настройки</b>
/settings target &lt;N&gt; — порог сезона (очки до победы)
/settings points &lt;a b c…&gt; — таблица очков по местам
/settings minplayers &lt;N&gt; — мин. участников, чтобы игра засчиталась
/settings daily &lt;HH:MM|off&gt; — ежедневное напоминание
/settings weekly &lt;on|off&gt; — недельный дайджест (понедельник)`

func (b *Bot) onSettings(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSettings.ensure", err)
	}
	chatID := c.Chat().ID
	args := c.Args()
	if len(args) == 0 {
		return c.Send(b.settingsSummary(chatID, season))
	}
	if !b.requireAdmin(c) {
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "target":
		n, err := strconv.Atoi(argAt(args, 1))
		if err != nil || n <= 0 {
			return b.ephemeral(c, "Укажи число: /settings target 100")
		}
		if err := b.st.UpdateSeasonTarget(season.ID, n); err != nil {
			return b.fail(c, "onSettings.target", err)
		}
		return c.Send(fmt.Sprintf("✅ Порог сезона: <b>%d</b> очков", n))

	case "points":
		table, err := parseInts(args[1:])
		if err != nil || len(table) == 0 {
			return b.ephemeral(c, "Укажи очки по местам: /settings points 3 1 0")
		}
		if err := b.st.UpdateSeasonPoints(season.ID, table); err != nil {
			return b.fail(c, "onSettings.points", err)
		}
		return c.Send("✅ Таблица очков: " + formatTable(table))

	case "minplayers":
		n, err := strconv.Atoi(argAt(args, 1))
		if err != nil || n < 2 {
			return b.ephemeral(c, "Минимум участников в игре (≥2): /settings minplayers 3")
		}
		if err := b.st.SetMinPlayers(chatID, n); err != nil {
			return b.fail(c, "onSettings.minplayers", err)
		}
		return c.Send(fmt.Sprintf("✅ Игра засчитывается, если участвовало ≥ <b>%d</b> игроков.", n))

	case "daily":
		val := strings.ToLower(argAt(args, 1))
		if val == "off" {
			if err := b.st.SetDailyReminder(chatID, false, ""); err != nil {
				return b.fail(c, "onSettings.daily", err)
			}
			return c.Send("🔕 Ежедневные напоминания выключены.")
		}
		if _, err := time.Parse("15:04", val); err != nil {
			return b.ephemeral(c, "Формат времени HH:MM, например: /settings daily 21:00")
		}
		if err := b.st.SetDailyReminder(chatID, true, val); err != nil {
			return b.fail(c, "onSettings.daily", err)
		}
		return c.Send("🔔 Ежедневное напоминание в " + val)

	case "weekly":
		val := strings.ToLower(argAt(args, 1))
		on := val == "on" || val == "вкл" || val == "1"
		off := val == "off" || val == "выкл" || val == "0"
		if !on && !off {
			return b.ephemeral(c, "Включить/выключить недельный дайджест: /settings weekly on|off")
		}
		if err := b.st.SetWeeklyDigest(c.Chat().ID, on); err != nil {
			return b.fail(c, "onSettings.weekly", err)
		}
		if on {
			return c.Send("🗓 Недельный дайджест включён (понедельник).")
		}
		return c.Send("🔕 Недельный дайджест выключен.")

	default:
		return c.Send(settingsUsage)
	}
}

func (b *Bot) settingsSummary(chatID int64, season *storage.Season) string {
	daily := "выкл"
	weekly := "вкл"
	minPlayers := 2
	if ch, err := b.st.GetChat(chatID); err == nil {
		if ch.DailyReminder {
			daily = "вкл, " + ch.DailyTime
		}
		if !ch.WeeklyDigest {
			weekly = "выкл"
		}
		if ch.MinPlayers > 0 {
			minPlayers = ch.MinPlayers
		}
	}
	return fmt.Sprintf(
		"⚙️ <b>Настройки</b>\n"+
			"Порог сезона: <b>%d</b>\n"+
			"Таблица очков: <b>%s</b>\n"+
			"Мин. участников в игре: <b>%d</b>\n"+
			"Ежедневное напоминание: <b>%s</b>\n"+
			"Недельный дайджест: <b>%s</b>\n\n%s",
		season.Target, formatTable(season.PointsTable), minPlayers, daily, weekly, settingsUsage)
}

// isAdmin reports whether the sender may perform privileged actions: in a group
// they must be a Telegram admin/creator; a private chat is always "admin".
func (b *Bot) isAdmin(c tele.Context) bool {
	chat := c.Chat()
	if chat == nil || chat.Type == tele.ChatPrivate {
		return true
	}
	u := realSender(c)
	if u == nil {
		return false
	}
	m, err := b.tb.ChatMemberOf(chat, u)
	if err != nil {
		log.Printf("isAdmin: %v", err)
		return false
	}
	return m.Role == tele.Creator || m.Role == tele.Administrator
}

// statsView builds the dashboard text + tab keyboard for one tab. The "me" tab
// is rendered for whoever triggered the update (c.Sender()).
func (b *Bot) statsView(c tele.Context, tab string) (string, *tele.ReplyMarkup, error) {
	season, err := b.ensure(c)
	if err != nil {
		return "", nil, err
	}
	chatID := c.Chat().ID
	var text string
	switch tab {
	case "me":
		text, err = b.meTab(c, season)
	case "speed":
		ranked, fewer, e := b.st.Speedboard(chatID, season.ID, "medium", speedMinGames)
		if e != nil {
			return "", nil, e
		}
		text = speedText(season, "medium", ranked, fewer, speedMinGames)
	case "duels":
		rows, e := b.duelStandingsWithElo(chatID)
		if e != nil {
			return "", nil, e
		}
		recent, e := b.st.RecentDuels(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = duelsText(rows, recent, b.chatTZ(chatID))
	case "history":
		games, e := b.st.RecentGames(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = historyText(games, b.chatTZ(chatID))
	case "records":
		recs, e := b.st.RecordsBoard(chatID)
		if e != nil {
			return "", nil, e
		}
		text = recordsText(recs)
	default:
		tab = "table"
		standings, e := b.st.Standings(chatID, season.ID)
		if e != nil {
			return "", nil, e
		}
		text = standingsText(season, standings)
	}
	if err != nil {
		return "", nil, err
	}
	return text, statsKeyboard(tab), nil
}

// meTab renders the caller's personal stats, or a join hint if unregistered.
func (b *Bot) meTab(c tele.Context, season *storage.Season) (string, error) {
	sender := realSender(c)
	if sender == nil {
		return "Не удалось определить пользователя.", nil
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, sender.ID)
	if err != nil {
		return "", err
	}
	if player == nil {
		return "Ты ещё не в игре. Жми /join", nil
	}
	stat, err := b.st.StatFor(c.Chat().ID, season.ID, player.ID)
	if err != nil {
		return "", err
	}
	sp, err := b.st.SpeedFor(c.Chat().ID, season.ID, player.ID, "medium")
	if err != nil {
		return "", err
	}
	duelW, duelL, err := b.st.DuelRecord(c.Chat().ID, player.ID)
	if err != nil {
		return "", err
	}
	return meText(player.Name, stat, sp, duelW, duelL, season) +
		b.meExtra(c.Chat().ID, player.ID, b.chatTZ(c.Chat().ID)), nil
}

func (b *Bot) onStats(c tele.Context) error {
	text, markup, err := b.statsView(c, "table")
	if err != nil {
		return b.fail(c, "onStats", err)
	}
	return c.Send(text, markup)
}

// onStatsTab edits the single shared group message in place; the "me" tab therefore
// renders for whoever last tapped, not as a per-user private view — this is by design.
func (b *Bot) onStatsTab(c tele.Context) error {
	text, markup, err := b.statsView(c, c.Data())
	if err != nil {
		return b.fail(c, "onStatsTab", err)
	}
	_ = c.Respond()
	return c.Edit(text, markup)
}

func (b *Bot) requireAdmin(c tele.Context) bool {
	if b.isAdmin(c) {
		return true
	}
	_ = b.ephemeral(c, "⛔ Это действие доступно только админам группы.")
	return false
}

// speedMinGames is the minimum timed games to be ranked on /speed (vs listed
// in the "мало игр" footer).
const speedMinGames = 3

// onSpeed shows the speed leaderboard. Args in any order: a difficulty
// (default medium) and "all" for the all-seasons career board.
func (b *Bot) onSpeed(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSpeed.ensure", err)
	}
	difficulty, all := "medium", false
	for _, a := range c.Args() {
		switch low := strings.ToLower(a); {
		case validDifficulty[low]:
			difficulty = low
		case low == "all" || low == "все" || low == "всё":
			all = true
		}
	}
	seasonID, se := season.ID, season
	if all {
		seasonID, se = 0, nil
	}
	ranked, fewer, err := b.st.Speedboard(c.Chat().ID, seasonID, difficulty, speedMinGames)
	if err != nil {
		return b.fail(c, "onSpeed.board", err)
	}
	text := speedText(se, difficulty, ranked, fewer, speedMinGames)
	if !all && len(ranked) > 0 {
		text += "\n<i>/speed all — топ за все сезоны</i>"
	}
	return c.Send(text)
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func parseInts(ss []string) ([]int, error) {
	out := make([]int, 0, len(ss))
	for _, s := range ss {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
