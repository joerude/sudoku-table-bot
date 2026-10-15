package bot

import (
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// buttonStyles maps button text to its style across a keyboard.
func buttonStyles(kb *tele.ReplyMarkup) map[string]string {
	out := map[string]string{}
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			out[b.Text] = b.Style
		}
	}
	return out
}

func TestButtonStyles(t *testing.T) {
	rows := []storage.ResultRow{{PlayerID: 1}, {PlayerID: 2}}
	cases := []struct {
		kb   *tele.ReplyMarkup
		want map[string]string
	}{
		{duelKeyboard(1), map[string]string{"✅ Принять": styleSuccess, "❌ Отказ": styleDanger, "🗑 Отозвать": styleDanger}},
		{liveKeyboard(1, "", true, 0), map[string]string{"🏁 Записать сейчас": styleSuccess, "📝 Записать результат": styleSuccess}},
		{quickMenuKeyboard(), map[string]string{"🎮 Играть": styleSuccess, "📊 Таблица": "", "❓ Помощь": ""}},
		{playMenuKeyboard(), map[string]string{"🆕 Обычная игра": styleSuccess, "⚔️ Дуэль": ""}},
		{resultKeyboard(1, againButton(1)), map[string]string{"✏️ Исправить": "", "🗑 Удалить": styleDanger, "🔁 Ещё одну": stylePrimary}},
		{duelResultKeyboard(1, "medium", rows), map[string]string{"🔁 Реванш": stylePrimary}},
		{confirmDeleteKeyboard(1, ""), map[string]string{"✅ Да, удалить": styleDanger, "↩️ Отмена": ""}},
		{notCountedKeyboard(1), map[string]string{"🗑 Отменить": styleDanger}},
		{pendingConflictKeyboard(1, ""), map[string]string{"📝 Записать результат": styleSuccess, "🗑 Отменить игру": styleDanger}},
		{recordKeyboard(1), map[string]string{"📝 Записать результат": styleSuccess}},
	}
	for i, tc := range cases {
		got := buttonStyles(tc.kb)
		for text, want := range tc.want {
			if style, ok := got[text]; !ok || style != want {
				t.Errorf("case %d: %q style = %q (present %v), want %q", i, text, style, ok, want)
			}
		}
	}
}

func TestKeyboardJSONCarriesStyle(t *testing.T) {
	got := mustJSON(t, duelKeyboard(3))
	for _, want := range []string{`"style":"success"`, `"style":"danger"`} {
		if !strings.Contains(got, want) {
			t.Errorf("duel keyboard JSON %s lacks %s", got, want)
		}
	}
	if plain := mustJSON(t, learnRootKeyboard()); strings.Contains(plain, "style") || strings.Contains(plain, "disabled") {
		t.Errorf("an unstyled keyboard must not carry style or disabled: %s", plain)
	}
}

func TestClosedDuelKeyboardIsDisabled(t *testing.T) {
	for _, label := range []string{duelExpiredLabel, duelRevokedText} {
		kb := closedDuelKeyboard(5, label)
		if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
			t.Fatalf("%s: want one button, got %v", label, kb.InlineKeyboard)
		}
		btn := kb.InlineKeyboard[0][0]
		if btn.Text != label || btn.Disabled == nil || btn.Unique != cbAccept || btn.Data != "5" {
			t.Errorf("%s: button = %+v", label, btn)
		}
		if got := mustJSON(t, kb); !strings.Contains(got, `"disabled":{}`) {
			t.Errorf("%s: JSON %s lacks disabled", label, got)
		}
	}
}

func TestStatsKeyboardDisablesActiveTab(t *testing.T) {
	for _, tc := range []struct{ active, tab string }{{"me", "me"}, {"table", "table"}, {"speed_all", "speed"}} {
		kb := statsKeyboard(tc.active, nil, 1)
		tabRows := (len(statsTabs) + statsTabsPerRow - 1) / statsTabsPerRow // the speed toggle row below is no tab
		for _, row := range kb.InlineKeyboard[:tabRows] {
			for _, b := range row {
				if want := b.Data == tc.tab; (b.Disabled != nil) != want {
					t.Errorf("active %s: tab %s disabled = %v, want %v", tc.active, b.Data, b.Disabled != nil, want)
				}
			}
		}
	}
}
