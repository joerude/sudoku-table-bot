package bot

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// /metrics is the habit mechanics' scoreboard for the owner or a group admin:
// the last N days against the N days before them, computed on the live
// database, so the data never has to leave the server.
const (
	metricsDefaultDays = 14
	metricsMaxDays     = 90
	metricsDenied      = "⛔ Метрики доступны только владельцу бота или админу группы."
)

// parseMetricsDays reads the optional window length.
func parseMetricsDays(args []string) (int, bool) {
	if len(args) == 0 {
		return metricsDefaultDays, true
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > metricsMaxDays {
		return 0, false
	}
	return n, true
}

// metricsView is everything one chat's /metrics message shows.
type metricsView struct {
	Days        int
	Cur, Prev   domain.Metrics
	Names       map[int64]string
	Order       []int64 // active players in display order
	Meets       int     // games with a «Буду через…» promise (current window)
	MeetsPlayed int
	Pace        int // pace lines shown (current window)
	// usdoku successor rooms watched after a game, how many somebody entered,
	// and how many got a round started (current window).
	Successors       int
	SuccessorJoined  int
	SuccessorStarted int
	HookNear         int
	HookStake        int
	Off              []string // switched-off mechanics, by key
}

// pct renders hits/base as a whole percent, "н/д" without a base.
func pct(hits, base int) string {
	if base == 0 {
		return "н/д"
	}
	return fmt.Sprintf("%d%%", int(math.Round(100*float64(hits)/float64(base))))
}

// dec1 renders a number with one decimal and a Russian decimal comma.
func dec1(f float64) string {
	return strings.Replace(strconv.FormatFloat(f, 'f', 1, 64), ".", ",", 1)
}

func metricsText(v metricsView) string {
	c, p := v.Cur, v.Prev
	var b strings.Builder
	fmt.Fprintf(&b, "📈 <b>Метрики</b> · %d %s %s, в скобках %d %s до них\n\n",
		v.Days, plural(v.Days, "полный", "полных", "полных"), daysWord(v.Days), v.Days, daysWord(v.Days))
	fmt.Fprintf(&b, "🎮 Игр в неделю: <b>%s</b> (%s)\n", dec1(c.PerWeek), dec1(p.PerWeek))
	fmt.Fprintf(&b, "📅 Дни с игрой: <b>%s</b> (%s)\n", pct(c.DaysWithGame, c.Days), pct(p.DaysWithGame, p.Days))
	fmt.Fprintf(&b, "🔁 Следующая игра за 10 мин: <b>%s</b> (%s)\n", pct(c.Chained, c.ChainBase), pct(p.Chained, p.ChainBase))
	fmt.Fprintf(&b, "    с крючком %s из %d · без %s из %d\n",
		pct(c.ChainHook, c.ChainHookBase), c.ChainHookBase, pct(c.ChainPlain, c.ChainPlainBase), c.ChainPlainBase)
	fmt.Fprintf(&b, "🏁 Доиграли все: <b>%s</b> (%s)\n", pct(c.AllFinished, c.SeasonGames), pct(p.AllFinished, p.SeasonGames))
	var dnf []string
	for _, id := range v.Order {
		cur, prev := c.DNF[id], p.DNF[id]
		if cur[1] == 0 && prev[1] == 0 {
			continue
		}
		dnf = append(dnf, fmt.Sprintf("%s <b>%s</b> (%s)", esc(v.Names[id]), pct(cur[0], cur[1]), pct(prev[0], prev[1])))
	}
	if len(dnf) > 0 {
		b.WriteString("🚪 DNF: " + strings.Join(dnf, " · ") + "\n")
	}
	fmt.Fprintf(&b, "🗑 Пустых комнат в неделю: <b>%s</b> (%s)\n", dec1(c.AbandonedPerWeek), dec1(p.AbandonedPerWeek))
	fmt.Fprintf(&b, "🤝 «Буду через…»: %d, сыграно %d\n", v.Meets, v.MeetsPlayed)
	fmt.Fprintf(&b, "⏰ Напоминаний: %d, игра в течение 2 ч: %d\n", c.Nudges, c.NudgesPlayed)
	fmt.Fprintf(&b, "🏃 Строк темпа: %d · крючков: %d (😤 %d, ⏭ %d)\n",
		v.Pace, v.HookNear+v.HookStake, v.HookNear, v.HookStake)
	if v.Successors == 0 {
		b.WriteString("🔗 Комната-продолжение usdoku: н/д\n")
	} else {
		fmt.Fprintf(&b, "🔗 Комната-продолжение usdoku: заходили после %d из %d %s, сыграли %d\n",
			v.SuccessorJoined, v.Successors, plural(v.Successors, "игры", "игр", "игр"), v.SuccessorStarted)
	}
	off := "ничего"
	if len(v.Off) > 0 {
		labels := make([]string, len(v.Off))
		for i, k := range v.Off {
			labels[i] = k
			if f, ok := featureByKey(k); ok {
				labels[i] = f.Label
			}
		}
		off = esc(strings.Join(labels, ", "))
	}
	b.WriteString("🔕 Выключено: " + off)
	return b.String()
}

// windowMetrics computes one window of a chat's metrics; starts are the chat's
// game starts around it (see domain.MetricsInput.Starts).
func (b *Bot) windowMetrics(chatID int64, from, to time.Time, loc *time.Location, active []int64,
	starts []time.Time) (domain.Metrics, error) {
	f, t := fmtDBTime(from), fmtDBTime(to)
	rows, err := b.st.ResultsBetween(chatID, f, t)
	if err != nil {
		return domain.Metrics{}, err
	}
	hooks, err := b.st.EventsBetween(chatID, evHookShown, f, t)
	if err != nil {
		return domain.Metrics{}, err
	}
	nudgeEvents, err := b.st.EventsBetween(chatID, evNudgeSent, f, t)
	if err != nil {
		return domain.Metrics{}, err
	}
	abandoned, err := b.st.AbandonedRooms(chatID, f, t)
	if err != nil {
		return domain.Metrics{}, err
	}
	hookGames := make(map[int64]bool, len(hooks))
	for _, e := range hooks {
		hookGames[e.GameID] = true
	}
	var nudges []time.Time
	for _, e := range nudgeEvents {
		if at, err := parseDBTime(e.At); err == nil {
			nudges = append(nudges, at)
		}
	}
	return domain.ComputeMetrics(domain.MetricsInput{
		From: from, To: to, Loc: loc, Games: groupMetricGames(rows), Starts: starts, Active: active,
		HookGames: hookGames, Abandoned: abandoned, Nudges: nudges,
	}), nil
}

// groupMetricGames folds flat result rows into games; rows with unreadable
// times are dropped with their game.
func groupMetricGames(rows []storage.ResultRowSince) []domain.MetricGame {
	var out []domain.MetricGame
	var cur int64
	skip := false
	for i, r := range rows {
		if i == 0 || r.GameID != cur {
			cur = r.GameID
			created, err1 := parseDBTime(r.CreatedAt)
			completed, err2 := parseDBTime(r.CompletedAt)
			if skip = err1 != nil || err2 != nil; skip {
				continue
			}
			out = append(out, domain.MetricGame{ID: r.GameID, Created: created, Completed: completed, Duel: r.Duel})
		}
		if skip {
			continue
		}
		last := &out[len(out)-1]
		last.Results = append(last.Results, domain.MetricResult{PlayerID: r.PlayerID, Rank: r.Rank})
	}
	return out
}

// chatMetrics builds a chat's /metrics view: the last `days` whole local days
// (today, still running, is left out) against the same number of days before
// them, so "days with a game" counts calendar days like the baseline did.
// Event-based counts exist only since the mechanics went live, so they are
// shown for the current window alone.
func (b *Bot) chatMetrics(chatID int64, now time.Time, days int) (metricsView, error) {
	v := metricsView{Days: days, Names: map[int64]string{}}
	loc := loadLoc(b.chatTZ(chatID))
	today, _ := localDayBounds(now, loc)
	to := today.In(loc)
	from := to.AddDate(0, 0, -days)
	prevFrom := from.AddDate(0, 0, -days)

	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		return v, err
	}
	active := make([]int64, 0, len(players))
	for _, p := range players {
		active = append(active, p.ID)
		v.Names[p.ID] = p.Name
	}
	v.Order = active
	raw, err := b.st.GameStartsSince(chatID, fmtDBTime(prevFrom))
	if err != nil {
		return v, err
	}
	var starts []time.Time
	for _, s := range raw {
		if t, err := parseDBTime(s); err == nil {
			starts = append(starts, t)
		}
	}
	if v.Cur, err = b.windowMetrics(chatID, from, to, loc, active, starts); err != nil {
		return v, err
	}
	if v.Prev, err = b.windowMetrics(chatID, prevFrom, from, loc, active, starts); err != nil {
		return v, err
	}
	f, t := fmtDBTime(from), fmtDBTime(to)
	if v.Meets, v.MeetsPlayed, err = b.st.MeetOutcomes(chatID, f, t); err != nil {
		return v, err
	}
	hooks, err := b.st.EventsBetween(chatID, evHookShown, f, t)
	if err != nil {
		return v, err
	}
	seen := map[int64]bool{} // a re-rendered result logs its hook again
	for _, e := range hooks {
		if seen[e.GameID] {
			continue
		}
		seen[e.GameID] = true
		if e.Detail == "near" {
			v.HookNear++
		} else {
			v.HookStake++
		}
	}
	pace, err := b.st.EventsBetween(chatID, evPaceShown, f, t)
	if err != nil {
		return v, err
	}
	type line struct{ game, player int64 }
	lines := map[line]bool{}
	for _, e := range pace {
		lines[line{e.GameID, e.PlayerID}] = true
	}
	v.Pace = len(lines)
	succ, err := b.st.EventsBetween(chatID, evSuccessorRoom, f, t)
	if err != nil {
		return v, err
	}
	for _, e := range succ {
		players, started, ok := parseSuccessorDetail(e.Detail)
		if !ok {
			continue
		}
		v.Successors++
		if players > 0 {
			v.SuccessorJoined++
		}
		if started {
			v.SuccessorStarted++
		}
	}
	v.Off, err = b.st.DisabledFeatures(chatID)
	return v, err
}

// onMetrics answers /metrics [days]. In a group the report names each
// player's DNF rate, so it must never land in the chat: it goes as a true
// ephemeral message, else to the asker's private chat, and only a short note
// stays in the group. The owner in private gets one message per chat with games.
func (b *Bot) onMetrics(c tele.Context) error {
	if !b.requireOwnerOrAdmin(c, metricsDenied) {
		return nil
	}
	days, ok := parseMetricsDays(c.Args())
	if !ok {
		return b.ephemeral(c, fmt.Sprintf("Окно в днях, от 1 до %d: /metrics 28", metricsMaxDays))
	}
	if !isPrivate(c) {
		if _, err := b.ensure(c); err != nil {
			return b.fail(c, "onMetrics.ensure", err)
		}
		v, err := b.chatMetrics(c.Chat().ID, time.Now(), days)
		if err != nil {
			return b.fail(c, "onMetrics.group", err)
		}
		if v.Cur.Games == 0 && v.Prev.Games == 0 {
			return b.ephemeral(c, "За это время игр не было.")
		}
		text := metricsText(v)
		if b.sendPrivately(c, text, nil) {
			return nil
		}
		if _, err := b.tb.Send(realSender(c), text); err != nil {
			log.Printf("onMetrics.dm: %v", err)
			return b.ephemeral(c, "Не могу написать тебе в личку: открой чат со мной, нажми «Start» и повтори /metrics.")
		}
		return b.ephemeral(c, "📈 Метрики отправил в личку.")
	}

	chats, err := b.st.AllChats()
	if err != nil {
		return b.fail(c, "onMetrics.chats", err)
	}
	sent := 0
	for _, ch := range chats {
		v, err := b.chatMetrics(ch.ChatID, time.Now(), days)
		if err != nil {
			return b.fail(c, "onMetrics.chat", err)
		}
		if v.Cur.Games == 0 && v.Prev.Games == 0 {
			continue
		}
		text := metricsText(v)
		if len(chats) > 1 {
			text = fmt.Sprintf("💬 Чат %d\n", ch.ChatID) + text
		}
		if err := c.Send(text); err != nil {
			return err
		}
		sent++
	}
	if sent == 0 {
		return c.Send("За это время игр не было.")
	}
	return nil
}
