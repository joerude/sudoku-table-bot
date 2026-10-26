package bot

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// The manual picker trusts usdoku for whoever already finished there: those
// places are filled in when the picker opens and cannot be picked over, undone
// or reset. Everyone after them is still the picker's call.

const (
	// lockCacheTTL spares usdoku a request per tap: picker taps come seconds apart.
	lockCacheTTL = 10 * time.Second
	// lockFetchTimeout keeps a slow usdoku from stalling the callback answer.
	lockFetchTimeout = 4 * time.Second
)

type lockEntry struct {
	at    time.Time
	picks []storage.Pick
}

var (
	lockMu    sync.Mutex
	lockCache = map[int64]lockEntry{} // gameID -> usdoku-confirmed finishers
)

// confirmedFinishers maps usdoku's finish order to players. It stops at the
// first nick nobody claimed: that may be a player without /setnick, so every
// place after it is unknown. In a duel (pair set) a known non-duelist is a
// guest and is skipped, as in autoRecord.
func confirmedFinishers(order []usdoku.Player, startedAt int64, lookup func(nick string) int64,
	pair map[int64]bool) []storage.Pick {
	var out []storage.Pick
	for _, p := range order {
		id := lookup(p.Name)
		if id == 0 {
			break
		}
		if len(pair) > 0 && !pair[id] {
			continue
		}
		out = append(out, storage.Pick{PlayerID: id, DurationSecs: p.SolveSeconds(startedAt)})
	}
	return out
}

// mergeLocked puts the confirmed finishers first and keeps the other picks
// after them in their order. changed=false means picked already agrees.
func mergeLocked(picked []int64, locked []storage.Pick) (merged []storage.Pick, changed bool) {
	inLock := make(map[int64]bool, len(locked))
	merged = append(merged, locked...)
	for _, l := range locked {
		inLock[l.PlayerID] = true
	}
	for _, id := range picked {
		if !inLock[id] {
			merged = append(merged, storage.Pick{PlayerID: id})
		}
	}
	if len(merged) != len(picked) {
		return merged, true
	}
	for i := range merged {
		if merged[i].PlayerID != picked[i] {
			return merged, true
		}
	}
	return merged, false
}

// usdokuFinishers returns the game's usdoku-confirmed finishers in order, or
// nil when the game has no room, the room may already host another game
// (usdoku reuses codes, so only a game younger than the watcher's window is
// trusted) or usdoku did not answer.
func (b *Bot) usdokuFinishers(game *storage.Game) []storage.Pick {
	if !game.UsdokuCode.Valid || game.UsdokuCode.String == "" {
		return nil
	}
	created, err := time.Parse("2006-01-02 15:04:05", game.CreatedAt)
	if err != nil || time.Since(created) > watchTimeout {
		return nil
	}
	lockMu.Lock()
	e, ok := lockCache[game.ID]
	lockMu.Unlock()
	if ok && time.Since(e.at) < lockCacheTTL {
		return e.picks
	}

	ctx, cancel := context.WithTimeout(context.Background(), lockFetchTimeout)
	defer cancel()
	info, err := b.ud.Info(ctx, game.UsdokuCode.String)
	if err != nil {
		log.Printf("usdokuFinishers.info %s: %v", game.UsdokuCode.String, err)
		return nil
	}
	pairIDs, err := b.st.DuelPlayerIDs(game.ID)
	if err != nil {
		log.Printf("usdokuFinishers.pair: %v", err)
		return nil
	}
	pair := make(map[int64]bool, len(pairIDs))
	for _, id := range pairIDs {
		pair[id] = true
	}
	picks := confirmedFinishers(info.FinishOrder(), info.Info.StartedAt, func(nick string) int64 {
		if pl, _ := b.st.PlayerByNick(game.ChatID, nick); pl != nil {
			return pl.ID
		}
		return 0
	}, pair)

	lockMu.Lock()
	lockCache[game.ID] = lockEntry{at: time.Now(), picks: picks}
	lockMu.Unlock()
	return picks
}

// syncPicks makes the game's picks start with the usdoku-confirmed finishers
// and returns how many places are locked that way. Callers hold
// recordingGames for the game, so autoRecord cannot interleave.
func (b *Bot) syncPicks(game *storage.Game) int {
	locked := b.usdokuFinishers(game)
	if len(locked) == 0 {
		return 0
	}
	picked, err := b.st.PickedPlayerIDs(game.ID)
	if err != nil {
		log.Printf("syncPicks.picked: %v", err)
		return 0
	}
	if merged, changed := mergeLocked(picked, locked); changed {
		if err := b.st.ReplacePicks(game.ID, merged); err != nil {
			log.Printf("syncPicks.replace: %v", err)
			return 0
		}
	}
	return len(locked)
}
