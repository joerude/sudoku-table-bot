package chart

// Replay is one game as it went: cells solved per player over time. Series
// points sit Step seconds apart from each player's start; a finisher's line
// ends at Open, the others stop where the bot last saw them.
type Replay struct {
	Title  string // "Ход игры · QWER"
	Tag    string // e.g. "Medium · Hardcore"
	Meta   string // when and how long, e.g. "Пн, 23 ноября · 09:13 · 5 мин 12 с"
	Open   int    // cells to solve: the finish line
	Step   int    // seconds per x unit, a divisor of 60
	Series []Series
	Rows   []ReplayRow // the standing right of the plot, in place order
	Quote  string      // the winner's /phrase, drawn under the plot ("" = none)
	By     string      // the winner's name, after the quote
}

// ReplayRow is one line of the standing next to the replay.
type ReplayRow struct {
	Slot  int    // index of the player's series: the colour
	Place int    // 1-based finish place; 0 = did not finish
	Name  string // player name
	Value string // "2:59" or "18/51"
	Note  string // a muted second line, e.g. "с 22-й минуты"; "" = none
}

const (
	replayW, replayH = 1200, 720
	replayTableW     = 330 // the standing column right of the plot
	quoteRunes       = 80  // the /phrase limit plus room for the name
)

// RenderReplay draws r as a PNG.
func RenderReplay(r Replay) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	step := r.Step
	if step <= 0 || 60%step != 0 {
		step = 10
	}
	perMin := 60 / step
	xMax := maxLen(r.Series)

	c := newCanvas(replayW, replayH)
	c.text(fs.bold(34), 40, 56, inkMain, r.Title)
	if r.Tag != "" {
		c.textRight(fs.regular(24), replayW-40, 54, inkMuted, r.Tag)
	}
	if r.Meta != "" {
		c.text(fs.regular(24), 40, 94, inkMuted, r.Meta)
	}
	bottom := float64(replayH - 76)
	if q := drawable(fontRegular, r.Quote); q != "" {
		q = "«" + q + "»"
		if by := drawable(fontRegular, r.By); by != "" {
			q += " · " + by
		}
		c.text(fs.regular(24), 40, replayH-28, inkMuted, clip(q, quoteRunes))
		bottom -= 40
	}

	plot := box{l: 84, t: 156, r: replayW - replayTableW - 40, b: bottom}
	c.text(fs.regular(22), 40, plot.t-30, inkMuted, "клетки")
	drawLines(c, fs, plot, lineSpec{
		series:   r.Series,
		xMax:     xMax,
		goal:     r.Open,
		fromZero: true,
		capGoal:  true,
		large:    true,
		xTitle:   "минуты",
		xStep:    niceStep(xMax/perMin, 8) * perMin,
		xFmt:     func(x int) string { return itoa(x / perMin) },
	})
	drawStanding(c, fs, replayW-replayTableW, plot.t+12, replayW-40, r.Rows)
	return c.png()
}

// drawStanding lists the rows from (x, y) to right: place, colour dot, name
// and value, with an optional muted note under the name.
func drawStanding(c *canvas, fs *faces, x, y, right float64, rows []ReplayRow) {
	place, name, value, note := fs.bold(26), fs.bold(26), fs.regular(26), fs.regular(20)
	for _, row := range rows {
		if row.Place > 0 {
			c.textRight(place, x+20, y, inkMuted, itoa(row.Place))
		}
		c.disc(x+40, y-9, 9, slot(row.Slot))
		c.textRight(value, right, y, inkMain, row.Value)
		room := right - measure(value, row.Value) - 16 - (x + 58)
		n := drawable(fontBd, row.Name)
		for k := 12; k > 1 && measure(name, n) > room; k-- {
			n = clip(n, k)
		}
		c.text(name, x+58, y, inkMain, n)
		y += 48
		if row.Note != "" {
			c.text(note, x+58, y-18, inkMuted, row.Note)
			y += 18
		}
	}
}
