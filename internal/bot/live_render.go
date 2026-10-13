package bot

import (
	"strings"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// liveFinisher is one finisher line of the live card.
type liveFinisher struct {
	Name string
	Secs int64 // solve time; 0 = unknown
}

// liveSnapshot is the in-progress state of a usdoku room, names resolved.
type liveSnapshot struct {
	Finishers []liveFinisher
}

// buildLiveSnapshot reads a usdoku poll into a snapshot; name maps a usdoku nick
// to the registered player's name (or returns the nick itself).
func buildLiveSnapshot(info *usdoku.GameInfo, name func(nick string) string) liveSnapshot {
	var s liveSnapshot
	for _, p := range info.FinishOrder() {
		s.Finishers = append(s.Finishers, liveFinisher{
			Name: name(p.Name),
			Secs: p.SolveSeconds(info.Info.StartedAt),
		})
	}
	return s
}

// liveSectionText renders the live part of the card, "" when there is nothing
// to show yet.
func liveSectionText(s liveSnapshot) string {
	if len(s.Finishers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("🏁 <b>Финишировали:</b>\n")
	for i, f := range s.Finishers {
		b.WriteString(medal(i+1) + " " + esc(f.Name))
		if f.Secs > 0 {
			b.WriteString(" · ⏱ " + fmtDuration(int(f.Secs)))
		}
		b.WriteString("\n")
	}
	b.WriteString("<i>…жду остальных</i>")
	return b.String()
}
