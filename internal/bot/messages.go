package bot

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

const usdokuCreateURL = "https://www.usdoku.com/create"

const helpText = `🧩 <b>Sudoku League</b> - учёт ваших игр в судоку (usdoku.com): очки, места, сезоны, дуэли.

<b>Главное</b>
/play - новая игра, дуэль или позвать всех (кнопки)
/stats - таблица, моё, скорость, дуэли, история, рекорды, актив (вкладки)
/rating - лестница рейтинга (ELO) и корона 👑
/learn - справочник техник решения
/setnick &lt;ник&gt; - ник на usdoku, сразу регистрирует (нужен для авто-учёта)
/players - кто играет и какие у них ники

<b>Ещё</b>
/join [имя] - зарегистрироваться или сменить имя
/result - записать результат вручную, если авто-учёт не сработал
/code &lt;КОД&gt; - привязать вручную созданную usdoku-комнату к игре
/newgame, /duel, /invite - то же, что /play, но командой
/status, /me, /speed, /duels, /history - то же, что вкладки /stats
/season [N] - сводка сезона, в том числе архивного
/weekly - итоги недели сейчас
/export - игры сезона в CSV

<b>Админ</b>
/settings - порог сезона, очки, мин. игроков, напоминания
/removeplayer &lt;имя&gt; - убрать игрока (игры сохранятся)
/backup - скачать базу (владелец бота или админ группы); восстановить: файл с подписью /restore

💡 Дайте боту право удалять сообщения: служебные ответы тогда исчезают сами.`

const anonMsg = "🙈 Похоже, ты пишешь анонимно (от имени группы). Отключи «Оставаться анонимным» в правах админа или напиши от своего аккаунта — тогда я тебя привяжу."

const welcomeText = `👋 <b>Привет!</b> Я веду учёт ваших игр в судоку (usdoku.com): очки, места, сезоны.

<b>С чего начать:</b>
1. Каждый: /setnick ТвойНикНаUsdoku (сразу регистрирует)
2. /play, а я создам комнату и сам запишу итог

💡 Дайте мне право удалять сообщения, тогда служебный шум исчезнет сам.

Все команды: /help. Кнопки ниже 👇`

// tierTitle is the RU label of a learn tier.
func tierTitle(t domain.Tier) string {
	switch t {
	case domain.TierBasic:
		return "🟢 Базовые"
	case domain.TierMid:
		return "🔵 Средние"
	case domain.TierAdv:
		return "🔴 Продвинутые"
	}
	return string(t)
}

func learnRootMsg() string {
	return "📚 <b>Техники судоку</b>\n\nСправочник приёмов решения: от одиночек до цепочек. " +
		"Выбери уровень — или жми «Случайная», если просто хочешь узнать что-то новое."
}

func learnTierMsg(t domain.Tier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n\n", esc(tierTitle(t)))
	for _, tech := range domain.TechniquesByTier(t) {
		if tech.Alias == tech.Name {
			fmt.Fprintf(&b, "• <b>%s</b>\n", esc(tech.Name))
			continue
		}
		fmt.Fprintf(&b, "• <b>%s</b> — %s\n", esc(tech.Name), esc(tech.Alias))
	}
	b.WriteString("\nЖми технику, чтобы прочитать разбор.")
	return b.String()
}

func learnTechMsg(t domain.Technique) string {
	var b strings.Builder
	if t.Alias == t.Name {
		fmt.Fprintf(&b, "<b>%s</b>\n", esc(t.Name))
	} else {
		fmt.Fprintf(&b, "<b>%s</b> · <i>%s</i>\n", esc(t.Name), esc(t.Alias))
	}
	fmt.Fprintf(&b, "<i>%s</i>\n\n", esc(tierTitle(t.Tier)))
	b.WriteString(esc(t.Blurb))
	return b.String()
}

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

// notCountedText explains why auto-record refused a game and what can be done.
func notCountedText(min int) string {
	return fmt.Sprintf(
		"🚫 Игра не засчитана: участвовало меньше <b>%d</b> игроков (текущий минимум).\n"+
			"Можно записать вручную, отменить игру или изменить порог: /settings minplayers", min)
}

// staleNudgeText is the «game has no result» reminder. It replaces the game's
// card; pingLine (may be "") is a mention of the creator, used only when there
// is no card to edit and the nudge goes out as a new message.
func staleNudgeText(code, pingLine string) string {
	text := "⏰ <b>Результата нет, запишем?</b>"
	if code != "" {
		text += "\nКомната: https://www.usdoku.com/" + esc(code)
	}
	if pingLine != "" {
		text += "\n" + pingLine
	}
	return text
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
		b.WriteString("\nНикто из знакомых игроков не финишировал. Запишите вручную:")
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
func resultText(rows []storage.ResultRow, difficulty, mode string, leader *storage.Standing,
	target, games, minGames int) string {
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
	// The games counter rides along on every result post: it is the one message
	// everyone reads, so it is where the season's tempo stays visible.
	if games > 0 && minGames > 0 {
		fmt.Fprintf(&b, "\n🎲 Игр в сезоне: <b>%d</b>/%d", games, minGames)
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
	b.WriteString(fmt.Sprintf("🏆 <b>Сезон %d</b> · до %d %s\n\n", se.Number, se.Target, pointsWord(se.Target)))
	if len(rows) == 0 {
		b.WriteString("Пока нет игроков. /join")
		return b.String()
	}
	for i, r := range rows {
		gap := ""
		if i > 0 && rows[0].Points > r.Points {
			gap = fmt.Sprintf("-%d · ", rows[0].Points-r.Points)
		}
		b.WriteString(fmt.Sprintf("%s <b>%s</b> · <b>%d</b>   <i>(%s%d поб · %d %s)</i>\n",
			medal(i+1), esc(r.Name), r.Points, gap, r.Wins, r.Games, gamesWord(r.Games)))
	}
	if leader := rows[0]; leader.Points > 0 {
		b.WriteString(fmt.Sprintf("\n📈 <b>%s</b>: %d/%d %s",
			esc(leader.Name), leader.Points, se.Target, progressBar(leader.Points, se.Target)))
	}
	return b.String()
}

// raceText renders who gained what over the last `days` days: the current
// standings order with each player's points earned in the window and the place
// change against the window start. Empty when no game fell into the window.
func raceText(rows []storage.Standing, gains map[int64]int, games, days int) string {
	if games == 0 || len(rows) == 0 {
		return ""
	}
	// Places at the window start: points minus the window gain, ties keep the
	// current order.
	prev := make([]storage.Standing, len(rows))
	copy(prev, rows)
	sort.SliceStable(prev, func(i, j int) bool {
		return prev[i].Points-gains[prev[i].PlayerID] > prev[j].Points-gains[prev[j].PlayerID]
	})
	before := make(map[int64]int, len(prev))
	for i, r := range prev {
		before[r.PlayerID] = i
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🏁 <b>Гонка за %d %s</b> · %d %s\n",
		days, daysWord(days), games, gamesWord(games)))
	for i, r := range rows {
		move := ""
		switch d := before[r.PlayerID] - i; {
		case d > 0:
			move = fmt.Sprintf("  ↗️%d", d)
		case d < 0:
			move = fmt.Sprintf("  ↘️%d", -d)
		}
		b.WriteString(fmt.Sprintf("%s %s <b>+%d</b>%s\n", medal(i+1), esc(r.Name), gains[r.PlayerID], move))
	}
	return strings.TrimRight(b.String(), "\n")
}

// seasonLastDay returns the season's last playable instant, false when the
// season has no readable deadline. The stored deadline is the midnight that
// opens the day after the season, so the last playable moment is a tick before.
func seasonLastDay(se *storage.Season) (time.Time, bool) {
	if se == nil || !se.Deadline.Valid || se.Deadline.String == "" {
		return time.Time{}, false
	}
	deadline, err := parseDBTime(se.Deadline.String)
	if err != nil {
		return time.Time{}, false
	}
	return deadline.Add(-time.Second), true
}

// seasonGamesLine renders the games floor the calendar exit has to clear, plus
// the pace needed to clear it in time, as one line without a leading newline
// (seasonStatusBlock puts it under the deadline line). firstGame is the
// season's first game date (YYYY-MM-DD, empty when nothing has been played).
func seasonGamesLine(se *storage.Season, games int, firstGame string, now time.Time) string {
	if se == nil || se.MinGames < 1 {
		return ""
	}
	if games >= se.MinGames {
		return fmt.Sprintf("🎲 Игр: <b>%d</b>/%d ✅", games, se.MinGames)
	}
	line := fmt.Sprintf("🎲 Игр: <b>%d</b>/%d %s",
		games, se.MinGames, progressBar(games, se.MinGames))
	var parts []string
	if p, ok := actualPace(games, firstGame, now); ok {
		parts = append(parts, fmt.Sprintf("темп %.1f/день", p))
	}
	if p, ok := requiredPace(se, games, now); ok {
		parts = append(parts, fmt.Sprintf("нужно %.1f/день", p))
	}
	if len(parts) > 0 {
		line += " <i>(" + strings.Join(parts, ", ") + ")</i>"
	}
	return line
}

// seasonStatusBlock is the block a season panel ends with: the deadline line
// and, under it, the games counter. Either half may be missing.
func seasonStatusBlock(se *storage.Season, games int, firstGame string, now time.Time,
	loc *time.Location) string {
	block := seasonDeadlineLine(se, now, loc)
	if g := seasonGamesLine(se, games, firstGame, now); g != "" {
		if block == "" {
			block = "\n"
		}
		block += "\n" + g
	}
	return block
}

// actualPace is games per day since the season's first game. False until a full
// day has passed, when the number would be meaningless.
func actualPace(games int, firstGame string, now time.Time) (float64, bool) {
	first, err := time.Parse("2006-01-02", firstGame)
	if err != nil || games <= 0 {
		return 0, false
	}
	days := now.UTC().Sub(first).Hours() / 24
	if days < 1 {
		return 0, false
	}
	return float64(games) / days, true
}

// requiredPace is games per day needed to reach the floor before the deadline.
func requiredPace(se *storage.Season, games int, now time.Time) (float64, bool) {
	last, ok := seasonLastDay(se)
	if !ok || games >= se.MinGames {
		return 0, false
	}
	days := math.Floor(last.Sub(now.UTC()).Hours()/24) + 1
	if days < 1 {
		return 0, false
	}
	return float64(se.MinGames-games) / days, true
}

// seasonExtendedText announces a deadline that passed on an under-played season:
// what is missing, who leads, and the pace that would finish it. newDeadline is
// the new deadline instant (UTC); the text names the last playable day.
func seasonExtendedText(se *storage.Season, games int, leader *storage.Standing,
	newDeadline, now time.Time, loc *time.Location) string {
	var b strings.Builder
	short := se.MinGames - games
	fmt.Fprintf(&b, "⏳ <b>Сезон %d продлён</b>\n\n", se.Number)
	fmt.Fprintf(&b, "Сыграно <b>%d</b>/%d, до финала не хватает <b>%d</b> %s.\n",
		games, se.MinGames, short, plural(short, "игры", "игр", "игр"))
	if leader != nil && leader.Points > 0 {
		fmt.Fprintf(&b, "Лидер: <b>%s</b>, %d/%d\n", esc(leader.Name), leader.Points, se.Target)
	}
	fmt.Fprintf(&b, "\n📅 Новый финал: %s", fmtRuDate(newDeadline.Add(-time.Second), loc))
	if days := domain.SeasonDaysLeft(newDeadline, now, loc); days > 0 {
		fmt.Fprintf(&b, " (через %d %s)\n⚡ Это %.1f игры в день.",
			days, daysWord(days), float64(short)/float64(days+1))
	}
	return b.String()
}

// seasonDeadlineLine renders when the season ends by the calendar, as a block
// to append to a season panel. Empty when the season has no deadline yet.
func seasonDeadlineLine(se *storage.Season, now time.Time, loc *time.Location) string {
	last, ok := seasonLastDay(se)
	if !ok {
		return ""
	}
	days := domain.SeasonDaysLeft(last.Add(time.Second), now, loc)
	switch {
	case days < 0:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s", fmtRuDate(last, loc))
	case days == 0:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s, <b>сегодня!</b>", fmtRuDate(last, loc))
	case days == 1:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s (завтра)", fmtRuDate(last, loc))
	default:
		return fmt.Sprintf("\n\n📅 Финал сезона: %s (через %d %s)", fmtRuDate(last, loc), days, daysWord(days))
	}
}

// seasonText summarises the active season and the leader's progress.
func seasonText(se *storage.Season, leader *storage.Standing) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🏆 <b>Сезон %d</b>\nЦель: <b>%d</b> %s\nТаблица очков: %s\n\n",
		se.Number, se.Target, pointsWord(se.Target), formatTable(se.PointsTable)))
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
// upsetGain > 0 means the winner entered the duel rated below the loser and the
// win paid that many rating points; it adds the upset line.
func duelResultText(rows []storage.ResultRow, winnerWins, loserWins int, h2h bool, upsetGain int) string {
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
		if len(rows) >= 2 && rows[1].Rank != 0 && rows[1].Duration > 0 {
			delta := rows[1].Duration - w.Duration
			sign := "+"
			if delta < 0 {
				sign, delta = "-", -delta
			}
			fmt.Fprintf(&b, " vs %s (%s%s)", fmtDuration(rows[1].Duration), sign, fmtDuration(delta))
		}
	}
	if h2h && len(rows) >= 2 {
		fmt.Fprintf(&b, "\n\nH2H: <b>%s</b> %d–%d <b>%s</b>",
			esc(rows[0].Name), winnerWins, loserWins, esc(rows[1].Name))
	}
	if upsetGain > 0 {
		fmt.Fprintf(&b, "\n😮 Апсет: фаворит пал, <b>%s</b> берёт +%d", esc(w.Name), upsetGain)
	}
	return b.String()
}

// duelsText renders the /duels leaderboard (all-time duel records) with per-pair
// head-to-head, duel solve-time, current win-streak markers, challenge
// behaviour, and the recent log. streaks is keyed by player id.
func duelsText(rows []storage.DuelStanding, h2h []storage.H2HPair, speed []storage.SpeedRow,
	streaks map[int64]domain.Streak, recent []storage.DuelMatch,
	challenges []storage.ChallengeStat, tz string) string {
	if len(rows) == 0 && len(challenges) == 0 {
		return "⚔️ <b>Дуэли</b>\nЕщё не было дуэлей. Вызови кого-нибудь: /duel"
	}
	loc := loadLoc(tz)
	var b strings.Builder
	if len(rows) > 0 {
		b.WriteString("⚔️ <b>Дуэли</b> · счёт\n")
	} else {
		b.WriteString("⚔️ <b>Дуэли</b>\n")
	}
	for i, r := range rows {
		pct := 0
		if total := r.Wins + r.Losses; total > 0 {
			pct = r.Wins * 100 / total
		}
		fmt.Fprintf(&b, "%s <b>%s</b> · %d-%d <i>(%d%%)</i>",
			medal(i+1), esc(r.Name), r.Wins, r.Losses, pct)
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
	if len(challenges) > 0 {
		b.WriteString("\n<b>🤺 Вызовы</b>\n")
		for _, c := range challenges {
			fmt.Fprintf(&b, "%s — бросил <b>%d</b> · получил <b>%d</b>",
				esc(c.Name), c.Issued, c.Received)
			if c.Declined > 0 {
				fmt.Fprintf(&b, " · отказов <b>%d</b>", c.Declined)
			}
			b.WriteByte('\n')
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
			fmt.Fprintf(&b, "%s <b>%s</b> · %s   <i>(%d %s · ⚡ %s)</i>\n",
				medal(i+1), esc(r.Name), fmtDuration(r.AvgSecs), r.Games, gamesWord(r.Games), fmtDuration(r.BestSecs))
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
	items := make([]string, 0, len(games))
	season := -1 // sentinel: first game always opens a season block
	for _, g := range games {
		var b strings.Builder
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
		items = append(items, b.String())
	}
	// Leave room for the "/history all" hint some callers append.
	return clipItems("📜 <b>Последние игры</b>\n", items, tgMaxText-100)
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
		fmt.Fprintf(&b, " · идёт, до %d %s", se.Target, pointsWord(se.Target))
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
		b.WriteString(fmt.Sprintf("%s <b>%s</b> · <b>%d</b>   <i>(%d поб · %d %s)</i>\n",
			medal(shown), esc(r.Name), r.Points, r.Wins, r.Games, gamesWord(r.Games)))
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
	fmt.Fprintf(&b, "🎉🏆 <b>Сезон %d завершён!</b>\nПобедитель: <b>%s</b> (%d %s) 👑\n",
		number, esc(winner), points, pointsWord(points))
	if len(awards) > 0 {
		b.WriteString("\n<b>Номинации сезона</b>\n" + strings.Join(awards, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\nНачат <b>сезон %d</b>, все с нуля. Цель: %d %s. Поехали! 🔥",
		nextNumber, nextTarget, pointsWord(nextTarget))
	return b.String()
}

// seasonDeadlineEndText announces a season closed by the calendar rather than
// by reaching the target. nextDeadline is the new season's end instant (UTC);
// the text shows the last playable day, i.e. the day before it.
func seasonDeadlineEndText(number int, winner string, points int, awards []string,
	nextNumber, nextTarget int, nextDeadline time.Time, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📅🏆 <b>Сезон %d завершён по календарю!</b>\nПобедитель: <b>%s</b> (%d %s) 👑\n",
		number, esc(winner), points, pointsWord(points))
	if len(awards) > 0 {
		b.WriteString("\n<b>Номинации сезона</b>\n" + strings.Join(awards, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\nНачат <b>сезон %d</b>, все с нуля. Цель: %d %s, финал %s. Поехали! 🔥",
		nextNumber, nextTarget, pointsWord(nextTarget), fmtRuDate(nextDeadline.Add(-time.Second), loc))
	return b.String()
}

// seasonLastWeekText is the one-off post a week before the season deadline:
// the leader, the runner-up and the gap between them, then the games counter
// (and the shortfall while the floor is not met, since the deadline only
// closes a season that reached it). rows is the standings, best first (at
// least one row).
func seasonLastWeekText(number int, rows []storage.Standing, games, minGames int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🏁 <b>Последняя неделя сезона %d</b>: <b>%s</b> %d",
		number, esc(rows[0].Name), rows[0].Points)
	if len(rows) > 1 {
		fmt.Fprintf(&b, ", <b>%s</b> %d", esc(rows[1].Name), rows[1].Points)
		if gap := rows[0].Points - rows[1].Points; gap > 0 {
			fmt.Fprintf(&b, " (-%d)", gap)
		} else {
			b.WriteString(" (вровень)")
		}
	}
	if minGames > 0 {
		fmt.Fprintf(&b, "\n🎲 Игр: <b>%d</b>/%d", games, minGames)
		if short := minGames - games; short > 0 {
			fmt.Fprintf(&b, ", до финала не хватает <b>%d</b> %s", short, plural(short, "игры", "игр", "игр"))
		}
	}
	return b.String()
}

// seasonFinalDayText is the morning post on the deadline day with the current
// top of the table (at most three rows).
func seasonFinalDayText(number int, rows []storage.Standing) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⏳ <b>Сегодня финал сезона %d</b>\n\n", number)
	for i, r := range rows {
		if i == 3 {
			break
		}
		fmt.Fprintf(&b, "%s <b>%s</b>: <b>%d</b>\n", medal(i+1), esc(r.Name), r.Points)
	}
	b.WriteString("\nИграть можно до полуночи: /play")
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

// parseDuelPick splits a cbDuelPick payload
// "<difficulty>:<targetPlayerID>:<ownerTgID>". ownerTgID is the Telegram id of
// whoever opened the picker; 0 for pickers posted before the owner was encoded.
func parseDuelPick(s string) (difficulty string, targetID, ownerTg int64) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return "", 0, 0
	}
	difficulty = parts[0]
	targetID, _ = strconv.ParseInt(parts[1], 10, 64)
	if len(parts) >= 3 {
		ownerTg, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	return difficulty, targetID, ownerTg
}

// duelChallengeText is the posted challenge. code may be empty (manual fallback).
// nickWarn appends a note when a participant has no usdoku nick (no auto-record).
// ratC/ratT are the players' current ratings and gainC/gainT what a win would
// pay each; ratC <= 0 hides the stakes line.
func duelChallengeText(challenger string, target storage.Player, difficulty, code string, nickWarn bool, ratC, ratT, gainC, gainT int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ <b>%s</b> вызывает %s на дуэль! · %s\n",
		esc(challenger), mention(target), titleCase(difficulty))
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s\n", code)
	} else {
		fmt.Fprintf(&b, "⚠️ usdoku не ответил, комнаты нет — создайте вручную (%s) "+
			"и пришлите код: <code>/code LOVI</code>\n", usdokuCreateURL)
	}
	if ratC > 0 {
		fmt.Fprintf(&b, "\n⚖️ Рейтинг: %s %d, %s %d. Победа даст %s +%d, %s +%d\n",
			esc(challenger), ratC, esc(target.Name), ratT,
			esc(challenger), gainC, esc(target.Name), gainT)
	}
	b.WriteString("\nПринимаешь вызов?")
	fmt.Fprintf(&b, "\n⏳ Сгорит через %d мин, если не принять", duelChallengeTTLMinutes)
	if nickWarn {
		b.WriteString("\n\n⚠️ У кого-то не задан /setnick — авто-запись не сработает, запишите результат через /result.")
	}
	return b.String()
}

// duelSentText replaces the opponent picker once the challenge went out.
const duelSentText = "⚔️ Вызов отправлен"

func duelAcceptText(target storage.Player, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔥 Вызов принят: %s. Поехали.\n", mention(target))
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s", code)
	} else {
		fmt.Fprintf(&b, "⚠️ Комнаты на usdoku нет (сайт не ответил) — создайте вручную (%s) "+
			"и пришлите код: <code>/code LOVI</code>.\n"+
			"Или запишите результат через «📝 Записать результат».", usdokuCreateURL)
	}
	return b.String()
}

// duelRevokedText replaces a challenge its author withdrew.
const duelRevokedText = "🗑 Вызов отозван"

// duelExpiredLabel is the inactive button left on an expired challenge.
const duelExpiredLabel = "⌛ Вызов сгорел"

// duelExpiredText announces a challenge that nobody answered in time.
func duelExpiredText(challenger, target string) string {
	return fmt.Sprintf("⌛ Вызов %s → %s сгорел, лига свободна", esc(challenger), esc(target))
}

func duelDeclineText(target storage.Player) string {
	return fmt.Sprintf("❌ Вызов отклонён: %s. Дуэль отменена.", mention(target))
}

// duelAcceptReplyText is the reply under the challenge post that pings the
// challenger: edits do not notify, a fresh reply does.
func duelAcceptReplyText(challenger storage.Player) string {
	return fmt.Sprintf("🔥 Вызов принят, %s! Поехали", mention(challenger))
}

func duelDeclineReplyText(challenger storage.Player) string {
	return fmt.Sprintf("❌ Вызов отклонён, %s", mention(challenger))
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
		return "⏱ <b>Рекорды всех времён</b>\nПока нет рекордов, сыграйте авто-игру (нужен /setnick), и время попадёт сюда."
	}
	var b strings.Builder
	b.WriteString("⏱ <b>Рекорды всех времён</b> · лучшее время\n")
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

// fmtDBTime renders a time as a SQLite datetime('now') string (UTC).
func fmtDBTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// ruMonthsGen are Russian month names in the genitive case ("1 августа").
var ruMonthsGen = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// fmtRuDate renders a UTC instant as "31 августа" in loc.
func fmtRuDate(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	return fmt.Sprintf("%d %s", t.Day(), ruMonthsGen[int(t.Month())-1])
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
		diff := ""
		if fastest.Difficulty != "" {
			diff = titleCase(fastest.Difficulty) + " · "
		}
		fmt.Fprintf(&b, "\n⚡ Быстрейшее: <b>%s</b> · %s%s",
			fmtDuration(fastest.Secs), diff, esc(fastest.Name))
	}
	if streakName != "" && streakLen >= 2 {
		fmt.Fprintf(&b, "\n🔥 Серия побед: <b>%s</b> ×%d", esc(streakName), streakLen)
	}
	return b.String()
}

// digestFinalLine closes the weekly digest with the season countdown and the
// leader's lead, e.g. "📅 До финала 18 дней · Alice +3 (второй: Bob)". Empty
// when the season has no (readable, future) deadline.
func digestFinalLine(se *storage.Season, standings []storage.Standing, now time.Time, loc *time.Location) string {
	if se == nil || !se.Deadline.Valid {
		return ""
	}
	deadline, err := parseDBTime(se.Deadline.String)
	if err != nil {
		return ""
	}
	days := domain.SeasonDaysLeft(deadline, now, loc)
	var line string
	switch {
	case days < 0:
		return ""
	case days == 0:
		line = "📅 Финал сезона сегодня"
	default:
		line = fmt.Sprintf("📅 До финала %d %s", days, daysWord(days))
	}
	if len(standings) >= 2 && standings[0].Points > 0 {
		if gap := standings[0].Points - standings[1].Points; gap > 0 {
			line += fmt.Sprintf(" · <b>%s</b> +%d (второй: %s)",
				esc(standings[0].Name), gap, esc(standings[1].Name))
		} else {
			line += fmt.Sprintf(" · <b>%s</b> и <b>%s</b> поровну",
				esc(standings[0].Name), esc(standings[1].Name))
		}
	}
	return "\n\n" + line
}

// --- small helpers ---

func pointsWord(n int) string { return plural(n, "очко", "очка", "очков") }
func gamesWord(n int) string  { return plural(n, "игра", "игры", "игр") }
func daysWord(n int) string   { return plural(n, "день", "дня", "дней") }
func duelsWord(n int) string  { return plural(n, "дуэль", "дуэли", "дуэлей") }

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

// activityText renders the "who calls the games" tab: rounds started per
// player over the last 30 days (the sort key) and in total, plus how long ago
// they last played, counted in chat-local calendar days.
func activityText(rows []storage.InitiativeRow, now time.Time, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("📣 <b>Актив</b> · кто зовёт играть\n\n")
	if len(rows) == 0 {
		b.WriteString("Пока нет игроков. /join")
		return b.String()
	}
	for i, r := range rows {
		fmt.Fprintf(&b, "%s <b>%s</b>: <b>%d</b> %s · <b>%d</b> %s <i>за 30 дн</i>\n",
			medal(i+1), esc(r.Name), r.Games30, gamesWord(r.Games30), r.Duels30, duelsWord(r.Duels30))
		fmt.Fprintf(&b, "   <i>всего: %d · %d · %s</i>\n",
			r.GamesAll, r.DuelsAll, lastPlayedPhrase(r.LastPlayed, now, loc))
	}
	b.WriteString("\n<i>Считаются созданные игры: /play, /duel, /invite, /result. Порядок по последним 30 дням.</i>")
	return b.String()
}

// lastPlayedPhrase renders how long ago a player last played a recorded game,
// by calendar date in loc (a game at 00:30 yesterday is "вчера", not "сегодня").
func lastPlayedPhrase(lastPlayed string, now time.Time, loc *time.Location) string {
	if lastPlayed == "" {
		return "ещё не играл"
	}
	t, err := parseDBTime(lastPlayed)
	if err != nil {
		return "ещё не играл"
	}
	lt, ln := t.In(loc), now.In(loc)
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC) }
	switch days := int(day(ln).Sub(day(lt)) / (24 * time.Hour)); {
	case days <= 0:
		return "играл сегодня"
	case days == 1:
		return "играл вчера"
	default:
		return fmt.Sprintf("не играл %d %s", days, daysWord(days))
	}
}
