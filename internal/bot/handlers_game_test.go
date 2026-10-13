package bot

import (
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func TestOriginArgs(t *testing.T) {
	cases := []struct {
		name       string
		parts      []string
		difficulty string
		mode       string
	}{
		{"full", []string{"game", "hard", "original"}, "hard", "original"},
		{"defaults", []string{"duel"}, "medium", "hardcore"},
		{"difficulty only", []string{"duel", "extreme"}, "extreme", "hardcore"},
		{"bad difficulty", []string{"game", "nope", "original"}, "medium", "original"},
		{"bad mode", []string{"game", "easy", "nope"}, "easy", "hardcore"},
	}
	for _, tc := range cases {
		d, m := originArgs(tc.parts)
		if d != tc.difficulty || m != tc.mode {
			t.Errorf("%s: originArgs(%v) = (%q, %q), want (%q, %q)",
				tc.name, tc.parts, d, m, tc.difficulty, tc.mode)
		}
	}
}

func TestGameToast(t *testing.T) {
	cases := []struct {
		name  string
		g     *storage.Game
		want  string
		toast string
	}{
		{"nil", nil, "pending", "Игра не найдена"},
		{"deleted pending", &storage.Game{Status: "pending", Deleted: true}, "pending", "Игра не найдена"},
		{"deleted completed edit", &storage.Game{Status: "completed", Deleted: true}, "completed", "Игра не найдена"},
		{"pending picker ok", &storage.Game{Status: "pending"}, "pending", ""},
		{"completed picker", &storage.Game{Status: "completed"}, "pending", "Игра уже закрыта"},
		{"completed edit ok", &storage.Game{Status: "completed"}, "completed", ""},
		{"pending edit", &storage.Game{Status: "pending"}, "completed", toastAlreadyOpen},
	}
	for _, tc := range cases {
		if got := gameToast(tc.g, tc.want); got != tc.toast {
			t.Errorf("%s: gameToast = %q, want %q", tc.name, got, tc.toast)
		}
	}
}

func TestRoomParams(t *testing.T) {
	cases := []struct {
		name, diff, mode string
		wantD, wantM     string
	}{
		{"both known", "Hard", "ORIGINAL", "hard", "original"},
		{"unknown values ignored", "insane", "chaos", "", ""},
		{"empty", "", "", "", ""},
		{"mode only", "", "hardcore", "", "hardcore"},
	}
	for _, tc := range cases {
		info := &usdoku.GameInfo{Info: usdoku.Info{Difficulty: tc.diff, Mode: tc.mode}}
		d, m := roomParams(info)
		if d != tc.wantD || m != tc.wantM {
			t.Errorf("%s: roomParams = (%q, %q), want (%q, %q)", tc.name, d, m, tc.wantD, tc.wantM)
		}
	}
}

func TestRepeatParams(t *testing.T) {
	cases := []struct{ d, m, wantD, wantM string }{
		{"hard", "original", "hard", "original"},
		{"extreme", "hardcore", "extreme", "hardcore"},
		{"", "", "medium", "hardcore"},
		{"nope", "chaos", "medium", "hardcore"},
	}
	for _, tc := range cases {
		d, m := repeatParams(tc.d, tc.m)
		if d != tc.wantD || m != tc.wantM {
			t.Errorf("repeatParams(%q, %q) = (%q, %q), want (%q, %q)", tc.d, tc.m, d, m, tc.wantD, tc.wantM)
		}
	}
}
