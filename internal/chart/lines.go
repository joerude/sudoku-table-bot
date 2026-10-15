package chart

import (
	"fmt"
	"image/color"
	"math"
	"sort"
)

// Series is one player's line. It starts at (Offset, Base) and Points[i] sits
// at x = Offset+i+1, so a line can join later than x = 0 (a player's first
// rated game). Zero Offset and Base start at the origin (season points).
type Series struct {
	Name   string
	Offset int
	Base   int
	Points []int
}

func (s Series) last() int {
	if len(s.Points) == 0 {
		return s.Base
	}
	return s.Points[len(s.Points)-1]
}

// box is a plot area in final pixels; axis labels go outside it.
type box struct{ l, t, r, b float64 }

// lineSpec says how drawLines frames its series.
type lineSpec struct {
	series   []Series
	xMax     int    // x runs 0..xMax
	goal     int    // >0: a dashed goal line with goalLbl
	goalLbl  string // e.g. "цель 100"
	fromZero bool   // y starts at 0 and reaches the goal; otherwise it fits the data
	xTitle   string // under the x axis, right-aligned; "" for none
	labels   bool   // end labels right of the plot
	small    bool   // inset (poster): smaller text and lines
}

// drawLines draws grid, axes, the goal line and every series into p.
func drawLines(c *canvas, fs *faces, p box, sp lineSpec) {
	tick, label := fs.regular(20), fs.bold(22)
	lw, dot := 5.0, 7.0
	if sp.xMax > 120 {
		lw = 3.5
	}
	if sp.small {
		tick, label = fs.regular(18), fs.bold(20)
		lw, dot = 4, 6
	}

	lo, hi := yRange(sp)
	step := niceStep(hi-lo, 6)
	lo = floorTo(lo, step)
	hi = ceilTo(hi, step)
	if hi == lo {
		hi = lo + step
	}
	xOf := func(x int) float64 {
		if sp.xMax == 0 {
			return p.l
		}
		return p.l + (p.r-p.l)*float64(x)/float64(sp.xMax)
	}
	yOf := func(v int) float64 { return p.b - (p.b-p.t)*float64(v-lo)/float64(hi-lo) }

	for v := lo; v <= hi; v += step {
		y := yOf(v)
		c.hline(p.l, p.r, y, 1.5, gridColor)
		c.textRight(tick, p.l-12, y+7, inkMuted, fmt.Sprint(v))
	}
	xStep := niceStep(sp.xMax, 8)
	for x := 0; x <= sp.xMax; x += xStep {
		c.textCenter(tick, xOf(x), p.b+30, inkMuted, fmt.Sprint(x))
	}
	if sp.xTitle != "" {
		c.textRight(tick, p.r, p.b+58, inkMuted, sp.xTitle)
	}
	if sp.goal > 0 && sp.goal >= lo && sp.goal <= hi {
		y := yOf(sp.goal)
		c.dashed(p.l, p.r, y, 2, goalColor)
		c.textRight(tick, p.r-4, y-10, inkMuted, sp.goalLbl)
	}

	type end struct {
		y, labelY float64
		col       color.RGBA
		lbl       string
	}
	var ends []end
	for i, s := range sp.series {
		col := slot(i)
		px, py := xOf(s.Offset), yOf(s.Base)
		for j, v := range s.Points {
			x, y := xOf(s.Offset+j+1), yOf(v)
			c.line(px, py, x, y, lw, col)
			px, py = x, y
		}
		c.disc(px, py, dot+3, surface)
		c.disc(px, py, dot, col)
		ends = append(ends, end{y: py, labelY: py, col: col, lbl: fmt.Sprintf("%s %d", clip(s.Name, 12), s.last())})
	}
	if !sp.labels {
		return
	}

	// End labels right of the plot, pushed apart so they never overlap; a
	// short leader ties a moved label to its line.
	gap := 34.0
	if sp.small {
		gap = 28
	}
	sort.SliceStable(ends, func(i, j int) bool { return ends[i].y < ends[j].y })
	for i := 1; i < len(ends); i++ {
		ends[i].labelY = math.Max(ends[i].labelY, ends[i-1].labelY+gap)
	}
	if n := len(ends); n > 0 && ends[n-1].labelY > p.b {
		shift := ends[n-1].labelY - p.b
		for i := range ends {
			ends[i].labelY -= shift
		}
	}
	for _, e := range ends {
		if math.Abs(e.labelY-e.y) > 3 {
			c.line(p.r+dot+2, e.y, p.r+20, e.labelY, 2, e.col)
		}
		c.text(label, p.r+24, e.labelY+8, inkMain, e.lbl)
	}
}

// yRange is the data's value range, widened by the goal line for fromZero.
func yRange(sp lineSpec) (lo, hi int) {
	lo, hi = math.MaxInt, math.MinInt
	for _, s := range sp.series {
		lo, hi = min(lo, s.Base), max(hi, s.Base)
		for _, v := range s.Points {
			lo, hi = min(lo, v), max(hi, v)
		}
	}
	if lo > hi { // no series
		lo, hi = 0, 0
	}
	if sp.fromZero {
		lo, hi = 0, max(hi, sp.goal)
	}
	return lo, hi
}

func floorTo(v, step int) int {
	if v < 0 {
		return -ceilTo(-v, step)
	}
	return v / step * step
}

func ceilTo(v, step int) int {
	if v < 0 {
		return -floorTo(-v, step)
	}
	return (v + step - 1) / step * step
}

// drawLegend draws a swatch and name per series in one row from (x, y).
func drawLegend(c *canvas, fs *faces, x, y float64, series []Series) {
	f := fs.regular(22)
	for i, s := range series {
		name := clip(s.Name, 12)
		c.line(x, y-8, x+30, y-8, 5, slot(i))
		c.text(f, x+40, y, inkMain, name)
		x += 40 + measure(f, name) + 34
	}
}
