package bot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// Callback unique ids. Payloads are encoded as plain strings.
const (
	cbPick    = "pick"    // payload: "<gameID>:<playerID>"
	cbDone    = "done"    // payload: "<gameID>"
	cbReset   = "reset"   // payload: "<gameID>"
	cbEdit    = "edit"    // payload: "<gameID>"
	cbUndo    = "undo"    // payload: "<gameID>" — undo last pick
	cbCancel  = "cancel"  // payload: "<gameID>" — cancel recording
	cbDel     = "del"     // payload: "<gameID>" — asks to confirm deletion
	cbDelY    = "dely"    // payload: "<gameID>" — confirmed delete
	cbDelN    = "deln"    // payload: "<gameID>" — cancel delete
	cbUndel   = "undel"   // payload: "<gameID>" — restore deleted game
	cbRec     = "rec"     // payload: "<gameID>"
	cbDoneDNF = "donednf" // payload: "<gameID>" — finalize, rest = DNF (0)

	// Quick-action menu (no payload).
	cbQGame   = "qgame"
	cbQStatus = "qstatus"
	cbQMe     = "qme"

	cbDuelPick   = "duelpick"   // payload: "<difficulty>:<targetPlayerID>:<ownerTgID>"
	cbDuelCancel = "duelcancel" // payload: "<ownerTgID>", dismiss the opponent picker
	cbAccept     = "accept"     // payload: "<gameID>"
	cbDecline    = "decline"    // payload: "<gameID>"
	cbRematch    = "rematch"    // payload: "<difficulty>:<playerAID>:<playerBID>", rematch from a duel result
	cbRevoke     = "revoke"     // payload: "<gameID>", challenger withdraws an unanswered challenge

	cbJoinIn = "joinin" // payload: "<gameID>"

	cbStatsTab = "ststab" // payload: "<tab>" (see statsTabs) or "speed_all"

	cbSeasonView = "seasview" // payload: "<number>" (that season's summary) or "archive" (all seasons)

	cbClaimNick = "claim" // payload: "<gameID>:<usdokuNick>" — bind nick to tapper

	cbPlayGame   = "pgame" // no payload — opens difficulty chooser
	cbPlayDuel   = "pduel" // no payload — routes to duel flow
	cbPlayInvite = "pinv"  // no payload — routes to invite flow
	cbPlayDiff   = "pdiff" // payload: "<difficulty>" — creates a normal game

	cbLearnRoot = "lroot" // no payload — back to the tier chooser
	cbLearnTier = "ltier" // payload: "<tier>" — lists that tier's techniques
	cbLearnTech = "ltech" // payload: "<techniqueKey>" — renders one technique
	cbLearnRand = "lrand" // no payload — random technique
)

// quickMenuKeyboard offers one-tap shortcuts for the most common actions.
func quickMenuKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("🆕 Игра", cbQGame),
		m.Data("📊 Таблица", cbQStatus),
		m.Data("👤 Я", cbQMe),
	))
	return m
}

func gid(id int64) string { return fmt.Sprintf("%d", id) }

// pickerKeyboard shows remaining players to tap (in finish order) plus controls.
func pickerKeyboard(gameID int64, remaining []storage.Player, pickedCount int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row

	var btns []tele.Btn
	for _, p := range remaining {
		btns = append(btns, m.Data(p.Name, cbPick, fmt.Sprintf("%d:%d", gameID, p.ID)))
	}
	for i := 0; i < len(btns); i += 2 {
		end := i + 2
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, m.Row(btns[i:end]...))
	}

	if pickedCount > 0 {
		rows = append(rows, m.Row(m.Data("✅ Готово", cbDone, gid(gameID))))
		rows = append(rows, m.Row(m.Data("🏳 Остальные не доиграли (0)", cbDoneDNF, gid(gameID))))
		rows = append(rows, m.Row(
			m.Data("↩️ Назад", cbUndo, gid(gameID)),
			m.Data("♻️ Сброс", cbReset, gid(gameID)),
			m.Data("✖️ Отмена", cbCancel, gid(gameID)),
		))
	} else {
		rows = append(rows, m.Row(m.Data("✖️ Отмена", cbCancel, gid(gameID))))
	}
	m.Inline(rows...)
	return m
}

// resultKeyboard is attached to a finalised game for light-touch corrections.
func resultKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("✏️ Исправить", cbEdit, gid(gameID)),
		m.Data("🗑 Удалить", cbDel, gid(gameID)),
	))
	return m
}

// confirmDeleteKeyboard asks the user to confirm deletion. origin (may be
// empty) rides along so a confirmed delete can resume the blocked command.
func confirmDeleteKeyboard(gameID int64, origin string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	payload := delPayload(gameID, origin)
	m.Inline(m.Row(
		m.Data("✅ Да, удалить", cbDelY, payload),
		m.Data("↩️ Отмена", cbDelN, payload),
	))
	return m
}

// delPayload encodes a delete-chain callback payload: bare game id, or
// "<gameID>:<origin>" when the delete should resume a blocked command.
func delPayload(gameID int64, origin string) string {
	if origin == "" {
		return gid(gameID)
	}
	return gid(gameID) + ":" + origin
}

// restoreKeyboard offers to undo a soft-delete.
func restoreKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("↩️ Вернуть", cbUndel, gid(gameID))))
	return m
}

// recordKeyboard offers a single "record result" button for a pending game.
func recordKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("📝 Записать результат", cbRec, gid(gameID))))
	return m
}

// liveKeyboard is the game card's keyboard: the invite card keeps its RSVP
// button (rsvp = roster size); every other card offers the manual record button.
func liveKeyboard(gameID int64, kind string, hasFinishers bool, rsvp int) *tele.ReplyMarkup {
	if kind == cardKindInvite {
		return inviteKeyboard(gameID, rsvp)
	}
	return recordKeyboard(gameID)
}

// recordAndClaimKeyboard combines the "record result" button with one claim-nick
// button per unrecognised usdoku nick, so the auto-record fallback is a single post.
// Nicks whose payload would exceed Telegram's 64-byte limit are skipped (listed in
// the text body for manual /setnick instead).
func recordAndClaimKeyboard(gameID int64, unknown []string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	rows := []tele.Row{m.Row(m.Data("📝 Записать результат", cbRec, gid(gameID)))}
	for _, n := range unknown {
		data := fmt.Sprintf("%d:%s", gameID, n)
		if len(data) > 64 {
			continue
		}
		rows = append(rows, m.Row(m.Data("Это я: "+n, cbClaimNick, data)))
	}
	m.Inline(rows...)
	return m
}

// duelPickKeyboard lists candidate opponents; tapping one issues the challenge.
// ownerTg (the opener's Telegram id) rides in every payload so only the opener
// can use or dismiss the picker.
func duelPickKeyboard(difficulty string, players []storage.Player, ownerTg int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, p := range players {
		btns = append(btns, m.Data(p.Name, cbDuelPick, fmt.Sprintf("%s:%d:%d", difficulty, p.ID, ownerTg)))
	}
	var rows []tele.Row
	for i := 0; i < len(btns); i += 2 {
		end := i + 2
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, m.Row(btns[i:end]...))
	}
	rows = append(rows, m.Row(m.Data("✖️ Отмена", cbDuelCancel, strconv.FormatInt(ownerTg, 10))))
	m.Inline(rows...)
	return m
}

// duelKeyboard is the prompt on a duel challenge: Accept/Decline for the
// target, Revoke for the challenger (each handler checks who pressed).
func duelKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(
			m.Data("✅ Принять", cbAccept, gid(gameID)),
			m.Data("❌ Отказ", cbDecline, gid(gameID)),
		),
		m.Row(m.Data("🗑 Отозвать", cbRevoke, gid(gameID))),
	)
	return m
}

// rematchButton is the «Реванш» button for a finished duel between players a and
// b (internal player ids). Either duelist may press it; the other becomes the target.
func rematchButton(difficulty string, aID, bID int64) tele.Btn {
	m := &tele.ReplyMarkup{}
	return m.Data("🔁 Реванш", cbRematch, fmt.Sprintf("%s:%d:%d", difficulty, aID, bID))
}

// parseRematch splits a cbRematch payload "<difficulty>:<playerAID>:<playerBID>".
func parseRematch(s string) (difficulty string, aID, bID int64) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 || !validDifficulty[parts[0]] {
		return "", 0, 0
	}
	aID, _ = strconv.ParseInt(parts[1], 10, 64)
	bID, _ = strconv.ParseInt(parts[2], 10, 64)
	if aID == 0 || bID == 0 {
		return "", 0, 0
	}
	return parts[0], aID, bID
}

// duelResultKeyboard is resultKeyboard plus a rematch row when both duelists
// are known. Use it in place of resultKeyboard on a duel's result post.
func duelResultKeyboard(gameID int64, difficulty string, rows []storage.ResultRow) *tele.ReplyMarkup {
	m := resultKeyboard(gameID)
	if len(rows) >= 2 && validDifficulty[difficulty] {
		m.InlineKeyboard = append(m.InlineKeyboard,
			[]tele.InlineButton{*rematchButton(difficulty, rows[0].PlayerID, rows[1].PlayerID).Inline()})
	}
	return m
}

// inviteKeyboard is the RSVP toggle on an /invite post; count is the roster size.
func inviteKeyboard(gameID int64, count int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data(fmt.Sprintf("✅ Я в деле (%d)", count), cbJoinIn, gid(gameID))))
	return m
}

// pendingConflictKeyboard is shown when a new game is requested while one is
// open. origin encodes the blocked command (e.g. "duel:medium") so deleting
// the stale game flows straight back into it.
func pendingConflictKeyboard(gameID int64, origin string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("📝 Записать результат", cbRec, gid(gameID)),
		m.Data("🗑 Отменить игру", cbDel, delPayload(gameID, origin)),
	))
	return m
}

// statsTabs are the /stats dashboard tabs in display order.
var statsTabs = []struct{ id, label string }{
	{"table", "🏆 Таблица"},
	{"me", "👤 Я"},
	{"speed", "⚡ Скорость"},
	{"duels", "⚔️ Дуэли"},
	{"history", "📜 История"},
	{"rating", "📊 Рейтинг"},
	{"activity", "📣 Актив"},
}

// statsTabsPerRow keeps the tab layout even: 7 tabs make rows of 4 and 3.
const statsTabsPerRow = 4

// recentSeasonButtons is how many season buttons sit under the Table tab; the
// rest hide behind the «Архив» button.
const recentSeasonButtons = 4

// playMenuKeyboard is the /play hub: pick a game mode.
func playMenuKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data("🆕 Обычная игра", cbPlayGame)),
		m.Row(
			m.Data("⚔️ Дуэль", cbPlayDuel),
			m.Data("📣 Позвать всех", cbPlayInvite),
		),
	)
	return m
}

// playDiffKeyboard chooses difficulty for a normal game; Medium is prominent.
func playDiffKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data("⚡ Medium (обычная)", cbPlayDiff, "medium")),
		m.Row(
			m.Data("🟢 Easy", cbPlayDiff, "easy"),
			m.Data("🔴 Hard", cbPlayDiff, "hard"),
			m.Data("💀 Extreme", cbPlayDiff, "extreme"),
		),
	)
	return m
}

// learnRootKeyboard is the /learn hub: three tiers plus a random pick.
func learnRootKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(
			m.Data("🟢 Базовые", cbLearnTier, string(domain.TierBasic)),
			m.Data("🔵 Средние", cbLearnTier, string(domain.TierMid)),
			m.Data("🔴 Продвинутые", cbLearnTier, string(domain.TierAdv)),
		),
		m.Row(m.Data("🎲 Случайная", cbLearnRand)),
	)
	return m
}

// learnTierKeyboard lists a tier's techniques two per row, plus a way back.
func learnTierKeyboard(tier domain.Tier) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	list := domain.TechniquesByTier(tier)
	for i := 0; i < len(list); i += 2 {
		row := tele.Row{m.Data(list[i].Name, cbLearnTech, list[i].Key)}
		if i+1 < len(list) {
			row = append(row, m.Data(list[i+1].Name, cbLearnTech, list[i+1].Key))
		}
		rows = append(rows, row)
	}
	rows = append(rows, m.Row(m.Data("⬅️ Назад", cbLearnRoot)))
	m.Inline(rows...)
	return m
}

// learnTechKeyboard links out to the article (and the deep wiki one, if any) and
// returns to the technique's own tier.
func learnTechKeyboard(t domain.Technique) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	links := tele.Row{m.URL("🔗 Разбор с картинками", t.URL)}
	if t.Wiki != "" {
		links = append(links, m.URL("📖 sudokuwiki", t.Wiki))
	}
	m.Inline(
		links,
		m.Row(m.Data("⬅️ Назад", cbLearnTier, string(t.Tier))),
	)
	return m
}

// claimNickKeyboard offers one tap-to-claim button per unrecognised usdoku nick.
// Nicks whose callback_data payload would exceed Telegram's 64-byte hard limit are
// skipped silently; they remain listed in the text body for manual /setnick.
func claimNickKeyboard(gameID int64, nicks []string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	for _, n := range nicks {
		data := fmt.Sprintf("%d:%s", gameID, n)
		if len(data) > 64 { // Telegram callback_data hard limit (bytes)
			continue
		}
		rows = append(rows, m.Row(m.Data("Это я: "+n, cbClaimNick, data)))
	}
	m.Inline(rows...)
	return m
}

// statsKeyboard renders the dashboard tab row; the active tab gets a "•" marker.
// The speed tab carries an extra toggle row: current season <-> all seasons
// (active "speed_all" highlights the speed tab). The table tab carries a season
// row when the chat has archived seasons (seasonNumber = the active season).
func statsKeyboard(active string, archived []int, seasonNumber int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	tabActive := active
	if active == "speed_all" {
		tabActive = "speed"
	}
	var btns []tele.Btn
	for _, t := range statsTabs {
		label := t.label
		if t.id == tabActive {
			label = "• " + label
		}
		btns = append(btns, m.Data(label, cbStatsTab, t.id))
	}
	var rows []tele.Row
	for i := 0; i < len(btns); i += statsTabsPerRow {
		end := min(i+statsTabsPerRow, len(btns))
		rows = append(rows, m.Row(btns[i:end]...))
	}
	switch active {
	case "speed":
		rows = append(rows, m.Row(m.Data("🌍 Все сезоны", cbStatsTab, "speed_all")))
	case "speed_all":
		rows = append(rows, m.Row(m.Data("📅 Текущий сезон", cbStatsTab, "speed")))
	case "table":
		if len(archived) > 0 {
			rows = append(rows, recentSeasonRows(m, archived, seasonNumber)...)
		}
	}
	m.Inline(rows...)
	return m
}

// seasonNumbers merges archived season numbers with the active one into a
// sorted, deduplicated list for the /season hub keyboard.
func seasonNumbers(archived []int, active int) []int {
	out := make([]int, 0, len(archived)+1)
	for _, n := range archived {
		if n != active {
			out = append(out, n)
		}
	}
	out = append(out, active)
	sort.Ints(out)
	return out
}

// seasonButton is one season's button; the active season is marked with ▶.
func seasonButton(m *tele.ReplyMarkup, n, active int) tele.Btn {
	label := fmt.Sprintf("С%d", n)
	if n == active {
		label = "▶ " + label
	}
	return m.Data(label, cbSeasonView, strconv.Itoa(n))
}

// recentSeasonRows is the Table tab's season strip: the latest
// recentSeasonButtons seasons in one row, plus «Архив» when older ones exist.
func recentSeasonRows(m *tele.ReplyMarkup, archived []int, active int) []tele.Row {
	nums := seasonNumbers(archived, active)
	hidden := len(nums) > recentSeasonButtons
	if hidden {
		nums = nums[len(nums)-recentSeasonButtons:]
	}
	btns := make([]tele.Btn, len(nums))
	for i, n := range nums {
		btns[i] = seasonButton(m, n, active)
	}
	rows := []tele.Row{m.Row(btns...)}
	if hidden {
		rows = append(rows, m.Row(m.Data("📚 Архив", cbSeasonView, seasonArchive)))
	}
	return rows
}

// seasonArchive is the cbSeasonView payload that opens the full season list.
const seasonArchive = "archive"

// seasonViewKeyboard sits under a season summary: arrows to the neighbouring
// seasons and a way back to the stats hub, so the summary is not a dead end.
func seasonViewKeyboard(archived []int, active, viewed int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	nums := seasonNumbers(archived, active)
	var arrows tele.Row
	for i, n := range nums {
		if n != viewed {
			continue
		}
		if i > 0 {
			arrows = append(arrows, m.Data(fmt.Sprintf("◀ С%d", nums[i-1]), cbSeasonView, strconv.Itoa(nums[i-1])))
		}
		if i < len(nums)-1 {
			arrows = append(arrows, m.Data(fmt.Sprintf("С%d ▶", nums[i+1]), cbSeasonView, strconv.Itoa(nums[i+1])))
		}
	}
	var rows []tele.Row
	if len(arrows) > 0 {
		rows = append(rows, arrows)
	}
	rows = append(rows, m.Row(m.Data("⬅️ К статистике", cbStatsTab, "table")))
	m.Inline(rows...)
	return m
}

// archiveKeyboard lists every season plus a way back to the stats hub.
func archiveKeyboard(archived []int, active int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	rows := seasonButtonRows(m, archived, active)
	rows = append(rows, m.Row(m.Data("⬅️ К статистике", cbStatsTab, "table")))
	m.Inline(rows...)
	return m
}

// seasonButtonRows renders one button per season in rows of 4; the active
// season is marked with ▶.
func seasonButtonRows(m *tele.ReplyMarkup, archived []int, active int) []tele.Row {
	var rows []tele.Row
	var btns []tele.Btn
	for _, n := range seasonNumbers(archived, active) {
		btns = append(btns, seasonButton(m, n, active))
		if len(btns) == 4 {
			rows = append(rows, m.Row(btns...))
			btns = nil
		}
	}
	if len(btns) > 0 {
		rows = append(rows, m.Row(btns...))
	}
	return rows
}

// seasonsKeyboard is the /season hub keyboard: just the season buttons.
func seasonsKeyboard(archived []int, active int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(seasonButtonRows(m, archived, active)...)
	return m
}
