package bot

import (
	"fmt"
	"strings"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

const usdokuCreateURL = "https://www.usdoku.com/create"

const helpText = `🧩 <b>Sudoku League</b> — учёт игр в судоку (usdoku.com).

<b>Игроки</b>
/join [имя] — зарегистрироваться
/players — список игроков

<b>Игра</b>
/newgame &lt;easy|medium|hard|extreme&gt; [hardcore|original] — новая игра + ссылка
/result — записать результат (жми игроков по порядку финиша)

<b>Статистика</b>
/status — таблица сезона
/season — инфо о сезоне
/me — моя статистика
/history — последние игры

<b>Настройки</b> (только админ)
/settings — порог сезона, таблица очков, напоминания`

// esc escapes the characters that matter for Telegram HTML parse mode.
func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func medal(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return fmt.Sprintf("%d.", rank)
	}
}

// newGameText is the message posted by /newgame.
func newGameText(difficulty, mode string) string {
	return fmt.Sprintf(
		"🧩 <b>Новая игра</b> · %s · %s\n\n"+
			"1. Открой и создай комнату:\n%s\n"+
			"2. Выбери <b>%s</b> и <b>%s</b> → <b>Create</b>\n"+
			"3. Скинь ссылку друзьям и играйте\n\n"+
			"Когда доиграете — жми «📝 Записать результат».",
		titleCase(difficulty), titleCase(mode), usdokuCreateURL, titleCase(difficulty), titleCase(mode))
}

// pickerText shows the current finish order while recording.
func pickerText(picked []storage.ResultRow) string {
	if len(picked) == 0 {
		return "🏁 <b>Кто как финишировал?</b>\nЖми игроков <b>по порядку финиша</b> (1-й, 2-й, 3-й…).\nКто не играл — просто не выбирай."
	}
	var b strings.Builder
	b.WriteString("🏁 <b>Кто как финишировал?</b>\nТекущий порядок:\n")
	for _, r := range picked {
		b.WriteString(fmt.Sprintf("%s %s\n", medal(r.Rank), esc(r.Name)))
	}
	b.WriteString("\nПродолжай тапать или жми «✅ Готово».")
	return b.String()
}

// resultText is the finalised game summary.
func resultText(rows []storage.ResultRow) string {
	var b strings.Builder
	b.WriteString("🏁 <b>Игра записана</b>\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%s %s — <b>%d</b>\n", medal(r.Rank), esc(r.Name), r.Points))
	}
	return b.String()
}

// standingsText renders the season leaderboard as a monospace table.
func standingsText(se *storage.Season, rows []storage.Standing) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🏆 <b>Сезон %d</b> · до %d очков\n", se.Number, se.Target))
	if len(rows) == 0 {
		b.WriteString("\nПока нет игроков. /join")
		return b.String()
	}
	b.WriteString("<pre>")
	b.WriteString(fmt.Sprintf("%-2s %-10s %4s %3s %3s\n", "#", "Игрок", "Очк", "W", "Игр"))
	for i, r := range rows {
		b.WriteString(fmt.Sprintf("%-2d %-10s %4d %3d %3d\n",
			i+1, escPre(trunc(r.Name, 10)), r.Points, r.Wins, r.Games))
	}
	b.WriteString("</pre>")
	return b.String()
}

// seasonText summarises the active season and the leader's progress.
func seasonText(se *storage.Season, leader *storage.Standing) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🏆 <b>Сезон %d</b>\nЦель: <b>%d</b> очков\nТаблица очков: %s\n\n",
		se.Number, se.Target, formatTable(se.PointsTable)))
	if leader == nil || leader.Points == 0 {
		b.WriteString("Ещё никто не набрал очков.")
		return b.String()
	}
	b.WriteString(fmt.Sprintf("Лидер: <b>%s</b> — %d/%d %s",
		esc(leader.Name), leader.Points, se.Target, progressBar(leader.Points, se.Target)))
	return b.String()
}

func meText(name string, st *storage.PlayerStat, se *storage.Season) string {
	if st.Games == 0 {
		return fmt.Sprintf("📊 <b>%s</b>\nВ сезоне %d пока нет игр. Сыграй и запиши: /result", esc(name), se.Number)
	}
	return fmt.Sprintf(
		"📊 <b>%s</b> · сезон %d\nМесто: <b>%d</b>\nОчки: <b>%d</b>/%d\nПобед: %d\nИгр: %d",
		esc(name), se.Number, st.Rank, st.Points, se.Target, st.Wins, st.Games)
}

func historyText(games []storage.HistoryGame) string {
	if len(games) == 0 {
		return "История пуста. Запиши первую игру: /result"
	}
	var b strings.Builder
	b.WriteString("📜 <b>Последние игры</b>\n")
	for _, g := range games {
		var order []string
		for i, name := range g.Order {
			order = append(order, medal(i+1)+esc(name))
		}
		diff := ""
		if g.Difficulty != "" {
			diff = " · " + titleCase(g.Difficulty)
		}
		b.WriteString(fmt.Sprintf("%s%s: %s\n", g.Date, diff, strings.Join(order, " ")))
	}
	return b.String()
}

func seasonEndText(number int, winner string, points, nextTarget, nextNumber int) string {
	return fmt.Sprintf(
		"🎉🏆 <b>Сезон %d завершён!</b>\nПобедитель: <b>%s</b> (%d очков) 👑\n\n"+
			"Начат <b>сезон %d</b> — все с нуля. Цель: %d очков. Поехали! 🔥",
		number, esc(winner), points, nextNumber, nextTarget)
}

// --- small helpers ---

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// escPre escapes only what matters inside a <pre> block.
func escPre(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func formatTable(t []int) string {
	parts := make([]string, len(t))
	for i, v := range t {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, "/")
}

func progressBar(cur, target int) string {
	const width = 10
	if target <= 0 {
		return ""
	}
	filled := cur * width / target
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return "[" + strings.Repeat("▰", filled) + strings.Repeat("▱", width-filled) + "]"
}
