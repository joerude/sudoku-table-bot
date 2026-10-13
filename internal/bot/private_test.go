package bot

import (
	"strings"
	"testing"
)

func TestPrivateTextAllowed(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"/start", true},
		{"/help", true},
		{"/learn", true},
		{"/backup", true},
		{"/BACKUP", true},
		{"/start@my_bot", true},
		{"/join", false},
		{"/setnick Ace", false},
		{"/play", false},
		{"/stats", false},
		{"/settings target 10", false},
		{"/join@my_bot", false},
		{"/Join", false},
		// not a known command: left to onText, which only hints at typos
		{"/stat", true},
		{"привет", true},
		{"1/2", true},
		{"", true}, // documents: /restore is checked in onDocument
	}
	for _, tc := range cases {
		if got := privateTextAllowed(tc.text); got != tc.want {
			t.Errorf("privateTextAllowed(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestPrivateCallbackAllowed(t *testing.T) {
	for _, u := range []string{cbLearnRoot, cbLearnTier, cbLearnTech, cbLearnRand} {
		if !privateCallbackAllowed(u) {
			t.Errorf("learn callback %q must work in private", u)
		}
	}
	for _, u := range []string{cbPick, cbPlayGame, cbQGame, cbStatsTab, cbJoinIn} {
		if privateCallbackAllowed(u) {
			t.Errorf("callback %q must be refused in private", u)
		}
	}
}

func TestPrivateTexts(t *testing.T) {
	if !strings.Contains(privateDeniedText, "группе") {
		t.Errorf("denial must point at the group: %q", privateDeniedText)
	}
	for _, want := range []string{"/learn", "/help", "группов"} {
		if !strings.Contains(privateStartText, want) {
			t.Errorf("privateStartText missing %q", want)
		}
	}
}
