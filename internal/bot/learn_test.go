package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// Telegram hard-caps callback_data at 64 bytes; telebot encodes it as "\f<unique>|<data>".
func TestLearnKeyboardsFitCallbackLimit(t *testing.T) {
	for _, tier := range domain.Tiers() {
		for _, row := range learnTierKeyboard(tier).InlineKeyboard {
			for _, btn := range row {
				if n := len("\f") + len(btn.Unique) + len("|") + len(btn.Data); n > 64 {
					t.Errorf("tier %s: button %q callback data %d bytes > 64", tier, btn.Text, n)
				}
			}
		}
	}
}

func TestLearnTechMsgAndKeyboard(t *testing.T) {
	tech, ok := domain.TechniqueByKey("x-wing")
	if !ok {
		t.Fatal("x-wing missing from catalog")
	}

	msg := learnTechMsg(tech)
	for _, want := range []string{tech.Name, tech.Alias, tech.Blurb} {
		if !strings.Contains(msg, want) {
			t.Errorf("learnTechMsg missing %q", want)
		}
	}

	// x-wing has a wiki link → two URL buttons; a technique without one → a single link.
	rows := learnTechKeyboard(tech).InlineKeyboard
	if got := len(rows[0]); got != 2 {
		t.Errorf("x-wing link row: %d buttons, want 2 (article + wiki)", got)
	}
	plain, _ := domain.TechniqueByKey("swordfish")
	if got := len(learnTechKeyboard(plain).InlineKeyboard[0]); got != 1 {
		t.Errorf("swordfish link row: %d buttons, want 1", got)
	}
}
