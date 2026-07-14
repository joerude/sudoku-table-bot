package bot

import (
	"reflect"
	"testing"
)

func TestPendingConflictKeyboardPayload(t *testing.T) {
	kb := pendingConflictKeyboard(5, "duel:medium")
	row := kb.InlineKeyboard[0]
	if got := row[0].Data; got != "5" {
		t.Errorf("rec payload = %q, want %q", got, "5")
	}
	if got := row[1].Data; got != "5:duel:medium" {
		t.Errorf("del payload = %q, want %q", got, "5:duel:medium")
	}
	kb = pendingConflictKeyboard(5, "")
	if got := kb.InlineKeyboard[0][1].Data; got != "5" {
		t.Errorf("del payload without origin = %q, want %q", got, "5")
	}
}

func TestConfirmDeleteKeyboardPayload(t *testing.T) {
	kb := confirmDeleteKeyboard(9, "game:hard:original")
	for i, want := range []string{"9:game:hard:original", "9:game:hard:original"} {
		if got := kb.InlineKeyboard[0][i].Data; got != want {
			t.Errorf("button %d payload = %q, want %q", i, got, want)
		}
	}
	kb = confirmDeleteKeyboard(9, "")
	if got := kb.InlineKeyboard[0][0].Data; got != "9" {
		t.Errorf("dely payload without origin = %q, want %q", got, "9")
	}
}

func TestSeasonNumbers(t *testing.T) {
	cases := []struct {
		name     string
		archived []int
		active   int
		want     []int
	}{
		{"archive plus active", []int{1, 2, 3}, 4, []int{1, 2, 3, 4}},
		{"no archive", nil, 1, []int{1}},
		{"active duplicated in archive", []int{1, 2}, 2, []int{1, 2}},
		{"unsorted archive", []int{3, 1, 2}, 4, []int{1, 2, 3, 4}},
	}
	for _, tc := range cases {
		if got := seasonNumbers(tc.archived, tc.active); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: seasonNumbers(%v, %d) = %v, want %v",
				tc.name, tc.archived, tc.active, got, tc.want)
		}
	}
}
