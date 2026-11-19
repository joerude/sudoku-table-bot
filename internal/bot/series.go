package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// Best-of duels (bo3/bo5). A series is a chain of ordinary duel games
// (games.series_id): each game is scored, rated and counted in the duel stats
// on its own; the series adds its score, the «⚔️ Партия N» button between
// games and its own result. The challenge is answered once, for the whole
// series; later games start by that button and need no accept.

const (
	cbDuelBack   = "duelback" // payload: "<difficulty>:<ownerTgID>", format step back to the opponent list
	cbSeriesNext = "sernext"  // payload: "<gameID>", the series game whose result post carries the button
)

// seriesIdleMinutes: an active series nobody played on for this long is
// abandoned (its games still count as single duels).
const seriesIdleMinutes = 120

// validBestOf are the series formats; 1 is a single duel.
func validBestOf(n int) bool { return n == 1 || n == 3 || n == 5 }

// parseBestOf finds a format argument ("bo3", "bo5", "bo1") among command
// args or origin parts; 0 when there is none (the picker then asks).
func parseBestOf(args []string) int {
	for _, a := range args {
		low := strings.ToLower(a)
		if !strings.HasPrefix(low, "bo") {
			continue
		}
		if n, err := strconv.Atoi(low[2:]); err == nil && validBestOf(n) {
			return n
		}
	}
	return 0
}

// duelPickBestOf reads the optional 4th part of a cbDuelPick payload; 0 = ask.
func duelPickBestOf(s string) int {
	parts := strings.Split(s, ":")
	if len(parts) < 4 {
		return 0
	}
	if n, err := strconv.Atoi(parts[3]); err == nil && validBestOf(n) {
		return n
	}
	return 0
}

// rematchBestOf reads the series format of a cbRematch payload; 1 for a single duel.
func rematchBestOf(s string) int {
	parts := strings.Split(s, ":")
	if len(parts) == 4 {
		if n, err := strconv.Atoi(parts[3]); err == nil && (n == 3 || n == 5) {
			return n
		}
	}
	return 1
}

// seriesFormat: "до 3 побед".
func seriesFormat(bestOf int) string {
	n := storage.WinsNeeded(bestOf)
	return fmt.Sprintf("до %d %s", n, plural(n, "победы", "побед", "побед"))
}

// seriesTitle: "Серия до 3 побед".
func seriesTitle(bestOf int) string { return "Серия " + seriesFormat(bestOf) }

// duelFormatText heads the format step after the opponent was picked.
func duelFormatText(target string) string {
	return fmt.Sprintf("⚔️ Дуэль · соперник: <b>%s</b>\nОдна партия или серия?", esc(target))
}

// duelFormatKeyboard offers a single game or a bo3/bo5 series; each button is
// a cbDuelPick with the format filled in, so it issues the challenge.
func duelFormatKeyboard(difficulty string, targetID, ownerTg int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	pick := func(label string, bestOf int) tele.Btn {
		return m.Data(label, cbDuelPick, fmt.Sprintf("%s:%d:%d:%d", difficulty, targetID, ownerTg, bestOf))
	}
	m.Inline(
		m.Row(pick("Одна партия", 1)),
		m.Row(pick("До 2 побед", 3), pick("До 3 побед", 5)),
		m.Row(m.Data("⬅️ Назад", cbDuelBack, fmt.Sprintf("%s:%d", difficulty, ownerTg))),
	)
	return m
}

// seriesNextButton starts the next game of the series from a result post.
func seriesNextButton(gameID int64, gameNo int) tele.Btn {
	m := &tele.ReplyMarkup{}
	return styled(m.Data(fmt.Sprintf("⚔️ Партия %d", gameNo), cbSeriesNext, gid(gameID)), styleSuccess)
}

// seriesView is what a series game's result post shows about the series, as
// of that game (an old post re-rendered after an edit keeps its own score).
type seriesView struct {
	BestOf       int
	GameNo       int // this game's number among the recorded games
	AName, BName string
	AWins, BWins int      // score after this game
	Winners      []string // winner of each recorded game up to this one, "" = nobody finished
	Done         bool     // this game decided the series
	H2H          bool     // HA/HB are set: finished series between the two, a's side first
	HA, HB       int
}

// seriesScoreLine: leader first and bold, "A 1:1 B" plain while level.
func seriesScoreLine(aName, bName string, aWins, bWins int) string {
	switch {
	case aWins > bWins:
		return fmt.Sprintf("<b>%s</b> %d:%d %s", esc(aName), aWins, bWins, esc(bName))
	case bWins > aWins:
		return fmt.Sprintf("<b>%s</b> %d:%d %s", esc(bName), bWins, aWins, esc(aName))
	}
	return fmt.Sprintf("%s %d:%d %s", esc(aName), aWins, bWins, esc(bName))
}

// seriesResultText renders a series game's result: the game as a duel result
// would show it, then the series score, or the series result once decided.
func seriesResultText(rows []storage.ResultRow, v seriesView, upsetGain int, phrase string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ <b>%s</b> · партия %d\n", seriesTitle(v.BestOf), v.GameNo)
	if writeDuelGame(&b, rows) {
		if phrase != "" {
			b.WriteString("\n\n" + phraseLine(phrase))
		}
		if upsetGain > 0 {
			fmt.Fprintf(&b, "\n😮 Победа над фаворитом: <b>%s</b> +%d", esc(rows[0].Name), upsetGain)
		}
	} else {
		b.WriteString(" Партия не в счёт.")
	}
	b.WriteString("\n\n")
	if !v.Done {
		b.WriteString("Серия: " + seriesScoreLine(v.AName, v.BName, v.AWins, v.BWins))
		return b.String()
	}
	b.WriteString("🏆 Серия: " + seriesScoreLine(v.AName, v.BName, v.AWins, v.BWins))
	names := make([]string, len(v.Winners))
	for i, w := range v.Winners {
		if w == "" {
			names[i] = "без финиша"
		} else {
			names[i] = esc(w)
		}
	}
	b.WriteString("\nПартии: " + strings.Join(names, ", "))
	if v.H2H {
		fmt.Fprintf(&b, "\nЛичный счёт в сериях: %s", seriesH2HLine(v))
	}
	return b.String()
}

// seriesH2HLine puts the series winner first.
func seriesH2HLine(v seriesView) string {
	if v.BWins > v.AWins {
		return fmt.Sprintf("<b>%s</b> %d:%d <b>%s</b>", esc(v.BName), v.HB, v.HA, esc(v.AName))
	}
	return fmt.Sprintf("<b>%s</b> %d:%d <b>%s</b>", esc(v.AName), v.HA, v.HB, esc(v.BName))
}

// seriesNextText is the card of a later series game: a new message, so both
// duelists get pinged.
func seriesNextText(bestOf, gameNo int, difficulty string, a, b storage.Player, aWins, bWins int, code string) string {
	var s strings.Builder
	fmt.Fprintf(&s, "⚔️ <b>%s</b> · партия %d · %s\n", seriesTitle(bestOf), gameNo, titleCase(difficulty))
	fmt.Fprintf(&s, "%s <b>%d:%d</b> %s\n", mention(a), aWins, bWins, mention(b))
	if code != "" {
		fmt.Fprintf(&s, "Комната: https://www.usdoku.com/%s", code)
	} else {
		fmt.Fprintf(&s, "⚠️ Комнаты на usdoku нет (сайт не ответил). Создайте вручную (%s) "+
			"и пришлите код: <code>/code LOVI</code>.\n"+
			"Или запишите результат через «📝 Записать результат».", usdokuCreateURL)
	}
	return s.String()
}

// seriesAbandonedText closes a series nobody played on.
func seriesAbandonedText(bestOf int, aName, bName string, aWins, bWins int) string {
	return fmt.Sprintf("⌛ %s не доиграна: %s", seriesTitle(bestOf), seriesScoreLine(aName, bName, aWins, bWins))
}

// seriesAcceptLine is added to the accepted challenge of a series; "" for a single duel.
func (b *Bot) seriesAcceptLine(gameID int64) string {
	se, err := b.st.SeriesOfGame(gameID)
	if err != nil || se == nil {
		return ""
	}
	return fmt.Sprintf("\n%s · партия 1", seriesTitle(se.BestOf))
}

// seriesResult renders a series game's result; false for a single duel, a
// series that was abandoned or cancelled, or a game no longer in it.
func (b *Bot) seriesResult(game *storage.Game, rows []storage.ResultRow, upset int, phrase string) (string, bool) {
	se, err := b.st.SeriesOfGame(game.ID)
	if err != nil || se == nil {
		if err != nil {
			log.Printf("seriesResult.series: %v", err)
		}
		return "", false
	}
	se, games, err := b.st.SettleSeries(se.ID)
	if err != nil || se == nil || (se.Status != "active" && se.Status != "done") {
		if err != nil {
			log.Printf("seriesResult.settle: %v", err)
		}
		return "", false
	}
	names, err := b.st.PlayerNames(game.ChatID)
	if err != nil {
		log.Printf("seriesResult.names: %v", err)
		return "", false
	}
	v, ok := buildSeriesView(se, games, game.ID, names)
	if !ok {
		return "", false
	}
	if v.Done {
		winner, loser := se.AID, se.BID
		if v.BWins > v.AWins {
			winner, loser = se.BID, se.AID
		}
		if w, l, herr := b.st.SeriesHeadToHead(game.ChatID, winner, loser); herr != nil {
			log.Printf("seriesResult.h2h: %v", herr)
		} else {
			v.H2H = true
			v.HA, v.HB = w, l
			if winner == se.BID {
				v.HA, v.HB = l, w
			}
		}
	}
	return seriesResultText(rows, v, upset, phrase), true
}

// buildSeriesView takes the series as of gameID: the recorded games up to and
// including it. false when gameID is not a recorded game of the series.
func buildSeriesView(se *storage.Series, games []storage.SeriesGame, gameID int64, names map[int64]string) (seriesView, bool) {
	v := seriesView{BestOf: se.BestOf, AName: names[se.AID], BName: names[se.BID]}
	found := false
	for _, g := range games {
		if g.Status != "completed" {
			continue
		}
		v.GameNo++
		switch g.WinnerID {
		case se.AID:
			v.AWins++
		case se.BID:
			v.BWins++
		}
		v.Winners = append(v.Winners, names[g.WinnerID])
		if g.GameID == gameID {
			found = true
			break
		}
	}
	if !found {
		return seriesView{}, false
	}
	need := storage.WinsNeeded(se.BestOf)
	v.Done = v.AWins >= need || v.BWins >= need
	return v, true
}

// seriesResultKB is the result keyboard of a series game; nil for a single
// duel. While the series runs, the latest recorded game carries «⚔️ Партия N»;
// the button stays after a press, so a deleted or burnt game can be created
// again from it. Once the series is over, the rematch repeats the format.
func (b *Bot) seriesResultKB(gameID int64) *tele.ReplyMarkup {
	se, err := b.st.SeriesOfGame(gameID)
	if err != nil || se == nil {
		return nil
	}
	if se.Status != "active" {
		m := resultKeyboard(gameID)
		if validDifficulty[se.Difficulty] {
			m.InlineKeyboard = append(m.InlineKeyboard,
				[]tele.InlineButton{*rematchButtonBo(se.Difficulty, se.AID, se.BID, se.BestOf).Inline()})
		}
		return m
	}
	games, err := b.st.SeriesGames(se.ID)
	if err != nil {
		log.Printf("seriesResultKB.games: %v", err)
		return resultKeyboard(gameID)
	}
	if lastRecorded(games) != gameID {
		return resultKeyboard(gameID)
	}
	return resultKeyboard(gameID, seriesNextButton(gameID, storage.CompletedCount(games)+1))
}

// lastRecorded is the id of the series' latest recorded game, 0 if none.
func lastRecorded(games []storage.SeriesGame) int64 {
	var id int64
	for _, g := range games {
		if g.Status == "completed" {
			id = g.GameID
		}
	}
	return id
}

// onDuelBack returns the format step to the opponent list.
func (b *Bot) onDuelBack(c tele.Context) error {
	parts := strings.Split(c.Data(), ":")
	if len(parts) != 2 || !validDifficulty[parts[0]] {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял выбор"})
	}
	owner, _ := strconv.ParseInt(parts[1], 10, 64)
	me := realSender(c)
	if me == nil || (owner != 0 && me.ID != owner) {
		return c.Respond(&tele.CallbackResponse{Text: "Это не твой выбор"})
	}
	caller, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		return b.fail(c, "onDuelBack.caller", err)
	}
	if caller == nil {
		return c.Respond(&tele.CallbackResponse{Text: guardJoinFirst})
	}
	others, err := b.opponents(c.Chat().ID, caller.ID)
	if err != nil {
		return b.fail(c, "onDuelBack.players", err)
	}
	_ = c.Respond()
	return c.Edit(duelPickPrompt, duelPickKeyboard(parts[0], others, me.ID))
}

// onSeriesNext starts the next game of a running series from the result post
// of its latest game. Either duelist may press; the other one is pinged by the
// new card. The game is accepted from the start (the series was accepted once).
func (b *Bot) onSeriesNext(c tele.Context) error {
	prevID := parseID(c.Data())
	se, err := b.st.SeriesOfGame(prevID)
	if err != nil {
		return b.fail(c, "onSeriesNext.series", err)
	}
	if se == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Серия не найдена"})
	}
	se, games, err := b.st.SettleSeries(se.ID)
	if err != nil {
		return b.fail(c, "onSeriesNext.settle", err)
	}
	refresh := func(toast string) error {
		if err := c.Edit(b.resultKB(prevID)); err != nil {
			log.Printf("onSeriesNext.refresh: %v", err)
		}
		return c.Respond(&tele.CallbackResponse{Text: toast})
	}
	if se.Status != "active" {
		return refresh("Серия уже закрыта")
	}
	me := realSender(c)
	if me == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
	}
	presser, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		return b.fail(c, "onSeriesNext.presser", err)
	}
	if presser == nil || (presser.ID != se.AID && presser.ID != se.BID) {
		return c.Respond(&tele.CallbackResponse{Text: "Партию начинают только участники серии"})
	}
	if lastRecorded(games) != prevID { // an older post: its button is stale
		return refresh("Следующая партия под последним результатом")
	}
	if pending, perr := b.st.SeriesPendingGame(se.ID); perr != nil {
		return b.fail(c, "onSeriesNext.pending", perr)
	} else if pending != nil {
		return c.Respond(&tele.CallbackResponse{Text: "Партия уже создана"})
	}
	otherID := se.AID
	if presser.ID == se.AID {
		otherID = se.BID
	}
	other, err := b.st.PlayerByID(otherID)
	if err != nil {
		return b.fail(c, "onSeriesNext.other", err)
	}
	if other == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Игрок не найден"})
	}
	room, err := b.createGameRoomAt(c, se.Difficulty, "hardcore", "", placeNew)
	if err != nil {
		return b.fail(c, "onSeriesNext.room", err)
	}
	if room == nil {
		_ = c.Respond()
		return nil // a guard replied (e.g. another game is pending)
	}
	if err := b.st.SetDuelTarget(room.gameID, other.ID); err != nil {
		return b.fail(c, "onSeriesNext.target", err)
	}
	if err := b.st.SetGameSeries(room.gameID, se.ID); err != nil {
		return b.fail(c, "onSeriesNext.series", err)
	}
	if _, err := b.st.AcceptDuel(room.gameID); err != nil {
		log.Printf("onSeriesNext.accept: %v", err) // only the expiry ticker cares
	}
	_ = c.Respond()

	a, bp := presser, other
	if presser.ID != se.AID {
		a, bp = other, presser
	}
	aWins, bWins := storage.SeriesScore(games, se.AID, se.BID)
	text := seriesNextText(se.BestOf, storage.CompletedCount(games)+1, se.Difficulty, *a, *bp, aWins, bWins, room.code)
	chatID := c.Chat().ID
	sent, err := b.tb.Send(c.Chat(), text, recordKeyboard(room.gameID))
	if err != nil {
		return b.fail(c, "onSeriesNext.send", err)
	}
	b.saveCard(room.gameID, chatID, int64(sent.ID), text, "")
	if room.msg != nil {
		if err := b.tb.Delete(room.msg); err != nil {
			log.Printf("onSeriesNext.notice: %v", err)
		}
	}
	log.Printf("⚔️ series %d: game %d created", se.ID, room.gameID)
	return nil
}

// seriesGameBurntLabel is the inactive button left on a burnt series game.
const seriesGameBurntLabel = "⌛ Партия сгорела"

// expireStaleSeriesGames burns later series games nobody started within
// duelChallengeTTLMinutes (only the presser came, or nobody), freeing the
// league's pending slot. A game whose room has the other duelist in it is
// alive and stays. The card turns into a note; «⚔️ Партия N» under the last
// result still works. Called from the reminder ticker.
func (b *Bot) expireStaleSeriesGames() {
	burnt, err := b.st.ExpireStaleSeriesGames(duelChallengeTTLMinutes, func(g storage.StaleSeriesGame) bool {
		return isWatching(g.GameID) && roomBusy(g.GameID)
	})
	if err != nil {
		log.Printf("expireStaleSeriesGames: %v", err)
	}
	for _, g := range burnt {
		log.Printf("series game %d burnt unstarted", g.GameID)
		if !g.PostMsgID.Valid {
			continue
		}
		if err := b.editPost(g.ChatID, g.PostMsgID.Int64, seriesGameBurntText,
			closedDuelKeyboard(g.GameID, seriesGameBurntLabel)); err != nil {
			log.Printf("expireStaleSeriesGames.edit: %v", err)
		}
	}
}

// seriesGameBurntText replaces the card of a burnt series game.
const seriesGameBurntText = "⌛ Партия серии не началась и сгорела, лига свободна. " +
	"Начать снова: «⚔️ Партия» под последним результатом"

// abandonStaleSeries closes series nobody played on for seriesIdleMinutes and
// says so quietly in the chat. Called from the reminder ticker.
func (b *Bot) abandonStaleSeries() {
	closed, err := b.st.AbandonStaleSeries(seriesIdleMinutes)
	if err != nil {
		log.Printf("abandonStaleSeries: %v", err)
	}
	for _, se := range closed {
		games, gerr := b.st.SeriesGames(se.ID)
		if gerr != nil {
			log.Printf("abandonStaleSeries.games: %v", gerr)
			continue
		}
		log.Printf("series %d abandoned", se.ID)
		if storage.CompletedCount(games) == 0 {
			continue // nothing was played: nothing to report
		}
		names, nerr := b.st.PlayerNames(se.ChatID)
		if nerr != nil {
			log.Printf("abandonStaleSeries.names: %v", nerr)
			continue
		}
		aWins, bWins := storage.SeriesScore(games, se.AID, se.BID)
		b.postChat(se.ChatID, seriesAbandonedText(se.BestOf, names[se.AID], names[se.BID], aWins, bWins), tele.Silent)
	}
}
