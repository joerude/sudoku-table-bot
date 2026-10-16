package bot

import (
	"fmt"
	"sort"
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
	Solving   []string  // joined, not finished yet
	Bars      []liveBar // progress of the solving ones, most solved first; nil = unknown
	Open      int       // cells to solve, the bars' 100%
}

// liveBar is one still-solving player's progress on the board.
type liveBar struct {
	Name   string
	Solved int
}

// buildLiveSnapshot reads a usdoku poll into a snapshot; name maps a usdoku nick
// to the registered player's name (or returns the nick itself). prog is the
// latest board progress, nil when there is none (lobby, or the fetch failed).
func buildLiveSnapshot(info *usdoku.GameInfo, name func(nick string) string, prog *usdoku.Progress) liveSnapshot {
	var s liveSnapshot
	for _, p := range info.Players {
		n := name(p.Name)
		s.Joined = append(s.Joined, n)
		if p.CompletedAt == nil {
			s.Solving = append(s.Solving, n)
			if prog != nil && prog.Open > 0 {
				s.Bars = append(s.Bars, liveBar{Name: n, Solved: prog.Solved[p.Name]})
			}
		}
	}
	if len(s.Bars) > 0 {
		s.Open = prog.Open
		sort.SliceStable(s.Bars, func(i, j int) bool { return s.Bars[i].Solved > s.Bars[j].Solved })
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
		if len(s.Bars) > 0 {
			return "🧩 <b>Решают:</b>\n" + barLines(s)
		}
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
	if len(s.Bars) > 0 {
		b.WriteString("\n⏳ <b>Ещё решают:</b>\n" + barLines(s) + "\n")
	} else {
		fmt.Fprintf(&b, "\n⏳ Ещё решают: %s\n", escJoin(s.Solving))
	}
	fmt.Fprintf(&b, "<i>Запишу сам через ~%d мин без новых финишей.</i>", int(settleQuiet.Minutes()))
	return b.String()
}

// barLines renders one "[▰▰▰▱▱] 38/54 · Name" line per solving player. The bar
// leads so the lines stay aligned whatever the names' widths.
func barLines(s liveSnapshot) string {
	lines := make([]string, len(s.Bars))
	for i, bar := range s.Bars {
		lines[i] = fmt.Sprintf("%s %d/%d · %s", progressBar(bar.Solved, s.Open), bar.Solved, s.Open, esc(bar.Name))
	}
	return strings.Join(lines, "\n")
}

// escJoin joins names with commas, escaping each for HTML.
func escJoin(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = esc(n)
	}
	return strings.Join(out, ", ")
}
