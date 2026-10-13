package bot

import (
	"fmt"
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
	Joined    []string // everyone in the room, in usdoku's order
	Finishers []liveFinisher
	Solving   []string // joined, not finished yet
}

// buildLiveSnapshot reads a usdoku poll into a snapshot; name maps a usdoku nick
// to the registered player's name (or returns the nick itself).
func buildLiveSnapshot(info *usdoku.GameInfo, name func(nick string) string) liveSnapshot {
	var s liveSnapshot
	for _, p := range info.Players {
		n := name(p.Name)
		s.Joined = append(s.Joined, n)
		if p.CompletedAt == nil {
			s.Solving = append(s.Solving, n)
		}
	}
	for _, p := range info.FinishOrder() {
		s.Finishers = append(s.Finishers, liveFinisher{
			Name: name(p.Name),
			Secs: p.SolveSeconds(info.Info.StartedAt),
		})
	}
	return s
}

// liveSectionText renders the live part of the card, "" when there is nothing
// to show yet. Before the first finish it lists who is in the room; after it,
// the finish order, who is still solving, and when the bot will record on its own.
func liveSectionText(s liveSnapshot) string {
	if len(s.Finishers) == 0 {
		if len(s.Joined) == 0 {
			return ""
		}
		return "👀 В комнате: " + escJoin(s.Joined)
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
	if len(s.Solving) == 0 {
		b.WriteString("<i>Все финишировали, записываю…</i>")
		return b.String()
	}
	fmt.Fprintf(&b, "\n⏳ Ещё решают: %s\n<i>Запишу сам через ~%d мин без новых финишей.</i>",
		escJoin(s.Solving), int(settleQuiet.Minutes()))
	return b.String()
}

// escJoin joins names with commas, escaping each for HTML.
func escJoin(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = esc(n)
	}
	return strings.Join(out, ", ")
}
