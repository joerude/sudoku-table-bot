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
	cbEdit  = "edit"  // payload: "<gameID>"
	cbDel   = "del"   // payload: "<gameID>" — asks to confirm deletion
	cbDelY  = "dely"  // payload: "<gameID>" — confirmed delete
	cbDelN  = "deln"  // payload: "<gameID>" — cancel delete
	cbUndel = "undel" // payload: "<gameID>" — restore deleted game
	cbRec   = "rec"   // payload: "<gameID>"
)

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
		rows = append(rows, m.Row(
			m.Data("✅ Готово", cbDone, gid(gameID)),
			m.Data("♻️ Сброс", cbReset, gid(gameID)),
		))
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

// pendingConflictKeyboard is shown when a new game is requested while one is open.
func pendingConflictKeyboard(gameID int64) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(
		m.Data("📝 Записать результат", cbRec, gid(gameID)),
		m.Data("🗑 Отменить игру", cbDel, gid(gameID)),
	))
	return m
}
