package bot

import (
	"os"
	"regexp"
	"testing"
)

func TestClassifyText(t *testing.T) {
	cases := []struct {
		text string
		hint textHint
		cmd  string
	}{
		{"/stat", hintUnknown, "stat"},
		{"/Stats please", hintUnknown, "stats"},
		{"/stat@my_bot", hintUnknown, "stat"},
		{"/stat@MY_BOT arg", hintUnknown, "stat"},
		{"/stat@other_bot", hintNone, ""},
		{"  /stat", hintUnknown, "stat"},
		{"@my_bot /start", hintMentioned, ""},
		{"@other_bot /start", hintNone, ""},
		{"глянь https://usdoku.com/play/ABC", hintNone, ""},
		{"1/2 партии", hintNone, ""},
		{"сыграли 3/4", hintNone, ""},
		{"/etc/passwd", hintNone, ""},
		{"привет", hintNone, ""},
		{"", hintNone, ""},
	}
	for _, tc := range cases {
		hint, cmd := classifyText(tc.text, "my_bot")
		if hint != tc.hint || cmd != tc.cmd {
			t.Errorf("classifyText(%q) = (%v, %q), want (%v, %q)", tc.text, hint, cmd, tc.hint, tc.cmd)
		}
	}
}

func TestSuggestCommand(t *testing.T) {
	known := []string{"play", "stats", "rating", "setnick", "learn", "help", "settings", "players"}
	cases := map[string]string{
		"stat":     "stats",
		"Stats":    "stats",
		"hlp":      "help",
		"seting":   "settings",
		"setnik":   "setnick",
		"raiting":  "rating",
		"lern":     "learn",
		"xyz":      "",
		"qwertyui": "",
		"a":        "",
	}
	for typed, want := range cases {
		if got := suggestCommand(typed, known); got != want {
			t.Errorf("suggestCommand(%q) = %q, want %q", typed, got, want)
		}
	}
}

func TestUnknownCommandText(t *testing.T) {
	if got := unknownCommandText("stat", "stats"); got != "Не знаю /stat. Похоже на /stats. Все команды: /help" {
		t.Errorf("with suggestion: %q", got)
	}
	if got := unknownCommandText("xyz", ""); got != "Не знаю /xyz. Все команды: /help" {
		t.Errorf("without suggestion: %q", got)
	}
	if got := unknownCommandText("restore", "settings"); got != restoreHintText {
		t.Errorf("/restore as text: %q", got)
	}
}

// knownCommands must list every command registered in routes(), otherwise a
// real command would be reported as unknown or never suggested.
func TestKnownCommandsMatchRoutes(t *testing.T) {
	src, err := os.ReadFile("bot.go")
	if err != nil {
		t.Fatal(err)
	}
	rx := regexp.MustCompile(`Handle\("/(\w+)"`)
	routed := map[string]bool{}
	for _, m := range rx.FindAllStringSubmatch(string(src), -1) {
		routed[m[1]] = true
	}
	listed := map[string]bool{}
	for _, k := range knownCommands {
		listed[k] = true
	}
	for r := range routed {
		if !listed[r] {
			t.Errorf("route /%s missing from knownCommands", r)
		}
	}
	for k := range listed {
		if !routed[k] {
			t.Errorf("knownCommands lists /%s, but no route handles it", k)
		}
	}
}
