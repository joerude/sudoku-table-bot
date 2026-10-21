package bot

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// When a hardcore round ends, usdoku opens a successor room (supersededBy)
// for the same players. The bot does not follow it, so a game played there is
// lost to the league. Before building a mechanic that adopts that room, the
// bot only measures, silently, whether anybody goes there: nothing is posted.
const (
	evSuccessorRoom = "successor_room" // detail: "players=N started=B"
	successorPoll   = time.Minute
	successorWatch  = 10 * time.Minute
)

// successorTracker sums up the polls of one successor room.
type successorTracker struct {
	polls      int // successful polls
	maxPlayers int
	started    bool
}

// known: at least one poll got through, so "nobody came" is a finding and
// not an outage.
func (t successorTracker) known() bool { return t.polls > 0 }

// observe folds a poll in; true once the round has started (nothing more to learn).
func (t *successorTracker) observe(info *usdoku.GameInfo) bool {
	t.polls++
	if n := len(info.Players); n > t.maxPlayers {
		t.maxPlayers = n
	}
	if info.Info.Status != "" && info.Info.Status != "pending" {
		t.started = true
	}
	return t.started
}

func (t successorTracker) detail() string {
	return fmt.Sprintf("players=%d started=%t", t.maxPlayers, t.started)
}

// parseSuccessorDetail reads an evSuccessorRoom detail back.
func parseSuccessorDetail(d string) (players int, started, ok bool) {
	n, err := fmt.Sscanf(d, "players=%d started=%t", &players, &started)
	return players, started, err == nil && n == 2
}

// watchSuccessor polls a finished game's successor room for successorWatch
// and logs what it saw. Silent: never posts or edits anything.
func (b *Bot) watchSuccessor(chatID, gameID int64, code string) {
	if b.ud == nil || code == "" {
		return
	}
	var tr successorTracker
	deadline := time.Now().Add(successorWatch)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		info, err := b.ud.Info(ctx, code)
		cancel()
		if err != nil {
			log.Printf("watchSuccessor %s: %v", code, err)
		} else if tr.observe(info) {
			break
		}
		time.Sleep(successorPoll)
	}
	if !tr.known() {
		return // usdoku never answered: nothing learned
	}
	b.logEvent(storage.Event{ChatID: chatID, GameID: gameID, Kind: evSuccessorRoom, Detail: tr.detail()})
}
