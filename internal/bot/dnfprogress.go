package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// featDNFProgress adds how far a non-finisher got to their result row:
// «без финиша · 48 из 54». A board left most of the way through reads as
// "nearly there", not as a blank, which is the point: finishing is the
// league's weakest number. Under half of the board the row stays as it was,
// since a small number would only read as a reproach.
const featDNFProgress = "dnfprogress"

// evDNFProgress logs a non-finisher's cells at the auto-record; player_id =
// who, detail = "solved/open/wrong" (an older "solved/open" reads as no wrong
// cells, and an older binary reads the first two numbers). It is logged
// whether or not the line is shown: the result post reads it back (so an edit
// or a restart keeps the line), and it tells boards dropped early from boards
// dropped near the end.
const evDNFProgress = "dnf_progress"

// boardProgress is how far a player got: Solved of Open cells, with Wrong more
// holding a wrong digit.
type boardProgress struct{ Solved, Open, Wrong int }

// worthShowing: at least half of the board, and not all of it (a full board
// without a finish is bad data, not progress).
func (p boardProgress) worthShowing() bool {
	return p.Open > 0 && p.Solved*2 >= p.Open && p.Solved < p.Open
}

// note is the progress as the result row words it, "48 из 54" plus the errors:
// "48 из 54, 3 с ошибкой", or "все клетки заняты, 3 с ошибкой" when the board
// is full and only the wrong cells kept the finish away.
func (p boardProgress) note() string {
	switch {
	case p.Wrong > 0 && p.Solved+p.Wrong >= p.Open:
		return fmt.Sprintf("все клетки заняты, %d с ошибкой", p.Wrong)
	case p.Wrong > 0:
		return fmt.Sprintf("%d из %d, %d с ошибкой", p.Solved, p.Open, p.Wrong)
	}
	return fmt.Sprintf("%d из %d", p.Solved, p.Open)
}

func (p boardProgress) detail() string {
	return fmt.Sprintf("%d/%d/%d", p.Solved, p.Open, p.Wrong)
}

// parseBoardProgress reads an evDNFProgress detail back: "solved/open/wrong",
// or the older "solved/open" (no wrong cells known).
func parseBoardProgress(d string) (boardProgress, bool) {
	parts := strings.Split(d, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return boardProgress{}, false
	}
	var v [3]int
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return boardProgress{}, false
		}
		v[i] = n
	}
	return boardProgress{Solved: v[0], Open: v[1], Wrong: v[2]}, true
}

// logDNFProgress records, at the auto-record, the cells of everybody who is
// in the room and has not finished, from the watcher's newest board fetch.
// Nothing is logged without a fetch (a restart mid-game, a progress outage).
func (b *Bot) logDNFProgress(game *storage.Game, info *usdoku.GameInfo) {
	last, ok := lastReplaySample(game.ID)
	if !ok || last.open <= 0 {
		return
	}
	for _, p := range info.Players {
		if p.CompletedAt != nil {
			continue
		}
		pl, err := b.st.PlayerByNick(game.ChatID, p.Name)
		if err != nil || pl == nil {
			continue
		}
		solved := min(last.solved[p.Name], last.open)
		b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: pl.ID, Kind: evDNFProgress,
			Detail: boardProgress{Solved: solved, Open: last.open, Wrong: min(last.wrong[p.Name], last.open-solved)}.detail()})
	}
}

// dnfProgress is the logged progress of a game's non-finishers by player id,
// nil when the mechanic is off. A re-record logs again: the newest wins.
func (b *Bot) dnfProgress(game *storage.Game) map[int64]boardProgress {
	if !b.featureOn(game.ChatID, featDNFProgress) {
		return nil
	}
	events, err := b.st.GameEvents(game.ChatID, game.ID, evDNFProgress)
	if err != nil {
		log.Printf("dnfProgress %d: %v", game.ID, err)
		return nil
	}
	out := map[int64]boardProgress{}
	for _, e := range events {
		if p, ok := parseBoardProgress(e.Detail); ok {
			out[e.PlayerID] = p
		}
	}
	return out
}
