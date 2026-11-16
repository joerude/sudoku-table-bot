// Package chart draws the league's pictures (season race, rating history,
// play calendar, season poster) as PNGs in pure Go, so the static alpine build
// keeps working without CGO. Renders stay in memory: nothing is written to disk.
package chart

// Race is the season race: cumulative points per player, game by game. Series
// keep their colour by position, so callers pass them in a stable per-player
// order (never by current rank).
//
// Fit turns it into a window of the race (the week): lines start at each
// Series.Base, the y axis fits the data, and the goal line shows only when it
// falls inside that range.
//
// Winners adds a strip under the plot: one cell per game in the colour of the
// player who won it, so runs of wins show at a glance. Only wins are drawn on
// purpose: a row of every place would turn one player's non-finishes into a
// wall of grey.
type Race struct {
	Title   string
	Target  int
	Series  []Series
	Fit     bool
	XTitle  string // under the x axis; "" = "игры"
	Winners []int  // per game, the index in Series of its winner; -1 = nobody shown
}

const (
	raceW, raceH = 1080, 720
	raceStripH   = 44 // extra canvas height for the winners strip
)

// RenderRace draws r as a PNG.
func RenderRace(r Race) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	xTitle := r.XTitle
	if xTitle == "" {
		xTitle = "игры"
	}
	h := raceH
	if len(r.Winners) > 0 {
		h += raceStripH
	}
	p := box{l: 76, t: 156, r: raceW - 210, b: raceH - 72}
	xMax := maxLen(r.Series)
	c := newCanvas(raceW, h)
	c.text(fs.bold(32), 40, 52, inkMain, r.Title)
	drawLegend(c, fs, 40, 102, r.Series)
	drawWinners(c, fs, p, xMax, r)
	drawLines(c, fs, p, lineSpec{
		series:   r.Series,
		xMax:     xMax,
		goal:     r.Target,
		goalLbl:  "цель " + itoa(r.Target),
		fromZero: !r.Fit,
		xTitle:   xTitle,
		labels:   true,
	})
	return c.png()
}

// drawWinners draws the winners strip below the x axis of plot p: game i sits
// under the line segment that leads to its point (x from i to i+1).
func drawWinners(c *canvas, fs *faces, p box, xMax int, r Race) {
	if len(r.Winners) == 0 || xMax == 0 {
		return
	}
	const top, height = raceH + 4, 16.0
	w := (p.r - p.l) / float64(xMax)
	gap := 2.0
	if w < 6 { // a long season: cells touch rather than vanish
		gap = 0
	}
	for i, win := range r.Winners {
		if i >= xMax {
			break
		}
		col := gridColor
		if win >= 0 && win < len(r.Series) {
			col = slot(win)
		}
		x := p.l + w*float64(i)
		c.rect(x+gap/2, top, x+w-gap/2, top+height, col)
	}
	c.text(fs.regular(18), p.r+24, top+14, inkMuted, "победитель игры")
}

// maxLen is the x range of series that all start at the origin.
func maxLen(series []Series) int {
	n := 0
	for _, s := range series {
		n = max(n, s.Offset+len(s.Points))
	}
	return n
}
