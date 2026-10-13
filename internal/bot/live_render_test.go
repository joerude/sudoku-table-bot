package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func i64p(v int64) *int64 { return &v }

func TestBuildLiveSnapshotResolvesNamesInFinishOrder(t *testing.T) {
	info := &usdoku.GameInfo{
		Info: usdoku.Info{StartedAt: 1000},
		Players: []usdoku.Player{
			{Name: "bob", JoinedAt: 1000, CompletedAt: i64p(1300)},
			{Name: "ann", JoinedAt: 1000, CompletedAt: i64p(1200)},
			{Name: "cat", JoinedAt: 1000},
		},
	}
	snap := buildLiveSnapshot(info, func(n string) string { return strings.ToUpper(n) })
	if len(snap.Finishers) != 2 || snap.Finishers[0].Name != "ANN" || snap.Finishers[1].Name != "BOB" {
		t.Fatalf("finish order wrong: %+v", snap.Finishers)
	}
	if snap.Finishers[0].Secs != 200 {
		t.Errorf("secs = %d, want 200", snap.Finishers[0].Secs)
	}
}

func TestLiveSectionTextEmptyUntilSomeoneFinishes(t *testing.T) {
	if got := liveSectionText(liveSnapshot{}); got != "" {
		t.Errorf("no finishers must render nothing, got %q", got)
	}
}

func TestLiveKeyboardKeepsRSVPOnInviteCard(t *testing.T) {
	inv := liveKeyboard(7, cardKindInvite, false, 2)
	if len(inv.InlineKeyboard) != 1 || !strings.Contains(inv.InlineKeyboard[0][0].Text, "(2)") {
		t.Errorf("invite card must keep the RSVP button with the roster size: %+v", inv.InlineKeyboard)
	}
	plain := liveKeyboard(7, "", false, 0)
	if len(plain.InlineKeyboard) != 1 || plain.InlineKeyboard[0][0].Unique != cbRec {
		t.Errorf("plain card must offer the record button: %+v", plain.InlineKeyboard)
	}
}
