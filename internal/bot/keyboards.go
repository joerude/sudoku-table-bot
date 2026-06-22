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
	cbRec   = "rec"   // payload: "<gameID>"

	// Quick-action menu (no payload).
	cbQGame   = "qgame"
	cbQStatus = "qstatus"
	cbQMe     = "qme"

	cbDuelPick = "duelpick" // payload: "<difficulty>:<targetPlayerID>"
	cbAccept   = "accept"   // payload: "<gameID>"
	cbDecline  = "decline"  // payload: "<gameID>"

	cbJoinIn = "joinin" // payload: "<gameID>"
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
