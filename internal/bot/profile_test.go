package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestParseProfileTab(t *testing.T) {
	if id, ok := parseProfileTab(profileTabData(42)); !ok || id != 42 {
		t.Errorf("round trip: got (%d, %v)", id, ok)
	}
	for _, bad := range []string{"me", "p:", "p:x", "p:-3", "table"} {
		if _, ok := parseProfileTab(bad); ok {
			t.Errorf("parseProfileTab(%q) accepted", bad)
		}
	}
}

func TestWithPhrase(t *testing.T) {
	got := withPhrase("👤 <b>Вова</b> · сезон 7\nМесто: <b>1</b>", "Нагнул <тебя>")
	want := "👤 <b>Вова</b> · сезон 7\n💬 «Нагнул &lt;тебя&gt;»\nМесто: <b>1</b>"
	if got != want {
		t.Errorf("withPhrase:\n got %q\nwant %q", got, want)
	}
	if got := withPhrase("👤 <b>Вова</b>", ""); got != "👤 <b>Вова</b>" {
		t.Errorf("no phrase must leave the text alone, got %q", got)
	}
}

func TestProfileExtraText(t *testing.T) {
	pr := &domain.PlayerRating{Rating: 1124}
	versus := []storage.VersusRow{
		{OpponentName: "Дима", GameWins: 18, GameLosses: 9, DuelWins: 4, DuelLosses: 1},
		{OpponentName: "Саша", GameWins: 12, GameLosses: 11},
	}
	out := profileExtraText(pr, 1, 2, versus)
	for _, want := range []string{
		"📈 Рейтинг: <b>1124</b> · место 1",
		"👑 Сезонов выиграно: <b>2</b>",
		"<b>Личный счёт</b>\nДима · игры 18:9 · дуэли 4:1\nСаша · игры 12:11",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in\n%s", want, out)
		}
	}
	if out := profileExtraText(nil, 0, 0, nil); out != "" {
		t.Errorf("nothing to show, got %q", out)
	}
}

func TestProfileRowsMarkShownPlayer(t *testing.T) {
	players := []storage.Player{{ID: 1, Name: "Вова"}, {ID: 2, Name: "Дима"}, {ID: 3, Name: "Очень длинное имя игрока"}, {ID: 4, Name: "Саша"}}
	rows := profileRows(players, 2)
	if len(rows) != 2 || len(rows[0]) != 3 || len(rows[1]) != 1 {
		t.Fatalf("want rows of 3 and 1, got %d rows", len(rows))
	}
	if rows[0][1].Text != "• Дима" || rows[0][0].Text != "Вова" {
		t.Errorf("shown player must be marked: %q, %q", rows[0][0].Text, rows[0][1].Text)
	}
	if n := len([]rune(rows[0][2].Text)); n > profileNameRunes {
		t.Errorf("long name not clipped: %q", rows[0][2].Text)
	}
}
