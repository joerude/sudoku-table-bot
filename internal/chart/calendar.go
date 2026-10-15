package chart

import (
	"image/color"
	"math"
	"time"
)

// Calendar is a GitHub-style grid of a season's days: one column per week
// (Monday first), one cell per day shaded by how many games were played.
type Calendar struct {
	Title    string
	Subtitle string
	Start    time.Time      // first day of the grid (local date)
	End      time.Time      // last day, inclusive
	Today    time.Time      // days after it are outlined, not shaded
	Counts   map[string]int // local "2006-01-02" -> games that day
	Mark     time.Time      // a day ringed in ink (the season's last day); zero = none
	MarkLbl  string         // legend text for Mark
}

const dayKey = "2006-01-02"

// Sequential blue ramp: none, then 1, 2, 3 and 4+ games a day.
var (
	dayEmpty = rgb(0xecebe6)
	dayRamp  = []color.RGBA{rgb(0x86b6ef), rgb(0x3987e5), rgb(0x1c5cab), rgb(0x0d366b)}
	white    = rgb(0xffffff)
)

var monthsShort = []string{"янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

func dayColor(n int) (fill, ink color.RGBA) {
	if n <= 0 {
		return dayEmpty, inkMuted
	}
	i := min(n, len(dayRamp)) - 1
	if i == 0 {
		return dayRamp[0], inkMain
	}
	return dayRamp[i], white
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// RenderCalendar draws cal as a PNG.
func RenderCalendar(cal Calendar) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	start, end, today := dateOnly(cal.Start), dateOnly(cal.End), dateOnly(cal.Today)
	if end.Before(start) {
		end = start
	}
	gridStart := start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7)) // back to Monday
	weeks := int(end.Sub(gridStart).Hours()/24)/7 + 1

	const w, padL, padR, top, gap = 1080, 84.0, 40.0, 176.0, 6.0
	cell := math.Min((w-padL-padR)/float64(weeks)-gap, 64)
	gridH := 7 * (cell + gap)
	h := int(top + gridH + 110)

	c := newCanvas(w, h)
	c.text(fs.bold(32), 40, 52, inkMain, cal.Title)
	if cal.Subtitle != "" {
		c.text(fs.regular(22), 40, 88, inkMuted, cal.Subtitle)
	}

	small, num := fs.regular(18), fs.bold(18)
	for i, lbl := range []string{"пн", "", "ср", "", "пт", "", "вс"} {
		if lbl != "" {
			c.textRight(small, padL-12, top+float64(i)*(cell+gap)+cell/2+6, inkMuted, lbl)
		}
	}

	mark := dateOnly(cal.Mark)
	lastMonth := time.Month(0)
	for wk := 0; wk < weeks; wk++ {
		x := padL + float64(wk)*(cell+gap)
		for d := 0; d < 7; d++ {
			day := gridStart.AddDate(0, 0, wk*7+d)
			if day.Before(start) || day.After(end) {
				continue
			}
			if day.Month() != lastMonth { // label the column where a month starts
				lastMonth = day.Month()
				c.text(small, x, top-14, inkMuted, monthsShort[day.Month()-1])
			}
			y := top + float64(d)*(cell+gap)
			r := cell / 5
			if !cal.Mark.IsZero() && day.Equal(mark) {
				c.roundRect(x-4, y-4, x+cell+4, y+cell+4, r+3, inkMain)
				c.roundRect(x-2, y-2, x+cell+2, y+cell+2, r+1, surface)
			}
			if day.After(today) { // still to come: an outline only
				c.roundRect(x, y, x+cell, y+cell, r, gridColor)
				c.roundRect(x+2, y+2, x+cell-2, y+cell-2, r-1, surface)
				continue
			}
			n := cal.Counts[day.Format(dayKey)]
			fill, ink := dayColor(n)
			c.roundRect(x, y, x+cell, y+cell, r, fill)
			if n > 0 && cell >= 28 {
				c.textCenter(num, x+cell/2, y+cell/2+7, ink, itoa(n))
			}
		}
	}

	// Legend: the ramp, then the ringed day.
	ly := top + gridH + 46
	lf := fs.regular(20)
	c.text(lf, 40, ly+6, inkMuted, "игр в день")
	x := 40 + measure(lf, "игр в день") + 18
	for i, lbl := range []string{"0", "1", "2", "3", "4+"} {
		fill, _ := dayColor(i)
		c.roundRect(x, ly-14, x+26, ly+12, 5, fill)
		c.text(lf, x+34, ly+6, inkMuted, lbl)
		x += 34 + measure(lf, lbl) + 22
	}
	if !cal.Mark.IsZero() && cal.MarkLbl != "" {
		x += 20
		c.roundRect(x-3, ly-17, x+29, ly+15, 7, inkMain)
		c.roundRect(x-1, ly-15, x+27, ly+13, 6, surface)
		c.roundRect(x+1, ly-13, x+25, ly+11, 5, dayEmpty)
		c.text(lf, x+38, ly+6, inkMuted, cal.MarkLbl)
	}
	return c.png()
}
