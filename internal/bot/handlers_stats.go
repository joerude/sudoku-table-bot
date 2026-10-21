package bot

import (
	"bytes"
	"encoding/csv"
	"errors"
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
	return badgeCollectionText(domain.BadgeProgress(in))
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

// onStatus is an alias of the stats hub's Table tab (with the season deadline).
func (b *Bot) onStatus(c tele.Context) error { return b.openStatsTab(c, "table") }

// onDuelsHub is the /duels alias: the stats hub opened on the Duels tab.
func (b *Bot) onDuelsHub(c tele.Context) error { return b.openStatsTab(c, "duels") }

// openStatsTab sends the stats hub as a new message on the given tab. The
// per-topic commands (/status, /me, /duels, /rating, ...) are aliases of it.
func (b *Bot) openStatsTab(c tele.Context, tab string) error {
	text, markup, err := b.statsView(c, tab)
	if err != nil {
		return b.fail(c, "openStatsTab."+tab, err)
	}
	return c.Send(text, markup)
}

// isNotModified reports Telegram's "message is not modified" edit error, which
// a tap on the already-open tab produces.
func isNotModified(err error) bool {
	return errors.Is(err, tele.ErrSameMessageContent) || errors.Is(err, tele.ErrMessageNotModified) ||
		(err != nil && strings.Contains(err.Error(), "message is not modified"))
}

// editInPlace swaps the callback's message for text+markup. Re-tapping the tab
// that is already open changes nothing, which Telegram reports as an error;
// that becomes an «Обновлено» toast instead.
func editInPlace(c tele.Context, text string, markup *tele.ReplyMarkup) error {
	err := c.Edit(text, markup)
	if isNotModified(err) {
		return c.Respond(&tele.CallbackResponse{Text: "Обновлено"})
	}
	_ = c.Respond()
	return err
}

// onSeason without args shows the active season; "/season N" shows any
// season's summary, including archived ones. Both carry the season buttons.
func (b *Bot) onSeason(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSeason.ensure", err)
	}
	nums, err := b.st.ArchivedNumbers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onSeason.archived", err)
	}
	if n, aerr := strconv.Atoi(argAt(c.Args(), 0)); aerr == nil && n > 0 {
		text, ok, err := b.seasonSummaryView(c.Chat().ID, n)
		if err != nil {
			return b.fail(c, "onSeason.summary", err)
		}
		if !ok {
			return b.ephemeral(c, noSuchSeasonText(n, nums))
		}
		return c.Send(text, seasonViewKeyboard(nums, season.Number, n))
	}
	leader, err := b.st.Leader(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onSeason.leader", err)
	}
	text := seasonText(season, leader) +
		b.seasonStatus(c.Chat().ID, season) +
		archiveHint(nums)
	return c.Send(text, seasonsKeyboard(nums, season.Number))
}

// seasonSummaryView builds season `number`'s summary (final table, dates,
// winner, nominations); ok=false when the chat has no such season.
func (b *Bot) seasonSummaryView(chatID int64, number int) (string, bool, error) {
	se, err := b.st.SeasonByNumber(chatID, number)
	if err != nil || se == nil {
		return "", false, err
	}
	standings, err := b.st.Standings(chatID, se.ID)
	if err != nil {
		return "", false, err
	}
	games, first, last, winner, err := b.st.SeasonMeta(chatID, se.ID)
	if err != nil {
		return "", false, err
	}
	awards := b.seasonAwards(chatID, se.ID, standings)
	return seasonSummaryText(se, evenGames(standings, games), games, first, last, winner, awards), true, nil
}

// onSeasonView switches the message to the tapped season's summary (or the
// full season list for «Архив») in place.
func (b *Bot) onSeasonView(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSeasonView.ensure", err)
	}
	nums, err := b.st.ArchivedNumbers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onSeasonView.archived", err)
	}
	if c.Data() == seasonArchive {
		return editInPlace(c, "📚 <b>Архив сезонов</b>\nВыбери сезон.", archiveKeyboard(nums, season.Number))
	}
	n, aerr := strconv.Atoi(c.Data())
	if aerr != nil || n < 1 {
		return c.Respond(&tele.CallbackResponse{Text: "Сезон не найден"})
	}
	text, ok, err := b.seasonSummaryView(c.Chat().ID, n)
	if err != nil {
		return b.fail(c, "onSeasonView.summary", err)
	}
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: "Сезон не найден"})
	}
	return editInPlace(c, text, seasonViewKeyboard(nums, season.Number, n))
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

// onMe is an alias of the stats hub's «Я» tab.
// onMe opens the stats hub on the "me" tab. An ephemeral /me gets just that
// tab's text, privately and without the hub (its tabs would edit a message
// nobody else sees).
func (b *Bot) onMe(c tele.Context) error {
	if incomingEphemeral(c) == 0 {
		return b.openStatsTab(c, "me")
	}
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onMe.ensure", err)
	}
	text, err := b.meTab(c, season)
	if err != nil {
		return b.fail(c, "onMe.tab", err)
	}
	if b.privately(c, clipText(text, tgMaxText), nil) {
		return nil
	}
	return b.openStatsTab(c, "me")
}

// historyDefault is the page size for the History tab / bare /history.
// historyMax caps "/history all" so the message stays under Telegram's 4096 limit.
const (
	historyDefault = 8
	historyMax     = 50
)

// onHistory shows recent games. "/history" opens the stats hub on its History
// tab; "/history N" → last N; "/history all" → up to historyMax.
func (b *Bot) onHistory(c tele.Context) error {
	if len(c.Args()) == 0 {
		return b.openStatsTab(c, "history")
	}
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
/settings mingames &lt;N&gt; — игр за сезон до закрытия по дедлайну
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
		return b.quiet(c, b.settingsSummary(chatID, season, b.isAdmin(c)))
	}
	if !b.requireAdmin(c) {
		return nil
	}

	key := strings.ToLower(args[0])
	if f, ok := featureByKey(key); ok {
		return b.onFeatureSwitch(c, f, argAt(args, 1))
	}
	switch key {
	case "target":
		n, err := strconv.Atoi(argAt(args, 1))
		if err != nil || n <= 0 {
			return b.ephemeral(c, "Укажи число: /settings target 100")
		}
		if err := b.st.UpdateSeasonTarget(season.ID, n); err != nil {
			return b.fail(c, "onSettings.target", err)
		}
		return b.quiet(c, fmt.Sprintf("✅ Порог сезона: <b>%d</b> %s", n, pointsWord(n)))

	case "points":
		table, err := parseInts(args[1:])
		if err != nil || len(table) == 0 {
			return b.ephemeral(c, "Укажи очки по местам: /settings points 3 1 0")
		}
		if err := b.st.UpdateSeasonPoints(season.ID, table); err != nil {
			return b.fail(c, "onSettings.points", err)
		}
		return b.quiet(c, "✅ Таблица очков: "+formatTable(table))

	case "mingames":
		n, err := strconv.Atoi(argAt(args, 1))
		if err != nil || n < 1 {
			return b.ephemeral(c, "Сколько игр нужно сыграть за сезон: /settings mingames 66")
		}
		if err := b.st.UpdateSeasonMinGames(season.ID, n); err != nil {
			return b.fail(c, "onSettings.mingames", err)
		}
		return b.quiet(c, fmt.Sprintf("✅ Сезон закроется по дедлайну, только если сыграно не меньше <b>%d</b> %s.",
			n, plural(n, "игры", "игр", "игр")))

	case "minplayers":
		n, err := strconv.Atoi(argAt(args, 1))
		if err != nil || n < 2 {
			return b.ephemeral(c, "Минимум участников в игре (≥2): /settings minplayers 3")
		}
		if err := b.st.SetMinPlayers(chatID, n); err != nil {
			return b.fail(c, "onSettings.minplayers", err)
		}
		return b.quiet(c, fmt.Sprintf("✅ Игра засчитывается, если участвовало ≥ <b>%d</b> %s.", n, plural(n, "игрока", "игроков", "игроков")))

	case "daily":
		val := strings.ToLower(argAt(args, 1))
		if val == "off" {
			if err := b.st.SetDailyReminder(chatID, false, ""); err != nil {
				return b.fail(c, "onSettings.daily", err)
			}
			return b.quiet(c, "🔕 Ежедневные напоминания выключены.")
		}
		val, ok := normalizeHHMM(val)
		if !ok {
			return b.ephemeral(c, "Формат времени HH:MM, например: /settings daily 21:00")
		}
		if err := b.st.SetDailyReminder(chatID, true, val); err != nil {
			return b.fail(c, "onSettings.daily", err)
		}
		return b.quiet(c, "🔔 Ежедневное напоминание в "+val)

	case "weekly":
		on, ok := parseOnOff(argAt(args, 1))
		if !ok {
			return b.ephemeral(c, "Включить/выключить недельный дайджест: /settings weekly on|off")
		}
		if err := b.st.SetWeeklyDigest(c.Chat().ID, on); err != nil {
			return b.fail(c, "onSettings.weekly", err)
		}
		if on {
			return b.quiet(c, "🗓 Недельный дайджест включён (понедельник).")
		}
		return b.quiet(c, "🔕 Недельный дайджест выключен.")

	default:
		return b.quiet(c, settingsUsage+featureUsage())
	}
}

func (b *Bot) settingsSummary(chatID int64, season *storage.Season, admin bool) string {
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
			"Игр за сезон: <b>%d</b>\n"+
			"Таблица очков: <b>%s</b>\n"+
			"Мин. участников в игре: <b>%d</b>\n"+
			"Ежедневное напоминание: <b>%s</b>\n"+
			"Недельный дайджест: <b>%s</b>\n%s\n%s",
		season.Target, season.MinGames, formatTable(season.PointsTable), minPlayers,
		daily, weekly, featureSummary(b.featureStates(chatID)), settingsFooter(admin))
}

// seasonStatus renders the deadline and games-floor block for a season panel.
// The counter is left out when the count can't be read (the panel stays
// useful without it).
func (b *Bot) seasonStatus(chatID int64, season *storage.Season) string {
	loc := loadLoc(b.chatTZ(chatID))
	games, first, _, _, err := b.st.SeasonMeta(chatID, season.ID)
	if err != nil {
		log.Printf("seasonStatus: %v", err)
		return seasonDeadlineLine(season, time.Now(), loc)
	}
	return seasonStatusBlock(season, games, first, time.Now(), loc)
}

// isAdmin reports whether the sender may perform privileged actions: in a group
// they must be a Telegram admin/creator; a private chat is always "admin".
func (b *Bot) isAdmin(c tele.Context) bool {
	chat := c.Chat()
	if chat == nil || chat.Type == tele.ChatPrivate {
		return true
	}
	return b.isGroupAdmin(c)
}

// isGroupAdmin reports whether the sender is a Telegram admin/creator of the
// current chat. False for anonymous senders and on API errors.
func (b *Bot) isGroupAdmin(c tele.Context) bool {
	chat := c.Chat()
	u := realSender(c)
	if chat == nil || u == nil {
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
// is rendered for whoever triggered the update (realSender).
func (b *Bot) statsView(c tele.Context, tab string) (string, *tele.ReplyMarkup, error) {
	season, err := b.ensure(c)
	if err != nil {
		return "", nil, err
	}
	chatID := c.Chat().ID
	if tab == "records" { // legacy button: records moved into the Speed tab
		tab = "speed"
	}
	var text string
	switch tab {
	case "me":
		text, err = b.meTab(c, season)
	case "speed", "speed_all":
		seasonID, se := season.ID, season
		if tab == "speed_all" {
			seasonID, se = 0, nil
		}
		ranked, fewer, e := b.st.Speedboard(chatID, seasonID, "medium", speedMinGames)
		if e != nil {
			return "", nil, e
		}
		recs, e := b.st.RecordsBoard(chatID)
		if e != nil {
			return "", nil, e
		}
		titles, e := b.st.TitlesBoard(chatID)
		if e != nil {
			return "", nil, e
		}
		text = speedText(se, "medium", ranked, fewer, speedMinGames) + "\n\n" + recordsText(recs, titles)
	case "rating":
		text, err = b.ratingTab(chatID)
	case "duels":
		text, err = b.duelsPanel(chatID)
		if err != nil {
			return "", nil, err
		}
	case "history":
		games, e := b.st.RecentGames(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = historyText(games, b.chatTZ(chatID))
	case "activity":
		since := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02 15:04:05")
		init, e := b.st.InitiativeStats(chatID, since)
		if e != nil {
			return "", nil, e
		}
		text = activityText(init, time.Now(), loadLoc(b.chatTZ(chatID)))
	default:
		tab = "table"
		standings, e := b.st.Standings(chatID, season.ID)
		if e != nil {
			return "", nil, e
		}
		text = standingsText(season, standings, b.recentRace(chatID, season), b.seasonForm(chatID, season)) +
			b.seasonStatus(chatID, season)
	}
	if err != nil {
		return "", nil, err
	}
	var archived []int
	if tab == "table" {
		if archived, err = b.st.ArchivedNumbers(chatID); err != nil {
			return "", nil, err
		}
	}
	return clipText(text, tgMaxText), statsKeyboard(tab, archived, season.Number), nil
}

// raceWindowDays is how far back the table's race columns look.
const raceWindowDays = 7

// recentRace reads the table's race window. Best-effort: on an error the table
// is shown without the window columns.
func (b *Bot) recentRace(chatID int64, season *storage.Season) raceWindow {
	since := time.Now().UTC().AddDate(0, 0, -raceWindowDays).Format("2006-01-02 15:04:05")
	gains, games, err := b.st.PointsSince(chatID, season.ID, since)
	if err != nil {
		log.Printf("recentRace: %v", err)
		return raceWindow{Days: raceWindowDays}
	}
	return raceWindow{Days: raceWindowDays, Games: games, Gains: gains}
}

// seasonForm is each player's season ranks in game order, for the table's form
// line. Best-effort: on an error the table is shown without it.
func (b *Bot) seasonForm(chatID int64, season *storage.Season) map[int64][]int {
	rows, err := b.st.SeasonRanks(chatID, season.ID)
	if err != nil {
		log.Printf("seasonForm: %v", err)
		return nil
	}
	form := make(map[int64][]int)
	for _, r := range rows {
		form[r.PlayerID] = append(form[r.PlayerID], r.Rank)
	}
	return form
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
	duelSp, duelStreak, err := b.duelExtras(c.Chat().ID, player.ID)
	if err != nil {
		return "", err
	}
	return meText(player.Name, stat, sp, duelW, duelL, duelSp, duelStreak, season) +
		b.meExtra(c.Chat().ID, player.ID, b.chatTZ(c.Chat().ID)), nil
}

func (b *Bot) onStats(c tele.Context) error { return b.openStatsTab(c, "table") }

// onStatsTab edits the single shared group message in place; the "me" tab therefore
// renders for whoever last tapped, not as a per-user private view — this is by design.
func (b *Bot) onStatsTab(c tele.Context) error {
	text, markup, err := b.statsView(c, c.Data())
	if err != nil {
		return b.fail(c, "onStatsTab", err)
	}
	return editInPlace(c, text, markup)
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
	if difficulty == "medium" { // the hub's Speed tab covers medium
		if all {
			return b.openStatsTab(c, "speed_all")
		}
		return b.openStatsTab(c, "speed")
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
