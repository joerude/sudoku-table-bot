package bot

import "time"

// Drain timing for auto-record. usdoku marks a hardcore game "supersededBy" (and
// keeps accepting completions on the old code) the moment the FIRST player
// finishes — not when everyone is done. So recording on the first finish loses
// the 2nd/3rd finishers. Instead we keep polling and "drain" stragglers:
//   - record at once if every joined player has finished;
//   - otherwise, once the round is over (someone finished or usdoku ended it),
//     keep waiting and record only after settleQuiet with no new finisher and
//     no new right cell (a player still filling the board is still playing:
//     recording them as a non-finisher made them finish after the record);
//   - drainMax bounds the wait so a never-ending trickle can't stall forever.
const (
	settleQuiet = 6 * time.Minute  // no new finisher or cell this long → field has settled
	drainMax    = 25 * time.Minute // hard cap on draining from the first finisher
)

// watchState carries the per-game drain progress across polls. Zero value = not
// yet draining.
type watchState struct {
	drainStart time.Time // when draining began (first finisher / round over)
	lastChange time.Time // last poll where the finisher count or the cells grew
	lastCount  int       // finisher count at lastChange
	maxCells   int       // most right cells seen on the board, all players summed
}

// shouldRecord decides, given the current poll, whether to auto-record now.
// completed = players with a finish time, total = players who joined, over =
// usdoku considers the round ended (terminal status or supersededBy set),
// cells = right cells of all players summed from the latest board fetch (0 =
// unknown). Cells only count when they pass the best seen so far: in hardcore
// a wrong digit can replace a right one, and fixing it back is no progress.
func (s *watchState) shouldRecord(now time.Time, completed, total int, over bool, cells int) bool {
	if total > 0 && completed == total {
		return true // everyone who joined has finished — nothing left to wait for
	}
	if completed == 0 && !over {
		return false // game still in progress, no finishers — keep plain polling
	}
	if s.drainStart.IsZero() {
		s.drainStart = now
		s.lastChange = now
		s.lastCount = completed
		s.maxCells = max(s.maxCells, cells)
		return false
	}
	if completed > s.lastCount {
		s.lastCount = completed
		s.lastChange = now
	}
	if cells > s.maxCells {
		s.maxCells = cells
		s.lastChange = now
		if completed == 0 {
			// Nobody finished yet the board is filling: usdoku's "over" is
			// left from an earlier round in this room, not this one. The cap
			// counts from this round's play, not from that stale flag.
			s.drainStart = now
		}
	}
	if now.Sub(s.lastChange) >= settleQuiet {
		return true
	}
	return now.Sub(s.drainStart) >= drainMax
}

// roundStarted reports whether usdoku's status moved from the lobby to the race
// between two polls. A watcher that first sees a room already running (bot
// restart, /code on a live room) has prev == "" and posts nothing.
func roundStarted(prev, cur string) bool {
	return prev == "pending" && cur == "running"
}
