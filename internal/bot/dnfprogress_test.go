package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func TestResultTextShowsDNFProgress(t *testing.T) {
	st := openTempStore(t)
	b := &Bot{st: st}
	game := &storage.Game{ID: 41, ChatID: -7901}
	// A re-record logs again: the newest line of a player wins.
	for _, e := range []storage.Event{
		{PlayerID: 2, Detail: "40/54"},
		{PlayerID: 2, Detail: "48/54"},
		{PlayerID: 3, Detail: "9/54"},
		{PlayerID: 4, Detail: "garbage"},
		{PlayerID: 5, Detail: "45/54/3"},
		{PlayerID: 6, Detail: "50/54/4"},
		{PlayerID: 7, Detail: "1/2/3/4"},
	} {
		e.ChatID, e.GameID, e.Kind = game.ChatID, game.ID, evDNFProgress
		if err := st.LogEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	left := b.dnfProgress(game)
	if left[2] != (boardProgress{Solved: 48, Open: 54}) || left[3] != (boardProgress{Solved: 9, Open: 54}) ||
		left[5] != (boardProgress{Solved: 45, Open: 54, Wrong: 3}) || len(left) != 4 {
		t.Fatalf("dnfProgress = %v", left)
	}

	rows := []storage.ResultRow{
		{PlayerID: 1, Rank: 1, Name: "Alice", Points: 3, Duration: 252},
		{PlayerID: 2, Rank: 0, Name: "Bob"},
		{PlayerID: 3, Rank: 0, Name: "Kate"},
		{PlayerID: 5, Rank: 0, Name: "Dan"},
		{PlayerID: 6, Rank: 0, Name: "Eve"},
	}
	out := resultText(rows, "medium", "hardcore", nil, 0, 0, 0, left, "")
	if !strings.Contains(out, "▫️ <code>Bob   ·</code> <i>без финиша · 48 из 54</i>") {
		t.Errorf("most of the board solved must show:\n%s", out)
	}
	// Under half of the board the number would only read as a reproach.
	if !strings.Contains(out, "▫️ <code>Kate  ·</code> <i>без финиша</i>") {
		t.Errorf("a board barely started keeps the plain row:\n%s", out)
	}

	// Errors ride along only where the progress is shown; a full board says so.
	if !strings.Contains(out, "▫️ <code>Dan   ·</code> <i>без финиша · 45 из 54, 3 с ошибкой</i>") {
		t.Errorf("wrong cells must follow the progress:\n%s", out)
	}
	if !strings.Contains(out, "▫️ <code>Eve   ·</code> <i>без финиша · все клетки заняты, 4 с ошибкой</i>") {
		t.Errorf("a full board with errors must say so:\n%s", out)
	}

	if err := st.SetFeature(game.ChatID, featDNFProgress, false); err != nil {
		t.Fatal(err)
	}
	if b.dnfProgress(game) != nil {
		t.Errorf("switched off: no progress in the result")
	}
}

func TestBoardProgressDetailRoundTrip(t *testing.T) {
	p := boardProgress{Solved: 45, Open: 54, Wrong: 3}
	if d := p.detail(); d != "45/54/3" {
		t.Errorf("detail = %q", d)
	}
	if got, ok := parseBoardProgress(p.detail()); !ok || got != p {
		t.Errorf("round trip = %+v %v", got, ok)
	}
	// An event logged before wrong cells were counted.
	if got, ok := parseBoardProgress("48/54"); !ok || got != (boardProgress{Solved: 48, Open: 54}) {
		t.Errorf("old detail = %+v %v", got, ok)
	}
	for _, bad := range []string{"", "48", "a/b", "1/2/3/4", "48/-5"} {
		if _, ok := parseBoardProgress(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestLogDNFProgressKeepsWrongCells(t *testing.T) {
	st := openTempStore(t)
	chat := int64(-7902)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	pl, _, err := st.RegisterPlayer(chat, 11, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetNick(pl.ID, "bobnick"); err != nil {
		t.Fatal(err)
	}
	b := &Bot{st: st}
	game := &storage.Game{ID: 42, ChatID: chat}
	addReplaySample(game.ID, usdoku.Progress{Open: 54, Solved: map[string]int{"bobnick": 45},
		Wrong: map[string]int{"bobnick": 3}}, time.Now())
	defer dropReplay(game.ID)
	b.logDNFProgress(game, &usdoku.GameInfo{Players: []usdoku.Player{{Name: "bobnick"}}})
	left := b.dnfProgress(game)
	if left[pl.ID] != (boardProgress{Solved: 45, Open: 54, Wrong: 3}) {
		t.Errorf("logged progress = %+v", left)
	}
}
