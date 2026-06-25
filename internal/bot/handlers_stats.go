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

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

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

func (b *Bot) onSeason(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSeason.ensure", err)
	}
	leader, err := b.st.Leader(c.Chat().ID, season.ID)
	if err != nil {
		return b.fail(c, "onSeason.leader", err)
	}
	return c.Send(seasonText(season, leader))
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
	return c.Send(meText(player.Name, stat, sp, duelW, duelL, season))
}

func (b *Bot) onHistory(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onHistory.ensure", err)
	}
	games, err := b.st.RecentGames(c.Chat().ID, 8)
	if err != nil {
		return b.fail(c, "onHistory.recent", err)
	}
	return c.Send(historyText(games))
}

const settingsUsage = `⚙️ <b>Настройки</b>
/settings target &lt;N&gt; — порог сезона (очки до победы)
/settings points &lt;a b c…&gt; — таблица очков по местам
/settings minplayers &lt;N&gt; — мин. участников, чтобы игра засчиталась
/settings daily &lt;HH:MM|off&gt; — ежедневное напоминание`

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

	default:
		return c.Send(settingsUsage)
	}
}

func (b *Bot) settingsSummary(chatID int64, season *storage.Season) string {
	daily := "выкл"
	minPlayers := 2
	if ch, err := b.st.GetChat(chatID); err == nil {
		if ch.DailyReminder {
			daily = "вкл, " + ch.DailyTime
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
			"Ежедневное напоминание: <b>%s</b>\n\n%s",
		season.Target, formatTable(season.PointsTable), minPlayers, daily, settingsUsage)
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
		rows, e := b.st.DuelLeaderboard(chatID)
		if e != nil {
			return "", nil, e
		}
		recent, e := b.st.RecentDuels(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = duelsText(rows, recent)
	case "history":
		games, e := b.st.RecentGames(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = historyText(games)
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
	return meText(player.Name, stat, sp, duelW, duelL, season), nil
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

func (b *Bot) onSpeed(c tele.Context) error {
	season, err := b.ensure(c)
	if err != nil {
		return b.fail(c, "onSpeed.ensure", err)
	}
	difficulty := "medium"
	if a := strings.ToLower(argAt(c.Args(), 0)); validDifficulty[a] {
		difficulty = a
	}
	ranked, fewer, err := b.st.Speedboard(c.Chat().ID, season.ID, difficulty, speedMinGames)
	if err != nil {
		return b.fail(c, "onSpeed.board", err)
	}
	return c.Send(speedText(season, difficulty, ranked, fewer, speedMinGames))
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
