// Package chart draws the league's pictures (season race, rating history,
// play calendar, season poster) as PNGs in pure Go, so the static alpine build
// keeps working without CGO. Renders stay in memory: nothing is written to disk.
package chart

// Race is the season race: cumulative points per player, game by game. Series
// keep their colour by position, so callers pass them in a stable per-player
// order (never by current rank).
type Race struct {
	Title  string
	Target int
	Series []Series
}

const raceW, raceH = 1080, 720

// RenderRace draws r as a PNG.
func RenderRace(r Race) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	c := newCanvas(raceW, raceH)
	c.text(fs.bold(32), 40, 52, inkMain, r.Title)
	drawLegend(c, fs, 40, 102, r.Series)
	drawLines(c, fs, box{l: 76, t: 156, r: raceW - 210, b: raceH - 72}, lineSpec{
		series:   r.Series,
		xMax:     maxLen(r.Series),
		goal:     r.Target,
		goalLbl:  "цель " + itoa(r.Target),
		fromZero: true,
		xTitle:   "игры",
		labels:   true,
	})
	return c.png()
}

// maxLen is the x range of series that all start at the origin.
func maxLen(series []Series) int {
	n := 0
	for _, s := range series {
		n = max(n, s.Offset+len(s.Points))
	}
	return n
}
