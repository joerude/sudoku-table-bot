package bot

import (
	"testing"

	tele "gopkg.in/telebot.v3"
)

func TestWithOptionsDoesNotAliasCallerSlice(t *testing.T) {
	kb := &tele.ReplyMarkup{}
	base := make([]interface{}, 1, 8) // spare capacity: a bare append would write into it
	base[0] = kb
	got := withOptions(base, tele.Silent, tele.NoPreview)
	if len(got) != 3 || got[0] != kb || got[1] != tele.Silent || got[2] != tele.NoPreview {
		t.Fatalf("got %v", got)
	}
	if len(base) != 1 || base[:2][1] != nil {
		t.Errorf("caller slice was modified: %v", base[:2])
	}
	if got := withOptions(nil, tele.Silent); len(got) != 1 || got[0] != tele.Silent {
		t.Errorf("nil base: %v", got)
	}
}
