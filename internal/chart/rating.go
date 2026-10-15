package chart

import "strconv"

// Rating is the eternal Elo history: one line per player from their first
// rated game (at the start rating) to now, x = rated games of the chat.
type Rating struct {
	Title    string
	Subtitle string
	Games    int // x runs 0..Games
	Series   []Series
}

// RenderRating draws r as a PNG.
func RenderRating(r Rating) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	c := newCanvas(raceW, raceH)
	c.text(fs.bold(32), 40, 52, inkMain, r.Title)
	if r.Subtitle != "" {
		c.text(fs.regular(20), 40, 84, inkMuted, r.Subtitle)
	}
	drawLegend(c, fs, 40, 124, r.Series)
	drawLines(c, fs, box{l: 86, t: 172, r: raceW - 220, b: raceH - 72}, lineSpec{
		series: r.Series,
		xMax:   r.Games,
		xTitle: "игры",
		labels: true,
	})
	return c.png()
}

func itoa(n int) string { return strconv.Itoa(n) }
