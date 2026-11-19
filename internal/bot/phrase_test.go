package bot

import (
	"errors"
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestNormalizePhrase(t *testing.T) {
	cases := []struct {
		in, want string
		err      error
	}{
		{"  Нагнул   тебя\nбрат ", "Нагнул тебя брат", nil},
		{"«Нагнул тебя брат»", "Нагнул тебя брат", nil},
		{`"Нагнул"`, "Нагнул", nil},
		{"«»", "", errPhraseEmpty},
		{"   ", "", errPhraseEmpty},
		{strings.Repeat("я", phraseMaxRunes), strings.Repeat("я", phraseMaxRunes), nil},
		{strings.Repeat("я", phraseMaxRunes+1), "", errPhraseTooLong},
	}
	for _, tc := range cases {
		got, err := normalizePhrase(tc.in)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("normalizePhrase(%q) = (%q, %v), want (%q, %v)", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestResultTextPhrase(t *testing.T) {
	rows := []storage.ResultRow{{Rank: 1, Name: "Вова", Points: 10}, {Rank: 2, Name: "Дима", Points: 6}}
	out := resultText(rows, "medium", "", nil, 0, 0, 0, nil, "Нагнул <тебя>")
	if !strings.Contains(out, "💬 <b>«Нагнул &lt;тебя&gt;»</b>") {
		t.Errorf("want the escaped bold phrase\ngot: %s", out)
	}
	// Nobody finished: no winner, so no phrase even if one is passed.
	dnf := []storage.ResultRow{{Rank: 0, Name: "Вова"}}
	if out := resultText(dnf, "medium", "", nil, 0, 0, 0, nil, "Нагнул"); strings.Contains(out, "💬") {
		t.Errorf("no winner, want no phrase\ngot: %s", out)
	}
}

func TestDuelResultTextPhrase(t *testing.T) {
	rows := []storage.ResultRow{{Rank: 1, Name: "Вова", Duration: 300}, {Rank: 2, Name: "Дима"}}
	out := duelResultText(rows, 3, 1, true, 0, "Нагнул тебя брат")
	phrase := strings.Index(out, "💬 <b>«Нагнул тебя брат»</b>")
	if phrase < 0 || phrase > strings.Index(out, "Личный счёт") {
		t.Errorf("want the phrase above the head-to-head line\ngot: %s", out)
	}
}

func TestPhraseGifArg(t *testing.T) {
	for _, tc := range []struct {
		arg       string
		clear, ok bool
	}{
		{"gif", false, true},
		{"GIF", false, true},
		{"gif -", true, true},
		{"gif party", false, false}, // a text phrase that starts with "gif"
		{"Нагнул тебя брат", false, false},
		{"", false, false},
	} {
		clear, ok := phraseGifArg(tc.arg)
		if clear != tc.clear || ok != tc.ok {
			t.Errorf("phraseGifArg(%q) = (%v, %v), want (%v, %v)", tc.arg, clear, ok, tc.clear, tc.ok)
		}
	}
}

func TestIsPhraseGifCaption(t *testing.T) {
	for caption, want := range map[string]bool{
		"/phrase gif":            true,
		"/phrase@sudoku_bot gif": true,
		"/phrase":                false,
		"/phrase gif -":          false,
		"смотри gif":             false,
	} {
		if got := isPhraseGifCaption(caption); got != want {
			t.Errorf("isPhraseGifCaption(%q) = %v, want %v", caption, got, want)
		}
	}
}
