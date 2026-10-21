package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

func roomInfo(status string, players int) *usdoku.GameInfo {
	g := &usdoku.GameInfo{Info: usdoku.Info{Status: status}}
	for i := 0; i < players; i++ {
		g.Players = append(g.Players, usdoku.Player{Name: "p"})
	}
	return g
}

func TestSuccessorTracker(t *testing.T) {
	var tr successorTracker
	if tr.observe(roomInfo("pending", 0)) || tr.observe(roomInfo("pending", 2)) {
		t.Errorf("a lobby is still worth watching")
	}
	if !tr.observe(roomInfo("running", 1)) {
		t.Errorf("a started round ends the watch")
	}
	if tr.maxPlayers != 2 || !tr.started || tr.detail() != "players=2 started=true" {
		t.Errorf("tracker = %+v, detail %q", tr, tr.detail())
	}
	var empty successorTracker
	if empty.known() {
		t.Errorf("no successful poll: nothing learned, nothing to log")
	}
	empty.observe(roomInfo("pending", 0))
	if !empty.known() || empty.detail() != "players=0 started=false" {
		t.Errorf("one poll of an empty room is a real \"nobody came\": %q", empty.detail())
	}
}

func TestParseSuccessorDetail(t *testing.T) {
	if p, s, ok := parseSuccessorDetail("players=3 started=true"); !ok || p != 3 || !s {
		t.Errorf("parse = %d %v %v", p, s, ok)
	}
	if _, _, ok := parseSuccessorDetail("junk"); ok {
		t.Errorf("junk must not parse")
	}
}

func TestMetricsShowSuccessorRooms(t *testing.T) {
	b, st := titlesChat(t, -7802)
	for i, d := range []string{"players=0 started=false", "players=2 started=true", "players=1 started=false"} {
		st.LogEvent(storage.Event{ChatID: -7802, GameID: int64(i + 1), Kind: evSuccessorRoom, Detail: d})
	}
	v, err := b.chatMetrics(-7802, time.Now().Add(48*time.Hour), 14)
	if err != nil {
		t.Fatal(err)
	}
	if v.Successors != 3 || v.SuccessorJoined != 2 || v.SuccessorStarted != 1 {
		t.Errorf("successors = %d/%d/%d, want 3 watched, 2 joined, 1 started",
			v.Successors, v.SuccessorJoined, v.SuccessorStarted)
	}
	if got := metricsText(v); !strings.Contains(got, "🔗 Комната-продолжение usdoku: заходили после 2 из 3 игр, сыграли 1") {
		t.Errorf("metrics text misses the successor line:\n%s", got)
	}
}
