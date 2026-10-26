package bot

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func lockIDs(ps []storage.Pick) []int64 {
	out := make([]int64, len(ps))
	for i, p := range ps {
		out[i] = p.PlayerID
	}
	return out
}

func eqIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestConfirmedFinishersStopAtUnknownNick(t *testing.T) {
	order := []usdoku.Player{
		{Name: "ann", JoinedAt: 1000, CompletedAt: i64p(1200)},
		{Name: "guest", JoinedAt: 1000, CompletedAt: i64p(1300)},
		{Name: "bob", JoinedAt: 1000, CompletedAt: i64p(1400)},
	}
	ids := map[string]int64{"ann": 1, "bob": 2}
	lookup := func(n string) int64 { return ids[n] }

	got := confirmedFinishers(order, 1000, lookup, nil)
	if !eqIDs(lockIDs(got), []int64{1}) {
		t.Fatalf("an unclaimed nick may be a player without /setnick: places after it are unknown, got %v", lockIDs(got))
	}
	if got[0].DurationSecs != 200 {
		t.Errorf("solve time kept: %+v", got[0])
	}
}

func TestConfirmedFinishersSkipDuelGuests(t *testing.T) {
	order := []usdoku.Player{
		{Name: "carl", JoinedAt: 1000, CompletedAt: i64p(1100)},
		{Name: "bob", JoinedAt: 1000, CompletedAt: i64p(1200)},
	}
	ids := map[string]int64{"ann": 1, "bob": 2, "carl": 3}
	got := confirmedFinishers(order, 1000, func(n string) int64 { return ids[n] }, map[int64]bool{1: true, 2: true})
	if !eqIDs(lockIDs(got), []int64{2}) {
		t.Errorf("a known guest in a duel room is skipped, got %v", lockIDs(got))
	}
}

func TestMergeLockedPutsUsdokuFirst(t *testing.T) {
	locked := []storage.Pick{{PlayerID: 1, DurationSecs: 300}}
	cases := []struct {
		name    string
		picked  []int64
		want    []int64
		changed bool
	}{
		{"empty picker gets the finisher", nil, []int64{1}, true},
		{"agreeing picks stay", []int64{1, 2}, []int64{1, 2}, false},
		{"a wrong winner moves behind", []int64{2}, []int64{1, 2}, true},
		{"usdoku's finisher moves up", []int64{2, 1, 3}, []int64{1, 2, 3}, true},
	}
	for _, c := range cases {
		got, changed := mergeLocked(c.picked, locked)
		if !eqIDs(lockIDs(got), c.want) || changed != c.changed {
			t.Errorf("%s: got %v changed=%v, want %v changed=%v", c.name, lockIDs(got), changed, c.want, c.changed)
		}
		if got[0].DurationSecs != 300 {
			t.Errorf("%s: locked time lost: %+v", c.name, got[0])
		}
	}
}

func TestIsPrefix(t *testing.T) {
	if !isPrefix(nil, []int64{1}) || !isPrefix([]int64{1}, []int64{1, 2}) {
		t.Error("head of the order must match")
	}
	if isPrefix([]int64{2}, []int64{1, 2}) || isPrefix([]int64{1, 2}, []int64{1}) {
		t.Error("a different or longer order is not a prefix")
	}
}

func TestPickerTextMarksUsdokuPlaces(t *testing.T) {
	rows := []storage.ResultRow{{Name: "Ann", Rank: 1}, {Name: "Bob", Rank: 2}}
	got := pickerText(rows, 1)
	if !strings.Contains(got, "Ann ✓") || strings.Contains(got, "Bob ✓") || !strings.Contains(got, "usdoku") {
		t.Errorf("only the usdoku place is marked: %q", got)
	}
	if strings.Contains(pickerText(rows, 0), "✓") {
		t.Error("nothing locked, nothing marked")
	}
}

func kbHas(m *tele.ReplyMarkup, unique string) bool {
	for _, row := range m.InlineKeyboard {
		for _, b := range row {
			if b.Unique == unique {
				return true
			}
		}
	}
	return false
}

func TestPickerKeyboardHidesUndoOverUsdokuPlaces(t *testing.T) {
	rest := []storage.Player{{ID: 2, Name: "Bob"}}
	if m := pickerKeyboard(5, rest, 1, 1); kbHas(m, cbUndo) || kbHas(m, cbReset) || !kbHas(m, cbCancel) {
		t.Errorf("only usdoku places picked: no undo or reset: %+v", m.InlineKeyboard)
	}
	if m := pickerKeyboard(5, rest, 2, 1); !kbHas(m, cbUndo) || !kbHas(m, cbReset) {
		t.Errorf("a manual pick can be undone: %+v", m.InlineKeyboard)
	}
}

func TestDuelPickerAsksForTheWinnerOnly(t *testing.T) {
	duelists := []storage.Player{{ID: 1, Name: "Ann"}, {ID: 2, Name: "Bob"}}
	m := duelPickerKeyboard(5, duelists, 0)
	if !kbHas(m, cbPick) || kbHas(m, cbDone) || kbHas(m, cbDoneDNF) || kbHas(m, cbUndo) {
		t.Errorf("open duel: two winner buttons and cancel: %+v", m.InlineKeyboard)
	}
	if n := len(m.InlineKeyboard[0]); n != 2 {
		t.Errorf("both duelists in one row, got %d", n)
	}
	if !strings.Contains(duelPickerText(nil, 0), "Кто победил") {
		t.Error("open duel asks for the winner")
	}

	m = duelPickerKeyboard(5, duelists[1:], 1)
	if kbHas(m, cbPick) || !kbHas(m, cbDone) {
		t.Errorf("winner known: confirm only: %+v", m.InlineKeyboard)
	}
	got := duelPickerText([]storage.ResultRow{{Name: "Ann", Rank: 1}}, 1)
	if !strings.Contains(got, "<b>Ann</b>") || !strings.Contains(got, "usdoku") {
		t.Errorf("winner from usdoku named: %q", got)
	}
}
