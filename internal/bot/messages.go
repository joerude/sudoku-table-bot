package bot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

const usdokuCreateURL = "https://www.usdoku.com/create"

const helpText = `🧩 <b>Sudoku League</b> — учёт ваших игр в судоку (usdoku.com): очки, места, сезоны, дуэли.

<b>Главное</b>
/play — новая игра, дуэль или позвать всех (кнопки)
/stats — вся статистика: таблица, моё, скорость, дуэли, история
/join [имя] — встать в игру
/setnick &lt;ник&gt; — привязать ник usdoku (для авто-учёта)
/players — кто играет

<b>Ещё</b>
/result — записать результат вручную (если авто-учёт не сработал)
/newgame, /duel, /invite — то же, что /play, но командой
/status, /me, /speed, /duels, /history, /season — то же, что вкладки /stats
/export — выгрузить игры сезона в CSV

<b>Админ</b>
/settings — порог сезона, очки, мин. игроков, напоминания
/removeplayer &lt;имя&gt; — убрать игрока (прошлые игры сохранятся)
/backup — скачать базу; восстановить — пришли файл с подписью /restore`

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

// mention renders a player as a Telegram mention that pings them: a text-mention
// link by user id (works for everyone who joined), falling back to @username,
// then a plain escaped name.
func mention(p storage.Player) string {
	if p.TgID.Valid {
		return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, p.TgID.Int64, esc(p.Name))
	}
	if p.Username.Valid && p.Username.String != "" {
		return "@" + esc(p.Username.String)
	}
	return esc(p.Name)
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

	if len(rows) > 0 && rows[0].Rank >= 1 {
		b.WriteString(fmt.Sprintf("\n🏆 <b>%s</b> побеждает!\n", esc(rows[0].Name)))
	}
	for _, r := range rows {
		if r.Rank == 0 {
			fmt.Fprintf(&b, "— <b>%s</b> · <i>не финишировал</i>\n", esc(r.Name))
			continue
		}
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

// fmtDuration renders a solve time in seconds as m:ss, or h:mm:ss past an hour.
func fmtDuration(secs int) string {
	if secs >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
	}
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

func meText(name string, st *storage.PlayerStat, sp *storage.SpeedStat, duelW, duelL int, se *storage.Season) string {
	if st.Games == 0 {
		base := fmt.Sprintf("📊 <b>%s</b>\nВ сезоне %d пока нет игр. Сыграй и запиши: /result", esc(name), se.Number)
		if duelW+duelL > 0 {
			base += fmt.Sprintf("\n⚔️ Дуэли: <b>%d–%d</b>", duelW, duelL)
		}
		return base
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"📊 <b>%s</b> · сезон %d\nМесто: <b>%d</b>\nОчки: <b>%d</b>/%d\nПобед: %d\nИгр: %d",
		esc(name), se.Number, st.Rank, st.Points, se.Target, st.Wins, st.Games)
	if sp != nil && sp.Games > 0 {
		fmt.Fprintf(&b, "\n⏱ Ср. время: <b>%s</b> (по %d, medium)", fmtDuration(sp.AvgSecs), sp.Games)
		fmt.Fprintf(&b, "\n⚡ Лучшее: <b>%s</b>", fmtDuration(sp.BestSecs))
	} else {
		b.WriteString("\n⏱ Ср. время: — (нет авто-игр на medium)")
	}
	if duelW+duelL > 0 {
		fmt.Fprintf(&b, "\n⚔️ Дуэли: <b>%d–%d</b>", duelW, duelL)
	}
	return b.String()
}

// duelResultText renders a finished duel. rows are rank-ordered (winner first);
// winnerWins/loserWins are the pair's head-to-head tally, shown only when h2h.
func duelResultText(rows []storage.ResultRow, winnerWins, loserWins int, h2h bool) string {
	var b strings.Builder
	b.WriteString("⚔️ <b>Дуэль</b>\n")
	if len(rows) == 0 {
		b.WriteString("Никто не финишировал.")
		return b.String()
	}
	w := rows[0]
	if w.Rank == 0 {
		b.WriteString("Никто не финишировал.")
		return b.String()
	}
	fmt.Fprintf(&b, "🏆 <b>%s</b> побеждает", esc(w.Name))
	if len(rows) >= 2 {
		if rows[1].Rank == 0 {
			fmt.Fprintf(&b, " — %s не финишировал", esc(rows[1].Name))
		} else {
			fmt.Fprintf(&b, " — %s проигрывает", esc(rows[1].Name))
		}
	}
	if w.Duration > 0 {
		fmt.Fprintf(&b, " · ⏱ %s", fmtDuration(w.Duration))
	}
	if h2h && len(rows) >= 2 {
		fmt.Fprintf(&b, "\n\nH2H: <b>%s</b> %d–%d <b>%s</b>",
			esc(rows[0].Name), winnerWins, loserWins, esc(rows[1].Name))
	}
	return b.String()
}

// duelsText renders the /duels leaderboard (all-time duel records) and recent log.
func duelsText(rows []storage.DuelStanding, recent []storage.DuelMatch) string {
	if len(rows) == 0 {
		return "⚔️ <b>Дуэли</b>\nЕщё не было дуэлей. Вызови кого-нибудь: /duel"
	}
	var b strings.Builder
	b.WriteString("⚔️ <b>Дуэли</b> · рейтинг\n")
	for i, r := range rows {
		pct := 0
		if total := r.Wins + r.Losses; total > 0 {
			pct = r.Wins * 100 / total
		}
		fmt.Fprintf(&b, "%s <b>%s</b> — %d–%d <i>(%d%%)</i>\n",
			medal(i+1), esc(r.Name), r.Wins, r.Losses, pct)
	}
	if len(recent) > 0 {
		b.WriteString("\n<b>Последние дуэли</b>\n")
		for _, m := range recent {
			if m.Loser != "" {
				fmt.Fprintf(&b, "%s · <b>%s</b> обыграл %s\n", m.Date, esc(m.Winner), esc(m.Loser))
			} else {
				fmt.Fprintf(&b, "%s · <b>%s</b> (соперник не финишировал)\n", m.Date, esc(m.Winner))
			}
		}
	}
	return b.String()
}

// speedText renders the /speed leaderboard: players ranked by average solve
// time at a difficulty, with a footer for those below the games threshold.
func speedText(se *storage.Season, difficulty string, ranked, fewer []storage.SpeedRow, minGames int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚡ <b>Самые быстрые</b> · %s · сезон %d\n", titleCase(difficulty), se.Number)
	if len(ranked) == 0 {
		fmt.Fprintf(&b, "\nПока мало данных — нужно ≥%d авто-игр на %s.\n"+
			"Создавай игры через /newgame (и задай /setnick) — время подтянется само.",
			minGames, titleCase(difficulty))
	} else {
		for i, r := range ranked {
			fmt.Fprintf(&b, "%s <b>%s</b> — %s   <i>(%d игр · ⚡ %s)</i>\n",
				medal(i+1), esc(r.Name), fmtDuration(r.AvgSecs), r.Games, fmtDuration(r.BestSecs))
		}
	}
	if len(fewer) > 0 {
		names := make([]string, len(fewer))
		for i, r := range fewer {
			names[i] = esc(r.Name)
		}
		fmt.Fprintf(&b, "\n<i>Мало игр (&lt;%d): %s</i>", minGames, strings.Join(names, ", "))
	}
	return b.String()
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

// namesMissingNick returns the names of players with no usdoku nick set, in
// input order — used to warn before a game that auto-record will skip them.
func namesMissingNick(players []storage.Player) []string {
	var out []string
	for _, p := range players {
		if !p.UsdokuNick.Valid || p.UsdokuNick.String == "" {
			out = append(out, p.Name)
		}
	}
	return out
}

// parseDuelPick splits a cbDuelPick payload "<difficulty>:<targetPlayerID>".
func parseDuelPick(s string) (difficulty string, targetID int64) {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", 0
	}
	difficulty = s[:i]
	targetID, _ = strconv.ParseInt(s[i+1:], 10, 64)
	return difficulty, targetID
}

// duelChallengeText is the posted challenge. code may be empty (manual fallback).
// nickWarn appends a note when a participant has no usdoku nick (no auto-record).
func duelChallengeText(challenger string, target storage.Player, difficulty, code string, nickWarn bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ <b>%s</b> вызывает %s на дуэль! · %s\n",
		esc(challenger), mention(target), titleCase(difficulty))
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s\n", code)
	}
	b.WriteString("\nПринимаешь вызов?")
	if nickWarn {
		b.WriteString("\n\n⚠️ У кого-то не задан /setnick — авто-запись не сработает, запишите результат через /result.")
	}
	return b.String()
}

func duelAcceptText(target storage.Player, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔥 %s принял вызов! Поехали.\n", mention(target))
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s", code)
	}
	return b.String()
}

func duelDeclineText(target storage.Player) string {
	return fmt.Sprintf("❌ %s отказался. Дуэль отменена.", mention(target))
}

// inviteText is the /invite post: a ping of everyone, the room link, and the
// current "in" roster. pings are all active players (mentioned to notify them);
// roster is who tapped "Я в деле". Editing this message to refresh the roster
// does NOT re-notify (Telegram only notifies on new messages).
func inviteText(difficulty, code string, pings, roster []storage.Player) string {
	var b strings.Builder
	b.WriteString("🎮 <b>Играем в судоку!</b>")
	if difficulty != "" {
		b.WriteString(" · " + titleCase(difficulty))
	}
	b.WriteString("\n")
	if len(pings) > 0 {
		ms := make([]string, len(pings))
		for i, p := range pings {
			ms[i] = mention(p)
		}
		b.WriteString(strings.Join(ms, " ") + "\n")
	}
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s\n", code)
	}
	b.WriteString("\nЖми «Я в деле», если играешь 👇")
	if len(roster) > 0 {
		names := make([]string, len(roster))
		for i, p := range roster {
			names[i] = esc(p.Name)
		}
		b.WriteString("\n\n✅ В деле: " + strings.Join(names, ", "))
	}
	return b.String()
}

// difficultyRank orders records easy < medium < hard < extreme < other.
var difficultyRank = map[string]int{"easy": 0, "medium": 1, "hard": 2, "extreme": 3}

// recordsText renders the all-time fastest solve per difficulty.
func recordsText(rows []storage.RecordRow) string {
	if len(rows) == 0 {
		return "🏆 <b>Рекорды</b>\nПока нет рекордов — сыграйте авто-игру (нужен /setnick), и время попадёт сюда."
	}
	sorted := make([]storage.RecordRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, ok := difficultyRank[sorted[i].Difficulty]
		if !ok {
			ri = 99
		}
		rj, ok := difficultyRank[sorted[j].Difficulty]
		if !ok {
			rj = 99
		}
		return ri < rj
	})
	var b strings.Builder
	b.WriteString("🏆 <b>Рекорды</b> · лучшее время\n")
	for _, r := range sorted {
		fmt.Fprintf(&b, "<b>%s</b> — %s · %s\n", titleCase(r.Difficulty), fmtDuration(r.Secs), esc(r.Name))
	}
	return b.String()
}

// streakBadgeText renders the streak lines + badge row appended to /me. Returns
// "" when there is nothing noteworthy (streaks < 2 and no badges).
func streakBadgeText(winStreak, dayStreak int, badges []string) string {
	var b strings.Builder
	if winStreak >= 2 {
		fmt.Fprintf(&b, "\n🔥 Серия побед: <b>%d</b>", winStreak)
	}
	if dayStreak >= 2 {
		fmt.Fprintf(&b, "\n📅 Дней подряд: <b>%d</b>", dayStreak)
	}
	if len(badges) > 0 {
		b.WriteString("\n🏆 " + strings.Join(badges, " "))
	}
	return b.String()
}

// loadLoc loads a timezone, falling back to UTC.
func loadLoc(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// parseDBTime parses a SQLite datetime('now') string (UTC) into a time.Time.
func parseDBTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04:05", s)
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
