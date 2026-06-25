package bot

import (
	"fmt"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// Callback unique ids. Payloads are encoded as plain strings.
const (
	cbPick  = "pick"  // payload: "<gameID>:<playerID>"
	cbDone  = "done"  // payload: "<gameID>"
	cbReset = "reset" // payload: "<gameID>"
	cbEdit   = "edit"   // payload: "<gameID>"
	cbUndo   = "undo"   // payload: "<gameID>" — undo last pick
	cbCancel = "cancel" // payload: "<gameID>" — cancel recording
	cbDel    = "del"    // payload: "<gameID>" — asks to confirm deletion
	cbDelY  = "dely"  // payload: "<gameID>" — confirmed delete
	cbDelN  = "deln"  // payload: "<gameID>" — cancel delete
	cbUndel = "undel" // payload: "<gameID>" — restore deleted game
	cbRec     = "rec"     // payload: "<gameID>"
	cbDoneDNF = "donednf" // payload: "<gameID>" — finalize, rest = DNF (0)

	// Quick-action menu (no payload).
	cbQGame   = "qgame"
	cbQStatus = "qstatus"
	cbQMe     = "qme"

	cbDuelPick    = "duelpick"    // payload: "<difficulty>:<targetPlayerID>"
	cbDuelCancel  = "duelcancel" // no payload — dismiss the opponent picker
	cbAccept      = "accept"     // payload: "<gameID>"
	cbDecline     = "decline"    // payload: "<gameID>"

	cbJoinIn = "joinin" // payload: "<gameID>"

	cbStatsTab = "ststab" // payload: "<tab>" — table|me|speed|duels|history

	cbClaimNick = "claim" // payload: "<gameID>:<usdokuNick>" — bind nick to tapper

	cbPlayGame   = "pgame" // no payload — opens difficulty chooser
	cbPlayDuel   = "pduel" // no payload — routes to duel flow
	cbPlayInvite = "pinv"  // no payload — routes to invite flow
	cbPlayDiff   = "pdiff" // payload: "<difficulty>" — creates a normal game
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

// confirmDeleteKeyboard asks the user to confirm deletion.
func confirmDeleteKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("✅ Да, удалить", cbDelY, gid(gameID)),
		m.Data("↩️ Отмена", cbDelN, gid(gameID)),
	))
	return m
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

// duelPickKeyboard lists candidate opponents; tapping one issues the challenge.
func duelPickKeyboard(difficulty string, players []storage.Player) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, p := range players {
		btns = append(btns, m.Data(p.Name, cbDuelPick, fmt.Sprintf("%s:%d", difficulty, p.ID)))
	}
	var rows []tele.Row
	for i := 0; i < len(btns); i += 2 {
		end := i + 2
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, m.Row(btns[i:end]...))
	}
	rows = append(rows, m.Row(m.Data("✖️ Отмена", cbDuelCancel)))
	m.Inline(rows...)
	return m
}

// duelKeyboard is the Accept/Decline prompt on a duel challenge.
func duelKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("✅ Принять", cbAccept, gid(gameID)),
		m.Data("❌ Отказ", cbDecline, gid(gameID)),
	))
	return m
}

// inviteKeyboard is the RSVP toggle on an /invite post; count is the roster size.
func inviteKeyboard(gameID int64, count int) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data(fmt.Sprintf("✅ Я в деле (%d)", count), cbJoinIn, gid(gameID))))
	return m
}

// pendingConflictKeyboard is shown when a new game is requested while one is open.
func pendingConflictKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("📝 Записать результат", cbRec, gid(gameID)),
		m.Data("🗑 Отменить игру", cbDel, gid(gameID)),
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
	{"records", "🏆 Рекорды"},
}

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
func statsKeyboard(active string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, t := range statsTabs {
		label := t.label
		if t.id == active {
			label = "• " + label
		}
		btns = append(btns, m.Data(label, cbStatsTab, t.id))
	}
	var rows []tele.Row
	for i := 0; i < len(btns); i += 3 {
		end := i + 3
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, m.Row(btns[i:end]...))
	}
	m.Inline(rows...)
	return m
}
