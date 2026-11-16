package bot

import (
	"fmt"
	"log"

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
// who, detail = "solved/open". It is logged whether or not the line is shown:
// the result post reads it back (so an edit or a restart keeps the line), and
// it tells boards dropped early from boards dropped near the end.
const evDNFProgress = "dnf_progress"

// boardProgress is how far a player got: Solved of Open cells.
type boardProgress struct{ Solved, Open int }

// worthShowing: at least half of the board, and not all of it (a full board
// without a finish is bad data, not progress).
func (p boardProgress) worthShowing() bool {
	return p.Open > 0 && p.Solved*2 >= p.Open && p.Solved < p.Open
}

func (p boardProgress) detail() string { return fmt.Sprintf("%d/%d", p.Solved, p.Open) }

// parseBoardProgress reads an evDNFProgress detail back.
func parseBoardProgress(d string) (boardProgress, bool) {
	var p boardProgress
	n, err := fmt.Sscanf(d, "%d/%d", &p.Solved, &p.Open)
	return p, err == nil && n == 2
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
		b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: pl.ID, Kind: evDNFProgress,
			Detail: boardProgress{Solved: min(last.solved[p.Name], last.open), Open: last.open}.detail()})
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
