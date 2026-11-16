package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
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
	} {
		e.ChatID, e.GameID, e.Kind = game.ChatID, game.ID, evDNFProgress
		if err := st.LogEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	left := b.dnfProgress(game)
	if left[2] != (boardProgress{48, 54}) || left[3] != (boardProgress{9, 54}) || len(left) != 2 {
		t.Fatalf("dnfProgress = %v", left)
	}

	rows := []storage.ResultRow{
		{PlayerID: 1, Rank: 1, Name: "Alice", Points: 3, Duration: 252},
		{PlayerID: 2, Rank: 0, Name: "Bob"},
		{PlayerID: 3, Rank: 0, Name: "Kate"},
	}
	out := resultText(rows, "medium", "hardcore", nil, 0, 0, 0, left)
	if !strings.Contains(out, "▫️ Bob · <i>без финиша · 48 из 54</i>") {
		t.Errorf("most of the board solved must show:\n%s", out)
	}
	// Under half of the board the number would only read as a reproach.
	if !strings.Contains(out, "▫️ Kate · <i>без финиша</i>") {
		t.Errorf("a board barely started keeps the plain row:\n%s", out)
	}

	if err := st.SetFeature(game.ChatID, featDNFProgress, false); err != nil {
		t.Fatal(err)
	}
	if b.dnfProgress(game) != nil {
		t.Errorf("switched off: no progress in the result")
	}
}
