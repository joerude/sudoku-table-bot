package bot

import (
	"errors"
	"testing"
	"time"

	tele "gopkg.in/telebot.v3"
)

func TestPickEphemeralRoute(t *testing.T) {
	asked := 0
	admin := func(v bool) func() bool {
		return func() bool { asked++; return v }
	}
	cases := []struct {
		name     string
		callback bool
		ephID    int
		admin    bool
		want     ephemeralRoute
		asks     int
	}{
		{"button press", true, 0, false, routeCallback, 0},
		{"ephemeral command", false, 7, false, routeReply, 0},
		{"command, admin bot", false, 0, true, routeAdmin, 1},
		{"command, plain bot", false, 0, false, routeLegacy, 1},
	}
	for _, tc := range cases {
		asked = 0
		if got := pickEphemeralRoute(tc.callback, tc.ephID, admin(tc.admin)); got != tc.want {
			t.Errorf("%s: route = %d, want %d", tc.name, got, tc.want)
		}
		if asked != tc.asks {
			t.Errorf("%s: admin asked %d times, want %d", tc.name, asked, tc.asks)
		}
	}
}

func TestBotAdminCache(t *testing.T) {
	var bc botAdminCache
	calls := 0
	fetch := func(v bool, err error) func() (bool, error) {
		return func() (bool, error) { calls++; return v, err }
	}
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if !bc.get(1, t0, fetch(true, nil)) || calls != 1 {
		t.Fatalf("first get: calls=%d", calls)
	}
	if !bc.get(1, t0.Add(botAdminTTL-time.Second), fetch(false, nil)) || calls != 1 {
		t.Errorf("fresh entry must be served from the cache, calls=%d", calls)
	}
	if bc.get(1, t0.Add(botAdminTTL), fetch(false, nil)) || calls != 2 {
		t.Errorf("stale entry must be refetched, calls=%d", calls)
	}
	if bc.get(2, t0, fetch(true, errors.New("boom"))) {
		t.Error("a failed check means not admin")
	}
	if !bc.get(2, t0, fetch(true, nil)) || calls != 4 {
		t.Errorf("a failed check must not be cached, calls=%d", calls)
	}
}

func TestMarkupOf(t *testing.T) {
	kb := &tele.ReplyMarkup{}
	if got := markupOf([]interface{}{tele.NoPreview, kb}); got != kb {
		t.Errorf("markupOf = %v, want the keyboard", got)
	}
	if got := markupOf([]interface{}{tele.Silent}); got != nil {
		t.Errorf("markupOf without a keyboard = %v", got)
	}
}
