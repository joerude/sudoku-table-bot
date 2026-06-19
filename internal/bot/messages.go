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
/setnick &lt;ник&gt; — твой ник на usdoku (для авто-учёта)
/players — список игроков
/removeplayer &lt;имя&gt; — убрать игрока

<b>Игра</b>
/newgame &lt;easy|medium|hard|extreme&gt; [hardcore|original] — новая игра + ссылка
/result — записать результат (жми игроков по порядку финиша)

<b>Статистика</b>
/status — таблица сезона
/season — инфо о сезоне
/me — моя статистика
/history — последние игры
/export — выгрузить CSV (игры + очки)

<b>Настройки</b> (только админ)
/settings — порог сезона, таблица очков, напоминания
/backup — выгрузить базу файлом; чтобы восстановить — пришли файл с подписью /restore`

const anonMsg = "🙈 Похоже, ты пишешь анонимно (от имени группы). Отключи «Оставаться анонимным» в правах админа или напиши от своего аккаунта — тогда я тебя привяжу."

const welcomeText = `👋 <b>Привет!</b> Я веду учёт ваших игр в судоку (usdoku.com) — очки, места, сезоны.

<b>С чего начать:</b>
1. Каждый игрок: /join
2. Каждый: /setnick &lt;ник на usdoku&gt; — чтобы результат записывался автоматически
3. /newgame medium — создать игру

Все команды — /help. Удобные кнопки ниже 👇`

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

// newGameWithCodeText is posted when the bot created a real usdoku game.
func newGameWithCodeText(difficulty, mode, code string) string {
	return fmt.Sprintf(
		"🧩 <b>Игра создана</b> · %s · %s\n\n"+
			"Заходите и играйте:\n%s/%s\n"+
			"Код: <b>%s</b>\n\n"+
			"🤖 Результат подтянется автоматически, когда доиграете "+
			"(у кого задан /setnick). Или жми «📝 Записать результат».",
		titleCase(difficulty), titleCase(mode), "https://www.usdoku.com", code, code)
}

// autoResultHeader prefixes an auto-captured result.
func autoResultHeader() string {
	return "🤖 <b>Результат подтянут с usdoku</b>\n"
}

// autoMappingText is shown when the bot can't fully map usdoku nicknames to players.
func autoMappingText(orderNicks, unknown []string) string {
	var b strings.Builder
	b.WriteString("🤖 <b>Игра на usdoku завершена</b>\nПорядок финиша:\n")
	for i, n := range orderNicks {
		b.WriteString(fmt.Sprintf("%s %s\n", medal(i+1), esc(n)))
	}
	if len(unknown) > 0 {
		b.WriteString(fmt.Sprintf(
			"\nНе узнал ники: <b>%s</b>\nПусть эти игроки сделают /setnick &lt;ник&gt;, "+
				"либо запишите вручную:", esc(strings.Join(unknown, ", "))))
	} else {
		b.WriteString("\nНужно минимум 2 знакомых игрока. Запишите вручную:")
	}
	return b.String()
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

// resultText is the finalised game summary: winner spotlight, per-finisher
// points + solve time (when known), and the season leader's progress.
func resultText(rows []storage.ResultRow, difficulty, mode string, leader *storage.Standing, target int) string {
	var b strings.Builder
	b.WriteString("🏁 <b>Игра записана</b>")
	if tag := gameTag(difficulty, mode); tag != "" {
		b.WriteString(" · " + tag)
	}
	b.WriteString("\n")

	if len(rows) > 0 {
		b.WriteString(fmt.Sprintf("\n🏆 <b>%s</b> побеждает!\n", esc(rows[0].Name)))
	}
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%s <b>%s</b> — %s", medal(r.Rank), esc(r.Name), signedPts(r.Points)))
		if r.Duration > 0 {
			b.WriteString(" · ⏱ " + fmtDuration(r.Duration))
		}
		b.WriteString("\n")
	}

	if leader != nil && leader.Points > 0 && target > 0 {
		b.WriteString(fmt.Sprintf("\n📈 <b>%s</b>: %d/%d %s",
			esc(leader.Name), leader.Points, target, progressBar(leader.Points, target)))
	}
	return b.String()
}

// gameTag renders the "Difficulty · Mode" suffix, omitting empty parts.
func gameTag(difficulty, mode string) string {
	var parts []string
	if difficulty != "" {
		parts = append(parts, titleCase(difficulty))
	}
	if mode != "" {
		parts = append(parts, titleCase(mode))
	}
	return strings.Join(parts, " · ")
}

// signedPts shows a positive score with a leading "+"; zero stays plain.
func signedPts(p int) string {
	if p > 0 {
		return fmt.Sprintf("+%d", p)
	}
	return fmt.Sprintf("%d", p)
}

// fmtDuration renders a solve time in seconds as m:ss.
func fmtDuration(secs int) string {
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// standingsText renders the season leaderboard as a medal list.
func standingsText(se *storage.Season, rows []storage.Standing) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🏆 <b>Сезон %d</b> · до %d очков\n\n", se.Number, se.Target))
	if len(rows) == 0 {
		b.WriteString("Пока нет игроков. /join")
		return b.String()
	}
	for i, r := range rows {
		b.WriteString(fmt.Sprintf("%s <b>%s</b> — <b>%d</b>   <i>(%d поб · %d игр)</i>\n",
			medal(i+1), esc(r.Name), r.Points, r.Wins, r.Games))
	}
	if leader := rows[0]; leader.Points > 0 {
		b.WriteString(fmt.Sprintf("\n📈 <b>%s</b>: %d/%d %s",
			esc(leader.Name), leader.Points, se.Target, progressBar(leader.Points, se.Target)))
	}
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
