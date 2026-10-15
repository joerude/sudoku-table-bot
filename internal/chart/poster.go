package chart

import (
	"fmt"
	"image/color"
)

// Poster is a finished season's card: champion, final table, the race as an
// inset and the season's awards.
type Poster struct {
	Season    int
	Champion  string
	Points    int
	Standings []PosterRow // final order
	Awards    []Award
	Race      Race   // the inset; its Title is not drawn
	Footer    string // e.g. "42 игры · 21 сен → 14 ноя"
}

// PosterRow is one line of the final table.
type PosterRow struct {
	Name   string
	Points int
	Wins   int
}

// Award is one line of the awards block: what, and who with the value.
type Award struct {
	Title string // "Больше всех побед"
	Value string // "mister · 21"
}

const posterW = 1080

// Medal fills for places 1-3; lower places get the grid colour.
var medals = []color.RGBA{rgb(0xd9a400), rgb(0x9c9c9c), rgb(0xb5743a)}

// RenderPoster draws p as a PNG; its height follows the table and awards.
func RenderPoster(p Poster) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	const rowH, awardH = 66.0, 52.0
	tableTop := 300.0
	insetTop := tableTop + rowH*float64(len(p.Standings)) + 60
	insetB := insetTop + 330
	awardsTop := insetB + 110
	h := int(awardsTop + awardH*float64(len(p.Awards)) + 90)
	if len(p.Awards) > 0 {
		h += 50
	}

	c := newCanvas(posterW, h)
	c.rect(0, 0, posterW, 10, medals[0])

	// Header: season, champion, trophy.
	c.text(fs.bold(26), 60, 92, inkMuted, fmt.Sprintf("СЕЗОН %d", p.Season))
	c.text(fs.bold(76), 60, 186, inkMain, clip(p.Champion, 16))
	c.text(fs.regular(28), 60, 236, inkMuted, fmt.Sprintf("чемпион · %d %s", p.Points, pointsWord(p.Points)))
	trophy(c, posterW-150, 150)

	// Final table.
	for i, r := range p.Standings {
		y := tableTop + float64(i)*rowH
		fill := gridColor
		if i < len(medals) {
			fill = medals[i]
		}
		c.disc(84, y+22, 24, fill)
		c.textCenter(fs.bold(24), 84, y+31, white, itoa(i+1))
		c.text(fs.bold(32), 128, y+34, inkMain, clip(r.Name, 18))
		c.textRight(fs.regular(26), posterW-60, y+33, inkMuted,
			fmt.Sprintf("%d %s · %d поб", r.Points, pointsWord(r.Points), r.Wins))
		if i < len(p.Standings)-1 {
			c.hline(60, posterW-60, y+rowH-10, 1.5, gridColor)
		}
	}

	// The race, small.
	c.text(fs.bold(26), 60, insetTop, inkMain, "Как шла гонка")
	drawLines(c, fs, box{l: 110, t: insetTop + 40, r: posterW - 210, b: insetB}, lineSpec{
		series:   p.Race.Series,
		xMax:     maxLen(p.Race.Series),
		goal:     p.Race.Target,
		goalLbl:  "цель " + itoa(p.Race.Target),
		fromZero: true,
		labels:   true,
		small:    true,
	})

	if len(p.Awards) > 0 {
		c.text(fs.bold(26), 60, awardsTop, inkMain, "Награды")
		for i, a := range p.Awards {
			y := awardsTop + 54 + float64(i)*awardH
			c.text(fs.regular(26), 60, y, inkMuted, a.Title)
			c.textRight(fs.bold(26), posterW-60, y, inkMain, a.Value)
		}
	}

	if p.Footer != "" {
		c.textCenter(fs.regular(22), posterW/2, float64(h)-36, inkMuted, p.Footer)
	}
	return c.png()
}

// trophy draws a simple gold cup centred at (x, y).
func trophy(c *canvas, x, y float64) {
	gold := medals[0]
	for _, hx := range []float64{x - 52, x + 52} { // handles
		c.disc(hx, y-34, 26, gold)
		c.disc(hx, y-34, 15, surface)
	}
	c.rect(x-52, y-74, x+52, y-20, gold) // bowl
	c.disc(x, y-20, 52, gold)
	c.rect(x-10, y+20, x+10, y+52, gold)         // stem
	c.roundRect(x-44, y+50, x+44, y+70, 6, gold) // base
}

// pointsWord is the Russian plural of "очко" for n.
func pointsWord(n int) string {
	n %= 100
	switch {
	case n >= 11 && n <= 14:
		return "очков"
	case n%10 == 1:
		return "очко"
	case n%10 >= 2 && n%10 <= 4:
		return "очка"
	}
	return "очков"
}
