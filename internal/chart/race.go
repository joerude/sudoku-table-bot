// Package chart draws the season race as a PNG line chart: pure Go (no CGO),
// so the static alpine build keeps working.
package chart

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Series is one player's line: cumulative season points after each game.
type Series struct {
	Name   string
	Points []int // Points[i] = total after game i+1; every series has the same length
}

// Race is the whole chart. Series keep their colour by position, so callers
// pass them in a stable per-player order (never by current rank).
type Race struct {
	Title  string
	Target int
	Series []Series
}

// Final size; drawn at ss times this and scaled down, which smooths the lines.
const (
	width, height = 1080, 720
	ss            = 2
)

// Light surface and ink, plus the categorical slots in their fixed order
// (validated palette: the first three slots are distinct all-pairs, colour
// blindness included).
var (
	surface   = rgb(0xfcfcfb)
	inkMain   = rgb(0x0b0b0b)
	inkMuted  = rgb(0x52514e)
	gridColor = rgb(0xe6e5e0)
	goalColor = rgb(0x8a8984)
	slots     = []color.RGBA{
		rgb(0x2a78d6), rgb(0xeb6834), rgb(0x1baf7a), rgb(0xeda100),
		rgb(0xe87ba4), rgb(0x008300), rgb(0x6250d6), rgb(0xe34948),
	}
)

// Layout in final pixels.
const (
	padLeft   = 76
	padRight  = 210
	padTop    = 130
	padBottom = 72
	lineW     = 5
	dotR      = 7
	labelGap  = 34 // minimum vertical distance between end labels
	nameMax   = 12
)

// RenderRace draws r as a PNG.
func RenderRace(r Race) ([]byte, error) {
	regular, err := face(goregular.TTF, 22)
	if err != nil {
		return nil, err
	}
	small, err := face(goregular.TTF, 20)
	if err != nil {
		return nil, err
	}
	bold, err := face(gobold.TTF, 32)
	if err != nil {
		return nil, err
	}
	labelBold, err := face(gobold.TTF, 22)
	if err != nil {
		return nil, err
	}

	c := newCanvas()
	c.fill(surface)

	games := 0
	top := r.Target
	for _, s := range r.Series {
		games = max(games, len(s.Points))
		for _, p := range s.Points {
			top = max(top, p)
		}
	}
	step := niceStep(top, 6)
	top = (top + step - 1) / step * step
	if top == 0 {
		top = step
	}

	plotL, plotR := float64(padLeft), float64(width-padRight)
	plotT, plotB := float64(padTop), float64(height-padBottom)
	xOf := func(game int) float64 {
		if games == 0 {
			return plotL
		}
		return plotL + (plotR-plotL)*float64(game)/float64(games)
	}
	yOf := func(pts int) float64 {
		return plotB - (plotB-plotT)*float64(pts)/float64(top)
	}

	// Title and legend.
	c.text(bold, 40, 52, inkMain, r.Title)
	lx := 40.0
	for i, s := range r.Series {
		col := slots[i%len(slots)]
		c.line(lx, 94, lx+30, 94, lineW, col)
		name := clip(s.Name)
		c.text(regular, lx+40, 102, inkMain, name)
		lx += 40 + c.measure(regular, name) + 34
	}

	// Grid, y labels, x ticks.
	for v := 0; v <= top; v += step {
		y := yOf(v)
		c.hline(plotL, plotR, y, 1.5, gridColor)
		lbl := fmt.Sprint(v)
		c.text(small, plotL-12-c.measure(small, lbl), y+7, inkMuted, lbl)
	}
	xStep := niceStep(games, 8)
	for g := 0; g <= games; g += xStep {
		lbl := fmt.Sprint(g)
		c.text(small, xOf(g)-c.measure(small, lbl)/2, plotB+30, inkMuted, lbl)
	}
	c.text(small, plotR-c.measure(small, "игры"), plotB+60, inkMuted, "игры")

	// Target line.
	if r.Target > 0 {
		y := yOf(r.Target)
		c.dashed(plotL, plotR, y, 2, goalColor)
		lbl := fmt.Sprintf("цель %d", r.Target)
		c.text(small, plotL+8, y-10, inkMuted, lbl)
	}

	// Lines: every series starts at 0 before game 1.
	type end struct {
		y   float64
		col color.RGBA
		lbl string
	}
	var ends []end
	for i, s := range r.Series {
		col := slots[i%len(slots)]
		px, py := xOf(0), yOf(0)
		last := 0
		for g, p := range s.Points {
			x, y := xOf(g+1), yOf(p)
			c.line(px, py, x, y, lineW, col)
			px, py, last = x, y, p
		}
		c.disc(px, py, dotR+3, surface)
		c.disc(px, py, dotR, col)
		ends = append(ends, end{y: py, col: col, lbl: fmt.Sprintf("%s %d", clip(s.Name), last)})
	}

	// End labels right of the plot, pushed apart so they never overlap.
	sort.SliceStable(ends, func(i, j int) bool { return ends[i].y < ends[j].y })
	for i := 1; i < len(ends); i++ {
		ends[i].y = math.Max(ends[i].y, ends[i-1].y+labelGap)
	}
	if n := len(ends); n > 0 && ends[n-1].y > plotB {
		shift := ends[n-1].y - plotB
		for i := range ends {
			ends[i].y -= shift
		}
	}
	for _, e := range ends {
		c.disc(plotR+22, e.y, 6, e.col)
		c.text(labelBold, plotR+36, e.y+8, inkMain, e.lbl)
	}

	return c.png()
}

// niceStep picks a 1/2/5×10ⁿ step that splits span into at most n parts.
func niceStep(span, n int) int {
	if span <= 0 {
		return 1
	}
	for mag := 1; ; mag *= 10 {
		for _, m := range []int{1, 2, 5} {
			if s := m * mag; span/s <= n {
				return s
			}
		}
	}
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= nameMax {
		return s
	}
	return string([]rune(s)[:nameMax-1]) + "…"
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func face(ttf []byte, size float64) (font.Face, error) {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{Size: size * ss, DPI: 72, Hinting: font.HintingFull})
}

// canvas draws in final-pixel coordinates on a supersampled image.
type canvas struct{ img *image.RGBA }

func newCanvas() *canvas {
	return &canvas{img: image.NewRGBA(image.Rect(0, 0, width*ss, height*ss))}
}

func (c *canvas) fill(col color.RGBA) {
	xdraw.Draw(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{}, xdraw.Src)
}

// disc fills a circle of radius r centred at (x, y).
func (c *canvas) disc(x, y, r float64, col color.RGBA) {
	cx, cy, rr := x*ss, y*ss, r*ss
	for py := int(cy - rr); py <= int(cy+rr)+1; py++ {
		for px := int(cx - rr); px <= int(cx+rr)+1; px++ {
			dx, dy := float64(px)+0.5-cx, float64(py)+0.5-cy
			if dx*dx+dy*dy <= rr*rr {
				c.set(px, py, col)
			}
		}
	}
}

// line strokes a segment w wide with round caps, so joints stay smooth.
func (c *canvas) line(x0, y0, x1, y1, w float64, col color.RGBA) {
	steps := int(math.Hypot(x1-x0, y1-y0)*ss) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		c.disc(x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, col)
	}
}

func (c *canvas) hline(x0, x1, y, w float64, col color.RGBA) {
	r := image.Rect(int(x0*ss), int((y-w/2)*ss), int(x1*ss), int((y+w/2)*ss)+1)
	xdraw.Draw(c.img, r, image.NewUniform(col), image.Point{}, xdraw.Src)
}

func (c *canvas) dashed(x0, x1, y, w float64, col color.RGBA) {
	const dash, gap = 12, 8
	for x := x0; x < x1; x += dash + gap {
		c.hline(x, math.Min(x+dash, x1), y, w, col)
	}
}

func (c *canvas) set(x, y int, col color.RGBA) {
	if image.Pt(x, y).In(c.img.Rect) {
		c.img.SetRGBA(x, y, col)
	}
}

// text draws s with its baseline at (x, y).
func (c *canvas) text(f font.Face, x, y float64, col color.RGBA, s string) {
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: f,
		Dot: fixed.P(int(x*ss), int(y*ss))}
	d.DrawString(s)
}

// measure is s's advance width in final pixels.
func (c *canvas) measure(f font.Face, s string) float64 {
	return float64(font.MeasureString(f, s).Ceil()) / ss
}

// png scales the supersampled image down to the final size and encodes it.
func (c *canvas) png() ([]byte, error) {
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(out, out.Bounds(), c.img, c.img.Bounds(), xdraw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
