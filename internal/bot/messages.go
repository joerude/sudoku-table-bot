package bot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

const usdokuCreateURL = "https://www.usdoku.com/create"

const helpText = `🧩 <b>Sudoku League</b> — учёт ваших игр в судоку (usdoku.com): очки, места, сезоны, дуэли.

<b>Главное</b>
/play — новая игра, дуэль или позвать всех (кнопки)
/stats — вся статистика: таблица, моё, скорость, дуэли, история
/rating — лестница рейтинга (ELO) + корона 👑
/join [имя] — встать в игру
/setnick &lt;ник&gt; — привязать ник usdoku (для авто-учёта)
/players — кто играет

<b>Ещё</b>
/result — записать результат вручную (если авто-учёт не сработал)
/code &lt;КОД&gt; — привязать вручную созданную usdoku-комнату к текущей игре
/newgame, /duel, /invite — то же, что /play, но командой
/status, /me, /speed, /duels, /history, /season — то же, что вкладки /stats
/season &lt;N&gt; — сводка любого сезона, включая архивные
/weekly — итоги недели сейчас (то же, что авто-пост в пн)
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
			"3. Скинь ссылку друзьям и играйте\n"+
			"4. Пришли мне код комнаты: <code>/code LOVI</code> — включу авто-запись\n\n"+
			"Или когда доиграете — жми «📝 Записать результат».",
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

// pendingConflictText warns that an unfinished game blocks a new one, showing
// the pending game's metadata so players can tell which game it is. creator is
// the resolved player name of who started it ("" hides the byline part).
func pendingConflictText(g *storage.Game, creator, tz string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚠️ <b>Уже есть незакрытая игра</b> · #%d\n", g.ID)
	if g.Difficulty.String != "" {
		fmt.Fprintf(&b, "🧩 %s", titleCase(g.Difficulty.String))
		if g.Mode.String != "" {
			fmt.Fprintf(&b, " · %s", titleCase(g.Mode.String))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "🕐 Создана: %s", fmtLocalDateTime(g.CreatedAt, loadLoc(tz)))
	if creator != "" {
		fmt.Fprintf(&b, " · %s", esc(creator))
	}
	b.WriteString("\n")
	if g.UsdokuCode.String != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s\n", g.UsdokuCode.String)
	}
	b.WriteString("\nСначала запиши её результат или отмени:")
	return b.String()
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

// standingsMove is one player's position change after a recorded game.
type standingsMove struct {
	Name  string
	Delta int // positive = moved up that many places
}

// standingsMoves diffs two standings snapshots (before → after) into moves.
// New players (absent from before) never count as a move.
func standingsMoves(before, after []storage.Standing) []standingsMove {
	pos := make(map[int64]int, len(before))
	for i, s := range before {
		pos[s.PlayerID] = i
	}
	var out []standingsMove
	for i, s := range after {
		if p, ok := pos[s.PlayerID]; ok && p != i {
			out = append(out, standingsMove{Name: s.Name, Delta: p - i})
		}
	}
	return out
}

// movesLine renders position changes as one compact line, climbers first.
func movesLine(moves []standingsMove) string {
	if len(moves) == 0 {
		return ""
	}
	sorted := make([]standingsMove, len(moves))
	copy(sorted, moves)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Delta > sorted[j].Delta })
	parts := make([]string, 0, len(sorted))
	for _, m := range sorted {
		if m.Delta > 0 {
			parts = append(parts, fmt.Sprintf("↗️ <b>%s</b> +%d", esc(m.Name), m.Delta))
		} else {
			parts = append(parts, fmt.Sprintf("↘️ %s %d", esc(m.Name), m.Delta))
		}
	}
	return "📊 " + strings.Join(parts, " · ")
}

// badgeLabels names each badge emoji for the "new badge" announcement.
var badgeLabels = map[string]string{
	"🏅": "чемпион сезона",
	"🔥": "3 победы подряд",
	"⚡": "решение быстрее 2:00",
	"💪": "10 побед",
	"💯": "50 побед",
	"🎯": "100 игр",
	"📅": "7 дней подряд",
}

// newBadges returns badges present in after but not in before, keeping order.
func newBadges(before, after []string) []string {
	had := make(map[string]bool, len(before))
	for _, b := range before {
		had[b] = true
	}
	var out []string
	for _, b := range after {
		if !had[b] {
			out = append(out, b)
		}
	}
	return out
}

// badgeLine announces freshly earned badges with their labels.
func badgeLine(name string, badges []string) string {
	parts := make([]string, len(badges))
	for i, bd := range badges {
		if label, ok := badgeLabels[bd]; ok {
			parts[i] = bd + " " + label
		} else {
			parts[i] = bd
		}
	}
	return fmt.Sprintf("🎖 <b>%s</b> получает бейдж: %s", esc(name), strings.Join(parts, ", "))
}

// personalBestLine celebrates a new fastest solve at a difficulty.
func personalBestLine(name, difficulty string, secs, prevSecs int) string {
	return fmt.Sprintf("🚀 <b>%s</b>: личный рекорд на %s — <b>%s</b> (было %s)",
		esc(name), titleCase(difficulty), fmtDuration(secs), fmtDuration(prevSecs))
}

// winStreakLine hypes an ongoing win streak.
func winStreakLine(name string, n int) string {
	return fmt.Sprintf("🔥 <b>%s</b>: %d побед подряд!", esc(name), n)
}

// Milestone thresholds. The 100-game and 50-win marks are covered by the 🎯/💯
// badges already, so milestones start above them.
var (
	gamesMilestones  = map[int]bool{250: true, 500: true, 1000: true, 2000: true}
	winsMilestones   = map[int]bool{100: true, 250: true, 500: true, 1000: true}
	leagueMilestones = map[int]bool{500: true, 1000: true, 2500: true, 5000: true}
)

func milestoneGamesLine(name string, n int) string {
	return fmt.Sprintf("🎂 <b>%s</b>: игра №%d в карьере!", esc(name), n)
}

func milestoneWinsLine(name string, n int) string {
	return fmt.Sprintf("🏆 <b>%s</b>: победа №%d в карьере!", esc(name), n)
}

func milestoneLeagueLine(n int) string {
	return fmt.Sprintf("🎉 Сыграна игра №%d в истории лиги!", n)
}

// award*Line renderers build the season-end nomination lines.
func awardWinsLine(name string, wins int) string {
	return fmt.Sprintf("🥇 Больше всех побед: <b>%s</b> (%d)", esc(name), wins)
}

func awardFastestLine(name string, secs int, difficulty string) string {
	return fmt.Sprintf("⚡ Быстрейшее решение: <b>%s</b> — %s (%s)",
		esc(name), fmtDuration(secs), titleCase(difficulty))
}

func awardStreakLine(name string, n int) string {
	return fmt.Sprintf("🔥 Лучшая серия побед: <b>%s</b> ×%d", esc(name), n)
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
		gap := ""
		if i > 0 && rows[0].Points > r.Points {
			gap = fmt.Sprintf("-%d · ", rows[0].Points-r.Points)
		}
		b.WriteString(fmt.Sprintf("%s <b>%s</b> — <b>%d</b>   <i>(%s%d поб · %d игр)</i>\n",
			medal(i+1), esc(r.Name), r.Points, gap, r.Wins, r.Games))
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

func meText(name string, st *storage.PlayerStat, sp *storage.SpeedStat, duelW, duelL int,
	duelSp *storage.SpeedStat, duelStreak domain.Streak, se *storage.Season) string {
	if st.Games == 0 {
		base := fmt.Sprintf("📊 <b>%s</b>\nВ сезоне %d пока нет игр. Сыграй и запиши: /result", esc(name), se.Number)
		if duelW+duelL > 0 {
			base += fmt.Sprintf("\n⚔️ Дуэли: <b>%d–%d</b>", duelW, duelL)
			base += duelDetailLines(duelSp, duelStreak)
		}
		return base
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"📊 <b>%s</b> · сезон %d\nМесто: <b>%d</b>\nОчки: <b>%d</b>/%d %s\nПобед: %d\nИгр: %d",
		esc(name), se.Number, st.Rank, st.Points, se.Target,
		progressBar(st.Points, se.Target), st.Wins, st.Games)
	if sp != nil && sp.Games > 0 {
		fmt.Fprintf(&b, "\n⏱ Ср. время: <b>%s</b> (по %d, medium)", fmtDuration(sp.AvgSecs), sp.Games)
		fmt.Fprintf(&b, "\n⚡ Лучшее: <b>%s</b>", fmtDuration(sp.BestSecs))
	} else {
		b.WriteString("\n⏱ Ср. время: — (нет авто-игр на medium)")
	}
	if duelW+duelL > 0 {
		fmt.Fprintf(&b, "\n⚔️ Дуэли: <b>%d–%d</b>", duelW, duelL)
		b.WriteString(duelDetailLines(duelSp, duelStreak))
	}
	return b.String()
}

// duelDetailLines renders a player's optional duel solve-time and win-streak
// lines for /me (empty string when there's nothing to show).
func duelDetailLines(sp *storage.SpeedStat, s domain.Streak) string {
	var b strings.Builder
	if sp != nil && sp.Games > 0 {
		fmt.Fprintf(&b, "\n⏱ В дуэлях: <b>%s</b> (лучшее <b>%s</b>)",
			fmtDuration(sp.AvgSecs), fmtDuration(sp.BestSecs))
	}
	if s.Best >= 2 {
		fmt.Fprintf(&b, "\n🔥 Серия дуэлей: <b>%d</b> (лучшая <b>%d</b>)", s.Current, s.Best)
	}
	return b.String()
}

// duelResultText renders a finished duel. rows are rank-ordered (winner first);
// winnerWins/loserWins are the pair's head-to-head tally, shown only when h2h.
// elo is the winner's rating right after this duel and eloDelta its change;
// elo <= 0 hides the rating line.
func duelResultText(rows []storage.ResultRow, winnerWins, loserWins int, h2h bool, elo, eloDelta int) string {
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
	if elo > 0 {
		fmt.Fprintf(&b, "\n📈 Рейтинг <b>%s</b>: <b>%d</b> (%+d)", esc(w.Name), elo, eloDelta)
	}
	return b.String()
}

// duelsText renders the /duels leaderboard (all-time duel records) with per-pair
// head-to-head, duel solve-time, current win-streak markers, and the recent log.
// streaks is keyed by player id (from domain.DuelStreaks).
func duelsText(rows []storage.DuelStanding, h2h []storage.H2HPair, speed []storage.SpeedRow,
	streaks map[int64]domain.Streak, recent []storage.DuelMatch, tz string) string {
	if len(rows) == 0 {
		return "⚔️ <b>Дуэли</b>\nЕщё не было дуэлей. Вызови кого-нибудь: /duel"
	}
	loc := loadLoc(tz)
	var b strings.Builder
	b.WriteString("⚔️ <b>Дуэли</b> · рейтинг\n")
	for i, r := range rows {
		pct := 0
		if total := r.Wins + r.Losses; total > 0 {
			pct = r.Wins * 100 / total
		}
		if r.Elo > 0 {
			fmt.Fprintf(&b, "%s <b>%s</b> — <b>%d</b> · %d–%d <i>(%d%%)</i>",
				medal(i+1), esc(r.Name), r.Elo, r.Wins, r.Losses, pct)
		} else {
			fmt.Fprintf(&b, "%s <b>%s</b> — %d–%d <i>(%d%%)</i>",
				medal(i+1), esc(r.Name), r.Wins, r.Losses, pct)
		}
		if s := streaks[r.PlayerID]; s.Current >= 2 {
			fmt.Fprintf(&b, " 🔥%d", s.Current)
		}
		b.WriteByte('\n')
	}
	if len(h2h) > 0 {
		b.WriteString("\n<b>Личные встречи</b>\n")
		for _, p := range h2h {
			fmt.Fprintf(&b, "%s <b>%d–%d</b> %s\n", esc(p.AName), p.AWins, p.BWins, esc(p.BName))
		}
	}
	if len(speed) > 0 {
		b.WriteString("\n<b>⏱ Время в дуэлях</b>\n")
		for _, r := range speed {
			fmt.Fprintf(&b, "%s — <b>%s</b> <i>(лучшее %s · %d)</i>\n",
				esc(r.Name), fmtDuration(r.AvgSecs), fmtDuration(r.BestSecs), r.Games)
		}
	}
	if len(recent) > 0 {
		b.WriteString("\n<b>Последние дуэли</b>\n")
		for _, m := range recent {
			when := fmtLocalDateTime(m.CompletedAt, loc)
			if m.Loser != "" {
				fmt.Fprintf(&b, "%s · <b>%s</b> обыграл %s\n", when, esc(m.Winner), esc(m.Loser))
			} else {
				fmt.Fprintf(&b, "%s · <b>%s</b> (соперник не финишировал)\n", when, esc(m.Winner))
			}
		}
	}
	return b.String()
}

// speedText renders the /speed leaderboard: players ranked by average solve
// time at a difficulty, with a footer for those below the games threshold.
// se == nil means the all-time (career) board.
func speedText(se *storage.Season, difficulty string, ranked, fewer []storage.SpeedRow, minGames int) string {
	var b strings.Builder
	scope := "все сезоны"
	if se != nil {
		scope = fmt.Sprintf("сезон %d", se.Number)
	}
	fmt.Fprintf(&b, "⚡ <b>Самые быстрые</b> · %s · %s\n", titleCase(difficulty), scope)
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

func historyText(games []storage.HistoryGame, tz string) string {
	if len(games) == 0 {
		return "История пуста. Запиши первую игру: /result"
	}
	loc := loadLoc(tz)
	var b strings.Builder
	b.WriteString("📜 <b>Последние игры</b>\n")
	season := -1 // sentinel: first game always opens a season block
	for _, g := range games {
		if g.SeasonNumber != season {
			season = g.SeasonNumber
			fmt.Fprintf(&b, "\n<b>— Сезон %d —</b>\n", season)
		}
		var order []string
		for i, name := range g.Order {
			order = append(order, medal(i+1)+esc(name))
		}
		diff := ""
		if g.Difficulty != "" {
			diff = " · " + titleCase(g.Difficulty)
		}
		when := fmtLocalDateTime(g.CompletedAt, loc)
		b.WriteString(fmt.Sprintf("%s%s: %s\n", when, diff, strings.Join(order, " ")))
	}
	return b.String()
}

// seasonSummaryText renders the /season N view: final (or current) table of one
// season with its dates, winner, and nominations. Zero-game rows are hidden so
// players who joined in later seasons don't clutter old archives.
func seasonSummaryText(se *storage.Season, rows []storage.Standing, games int, first, last, winner string, awards []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🏆 <b>Сезон %d</b>", se.Number)
	if se.Status == "archived" {
		b.WriteString(" · завершён")
	} else {
		fmt.Fprintf(&b, " · идёт, до %d очков", se.Target)
	}
	if first != "" {
		fmt.Fprintf(&b, "\n📅 %s — %s · игр: <b>%d</b>", first, last, games)
	}
	if winner != "" {
		fmt.Fprintf(&b, "\n👑 Победитель: <b>%s</b>", esc(winner))
	}
	b.WriteString("\n\n")
	shown := 0
	for _, r := range rows {
		if r.Games == 0 {
			continue
		}
		shown++
		b.WriteString(fmt.Sprintf("%s <b>%s</b> — <b>%d</b>   <i>(%d поб · %d игр)</i>\n",
			medal(shown), esc(r.Name), r.Points, r.Wins, r.Games))
	}
	if shown == 0 {
		b.WriteString("В этом сезоне не было игр.\n")
	}
	if len(awards) > 0 {
		b.WriteString("\n<b>Номинации</b>\n" + strings.Join(awards, "\n"))
	}
	return b.String()
}

// archiveHint appends the archived-season pointer under the /season view.
func archiveHint(nums []int) string {
	if len(nums) == 0 {
		return ""
	}
	if len(nums) == 1 {
		return fmt.Sprintf("\n\n<i>Архив: /season %d</i>", nums[0])
	}
	return fmt.Sprintf("\n\n<i>Архив: /season %d…%d</i>", nums[0], nums[len(nums)-1])
}

// noSuchSeasonText is the ephemeral reply for an unknown season number.
func noSuchSeasonText(n int, nums []int) string {
	if len(nums) == 0 {
		return fmt.Sprintf("Сезона %d нет. Архивных сезонов пока нет.", n)
	}
	return fmt.Sprintf("Сезона %d нет. В архиве: %d…%d.", n, nums[0], nums[len(nums)-1])
}

func seasonEndText(number int, winner string, points int, awards []string, nextTarget, nextNumber int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎉🏆 <b>Сезон %d завершён!</b>\nПобедитель: <b>%s</b> (%d очков) 👑\n",
		number, esc(winner), points)
	if len(awards) > 0 {
		b.WriteString("\n<b>Номинации сезона</b>\n" + strings.Join(awards, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\nНачат <b>сезон %d</b> — все с нуля. Цель: %d очков. Поехали! 🔥",
		nextNumber, nextTarget)
	return b.String()
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
// eloC/eloT are current ratings and gainC/gainT the winner's potential gain;
// eloC <= 0 hides the stakes block.
func duelChallengeText(challenger string, target storage.Player, difficulty, code string, nickWarn bool, eloC, eloT, gainC, gainT int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ <b>%s</b> вызывает %s на дуэль! · %s\n",
		esc(challenger), mention(target), titleCase(difficulty))
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s\n", code)
	} else {
		fmt.Fprintf(&b, "⚠️ usdoku не ответил, комнаты нет — создайте вручную (%s) "+
			"и пришлите код: <code>/code LOVI</code>\n", usdokuCreateURL)
	}
	if eloC > 0 {
		fmt.Fprintf(&b, "\n📈 <b>%s</b> %d · <b>%s</b> %d\n⚖️ На кону: +%d против +%d",
			esc(challenger), eloC, esc(target.Name), eloT, gainC, gainT)
		b.WriteString("\n")
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
	} else {
		fmt.Fprintf(&b, "⚠️ Комнаты на usdoku нет (сайт не ответил) — создайте вручную (%s) "+
			"и пришлите код: <code>/code LOVI</code>.\n"+
			"Или запишите результат через «📝 Записать результат».", usdokuCreateURL)
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
	} else {
		fmt.Fprintf(&b, "⚠️ usdoku не ответил, комнаты нет — создайте вручную (%s) "+
			"и пришлите код: <code>/code LOVI</code>\n", usdokuCreateURL)
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

// recordsText renders the all-time fastest solve per difficulty plus the
// championship-title tally. Empty only when there is neither.
func recordsText(rows []storage.RecordRow, titles []storage.TitleRow) string {
	if len(rows) == 0 && len(titles) == 0 {
		return "🏆 <b>Рекорды</b>\nПока нет рекордов — сыграйте авто-игру (нужен /setnick), и время попадёт сюда."
	}
	var b strings.Builder
	b.WriteString("🏆 <b>Рекорды</b> · лучшее время\n")
	if len(rows) == 0 {
		b.WriteString("Пока нет рекордов по времени.\n")
	} else {
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
		for _, r := range sorted {
			fmt.Fprintf(&b, "<b>%s</b> — %s · %s\n", titleCase(r.Difficulty), fmtDuration(r.Secs), esc(r.Name))
		}
	}
	if len(titles) > 0 {
		b.WriteString("\n👑 <b>Титулы</b>\n")
		for _, t := range titles {
			fmt.Fprintf(&b, "%s ×%d\n", esc(t.Name), t.Count)
		}
	}
	return b.String()
}

// badgeCollectionText renders the full badge collection appended to /me: an
// "N/total" header, then every badge in catalog order — earned ones with their
// label (🏅 carries a ×N title count), locked ones with a 🔒 and a progress
// hint. Returns "" only when the catalog is empty (defensive).
func badgeCollectionText(statuses []domain.BadgeStatus) string {
	if len(statuses) == 0 {
		return ""
	}
	earned := 0
	for _, s := range statuses {
		if s.Earned {
			earned++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n🏆 <b>Бейджи</b> (%d/%d)", earned, len(statuses))
	for _, s := range statuses {
		name := badgeLabels[s.Emoji]
		switch {
		case s.Earned && s.Emoji == "🏅" && s.Cur > 1:
			fmt.Fprintf(&b, "\n%s %s ×%d", s.Emoji, name, s.Cur)
		case s.Earned:
			fmt.Fprintf(&b, "\n%s %s", s.Emoji, name)
		case s.Time:
			// Speed badge: no cur/target — show best time when there is one.
			b.WriteString("\n🔒 " + s.Emoji + " " + name)
			if s.Cur > 0 {
				fmt.Fprintf(&b, " — лучшее %s", fmtDuration(s.Cur))
			}
		case s.Target > 1:
			fmt.Fprintf(&b, "\n🔒 %s %s — %d/%d", s.Emoji, name, s.Cur, s.Target)
		default:
			// Binary locked (target 1, e.g. 🏅 with no titles).
			fmt.Fprintf(&b, "\n🔒 %s %s", s.Emoji, name)
		}
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

// fmtLocalDateTime renders a UTC DB datetime in loc as "2006-01-02 15:04",
// falling back to the raw string if it can't be parsed.
func fmtLocalDateTime(s string, loc *time.Location) string {
	if t, err := parseDBTime(s); err == nil {
		return t.In(loc).Format("2006-01-02 15:04")
	}
	return s
}

// digestText renders the weekly digest: top-3 standings, the week's fastest
// solve, the longest current win streak, and games played this week. fastest
// and streakName may be empty when there is no data for them.
func digestText(se *storage.Season, top []storage.Standing, fastest *storage.RecordRow, streakName string, streakLen, weekGames int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📅 <b>Итоги недели</b> · Сезон %d\n\n", se.Number)
	for i, s := range top {
		fmt.Fprintf(&b, "%s <b>%s</b> — %d <i>(%d поб)</i>\n", medal(i+1), esc(s.Name), s.Points, s.Wins)
	}
	fmt.Fprintf(&b, "\n🎮 Игр за неделю: <b>%d</b>", weekGames)
	if fastest != nil {
		fmt.Fprintf(&b, "\n⚡ Быстрейшее: <b>%s</b> · %s · %s",
			fmtDuration(fastest.Secs), titleCase(fastest.Difficulty), esc(fastest.Name))
	}
	if streakName != "" && streakLen >= 2 {
		fmt.Fprintf(&b, "\n🔥 Серия побед: <b>%s</b> ×%d", esc(streakName), streakLen)
	}
	return b.String()
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

// ratingLadder renders the eternal rating ladder: rank, name, rating, peak, with
// a 👑 on the current crown and a marker on provisional (calibrating) players.
func ratingLadder(r domain.Ratings, names map[int64]string) string {
	if len(r.Ladder) == 0 {
		return "📊 <b>Рейтинг</b>\n\nПока нет сыгранных игр."
	}
	var sb strings.Builder
	sb.WriteString("📊 <b>Рейтинг</b>")
	for i, p := range r.Ladder {
		crown := ""
		if p.PlayerID == r.Crown {
			crown = " 👑"
		}
		prov := ""
		if p.Provisional {
			prov = " <i>(калибр.)</i>"
		}
		sb.WriteString(fmt.Sprintf("\n%d. <b>%s</b> — %d (пик %d)%s%s",
			i+1, esc(names[p.PlayerID]), p.Rating, p.Peak, crown, prov))
	}
	return sb.String()
}

// ratingDeltaLines renders a game's rating impact as the result-post footer:
// players ordered by delta (biggest gainer first), plus a crown-change line.
func ratingDeltaLines(gr domain.GameRating, names map[int64]string) string {
	type row struct {
		id, d, nr int64
	}
	rows := make([]row, 0, len(gr.Delta))
	for id, d := range gr.Delta {
		rows = append(rows, row{id: id, d: int64(d), nr: int64(gr.NewRating[id])})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].d != rows[j].d {
			return rows[i].d > rows[j].d
		}
		return rows[i].id < rows[j].id
	})
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		sign := ""
		if r.d > 0 {
			sign = "+"
		}
		parts = append(parts, fmt.Sprintf("%s %s%d → %d", esc(names[r.id]), sign, r.d, r.nr))
	}
	out := "\n\n📊 Рейтинг: " + strings.Join(parts, ", ")
	if line := crownChangeLine(gr, names); line != "" {
		out += "\n" + line
	}
	return out
}

// crownChangeLine announces a new #1 after a game (empty when unchanged).
func crownChangeLine(gr domain.GameRating, names map[int64]string) string {
	if gr.CrownAfter == 0 || gr.CrownAfter == gr.CrownBefore {
		return ""
	}
	if gr.CrownBefore == 0 {
		return fmt.Sprintf("👑 %s забирает корону!", esc(names[gr.CrownAfter]))
	}
	return fmt.Sprintf("👑 Корона сменилась: %s свергает %s!",
		esc(names[gr.CrownAfter]), esc(names[gr.CrownBefore]))
}
