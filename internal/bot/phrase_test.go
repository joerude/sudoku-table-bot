package bot

import (
	"errors"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"

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
	out := duelResultText(duelNo{}, rows, 3, 1, true, 0, "Нагнул тебя брат")
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
		clear, ok := phraseSubArg(tc.arg, phraseGifWord)
		if clear != tc.clear || ok != tc.ok {
			t.Errorf("phraseSubArg(%q) = (%v, %v), want (%v, %v)", tc.arg, clear, ok, tc.clear, tc.ok)
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
		if got := isPhraseCaption(caption, phraseGifWord); got != want {
			t.Errorf("isPhraseCaption(%q) = %v, want %v", caption, got, want)
		}
	}
}

func TestPhraseAudioRef(t *testing.T) {
	voice := &tele.Message{Voice: &tele.Voice{File: tele.File{FileID: "AwAC"}}}
	mp3 := &tele.Message{Audio: &tele.Audio{File: tele.File{FileID: "CQAC"}}}
	for name, tc := range map[string]struct {
		m    *tele.Message
		want string
	}{
		"voice itself":       {voice, "voice:AwAC"},
		"reply to an mp3":    {&tele.Message{ReplyTo: mp3}, "audio:CQAC"},
		"reply to text":      {&tele.Message{ReplyTo: &tele.Message{Text: "hi"}}, ""},
		"no reply, no sound": {&tele.Message{Text: "/phrase audio"}, ""},
	} {
		got := phraseAudioFile(tc.m)
		if got != tc.want {
			t.Errorf("%s: phraseAudioFile = %q, want %q", name, got, tc.want)
		}
		if tc.want == "" {
			continue
		}
		switch s := phraseAudioSendable(got).(type) {
		case *tele.Voice:
			if !strings.HasPrefix(got, "voice:") || s.FileID != "AwAC" {
				t.Errorf("%s: sendable %T %q", name, s, s.FileID)
			}
		case *tele.Audio:
			if !strings.HasPrefix(got, "audio:") || s.FileID != "CQAC" {
				t.Errorf("%s: sendable %T %q", name, s, s.FileID)
			}
		default:
			t.Errorf("%s: sendable %T", name, s)
		}
	}
	for _, bad := range []string{"", "voice:", "video:x", "nocolon"} {
		if s := phraseAudioSendable(bad); s != nil {
			t.Errorf("phraseAudioSendable(%q) = %T, want nil", bad, s)
		}
	}
}
