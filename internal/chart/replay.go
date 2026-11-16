package chart

// Replay is one game as it went: cells solved per player over time. Series
// points sit Step seconds apart from the round start; a finisher's line ends
// at Open, the others stop where the bot last saw them.
type Replay struct {
	Title  string // "Ход игры · QWER"
	Tag    string // e.g. "Medium · Hardcore"
	Open   int    // cells to solve: the finish line
	Step   int    // seconds per x unit, a divisor of 60
	Series []Series
}

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

	c := newCanvas(raceW, raceH)
	c.text(fs.bold(32), 40, 52, inkMain, r.Title)
	if r.Tag != "" {
		c.textRight(fs.regular(22), raceW-40, 50, inkMuted, r.Tag)
	}
	drawLegend(c, fs, 40, 102, r.Series)
	drawLines(c, fs, box{l: 76, t: 156, r: raceW - 230, b: raceH - 72}, lineSpec{
		series:   r.Series,
		xMax:     xMax,
		goal:     r.Open,
		goalLbl:  "все клетки: " + itoa(r.Open),
		fromZero: true,
		xTitle:   "минуты",
		xStep:    niceStep(xMax/perMin, 8) * perMin,
		xFmt:     func(x int) string { return itoa(x / perMin) },
		labels:   true,
	})
	return c.png()
}
