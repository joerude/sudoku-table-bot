package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

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
		return c.Send("Не удалось определить пользователя.")
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, c.Sender().ID)
	if err != nil {
		return b.fail(c, "onMe.player", err)
	}
	if player == nil {
		return c.Send("Ты ещё не в игре. Жми /join")
	}
	stat, err := b.st.StatFor(c.Chat().ID, season.ID, player.ID)
	if err != nil {
		return b.fail(c, "onMe.stat", err)
	}
	return c.Send(meText(player.Name, stat, season))
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
	if !b.requireAdmin(c, chatID) {
		return nil
	}

	switch strings.ToLower(args[0]) {
	case "target":
		n, err := strconv.Atoi(argAt(args, 1))
		if err != nil || n <= 0 {
			return c.Send("Укажи число: /settings target 100")
		}
		if err := b.st.UpdateSeasonTarget(season.ID, n); err != nil {
			return b.fail(c, "onSettings.target", err)
		}
		return c.Send(fmt.Sprintf("✅ Порог сезона: <b>%d</b> очков", n))

	case "points":
		table, err := parseInts(args[1:])
		if err != nil || len(table) == 0 {
			return c.Send("Укажи очки по местам: /settings points 3 1 0")
		}
		if err := b.st.UpdateSeasonPoints(season.ID, table); err != nil {
			return b.fail(c, "onSettings.points", err)
		}
		return c.Send("✅ Таблица очков: " + formatTable(table))

	case "daily":
		val := strings.ToLower(argAt(args, 1))
		if val == "off" {
			if err := b.st.SetDailyReminder(chatID, false, ""); err != nil {
				return b.fail(c, "onSettings.daily", err)
			}
			return c.Send("🔕 Ежедневные напоминания выключены.")
		}
		if _, err := time.Parse("15:04", val); err != nil {
			return c.Send("Формат времени HH:MM, например: /settings daily 21:00")
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
	if ch, err := b.st.GetChat(chatID); err == nil && ch.DailyReminder {
		daily = "вкл, " + ch.DailyTime
	}
	return fmt.Sprintf(
		"⚙️ <b>Настройки</b>\n"+
			"Порог сезона: <b>%d</b>\n"+
			"Таблица очков: <b>%s</b>\n"+
			"Ежедневное напоминание: <b>%s</b>\n\n%s",
		season.Target, formatTable(season.PointsTable), daily, settingsUsage)
}

func (b *Bot) requireAdmin(c tele.Context, chatID int64) bool {
	admin, err := b.st.ChatAdmin(chatID)
	if err != nil {
		_ = b.fail(c, "requireAdmin", err)
		return false
	}
	if admin != 0 && c.Sender() != nil && c.Sender().ID != admin {
		_ = c.Send("⛔ Менять настройки может только админ (тот, кто первым запустил бота в чате).")
		return false
	}
	return true
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
