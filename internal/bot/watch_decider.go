package bot

import "time"

// Drain timing for auto-record. usdoku marks a hardcore game "supersededBy" (and
// keeps accepting completions on the old code) the moment the FIRST player
// finishes — not when everyone is done. So recording on the first finish loses
// the 2nd/3rd finishers. Instead we keep polling and "drain" stragglers:
//   - record at once if every joined player has finished;
//   - otherwise, once the round is over (someone finished or usdoku ended it),
//     keep waiting and record only after settleQuiet with no new finisher;
//   - drainMax bounds the wait so a never-ending trickle can't stall forever.
const (
	settleQuiet = 6 * time.Minute  // no new finisher this long → field has settled
	drainMax    = 25 * time.Minute // hard cap on draining from the first finisher
)

// watchState carries the per-game drain progress across polls. Zero value = not
// yet draining.
type watchState struct {
	drainStart time.Time // when draining began (first finisher / round over)
	lastChange time.Time // last poll where the finisher count grew
	lastCount  int       // finisher count at lastChange
}

// shouldRecord decides, given the current poll, whether to auto-record now.
// completed = players with a finish time, total = players who joined, over =
// usdoku considers the round ended (terminal status or supersededBy set).
func (s *watchState) shouldRecord(now time.Time, completed, total int, over bool) bool {
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
		return false
	}
	if completed > s.lastCount {
		s.lastCount = completed
		s.lastChange = now
	}
	if now.Sub(s.lastChange) >= settleQuiet {
		return true
	}
	return now.Sub(s.drainStart) >= drainMax
}
