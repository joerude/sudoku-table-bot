package chart

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Every picture is drawn at ss times its final size and scaled down, which
// smooths lines and text without an anti-aliasing rasterizer. Nothing touches
// the disk: a render lives in memory until its PNG bytes are sent.
const ss = 2

// Light surface and ink, plus the categorical slots in their fixed order
// (validated palette: the first three slots stay distinct all-pairs, colour
// blindness included). Series take slots by a stable per-player order.
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

func slot(i int) color.RGBA { return slots[i%len(slots)] }

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// The fonts are parsed once per process; faces are cheap and not safe for
// concurrent use, so every render makes its own.
var (
	fontsOnce           sync.Once
	fontRegular, fontBd *opentype.Font
	fontsErr            error
)

func loadFonts() error {
	fontsOnce.Do(func() {
		if fontRegular, fontsErr = opentype.Parse(goregular.TTF); fontsErr != nil {
			return
		}
		fontBd, fontsErr = opentype.Parse(gobold.TTF)
	})
	return fontsErr
}

// faces holds the sizes one render needs, created on first use.
type faces struct{ m map[[2]int]font.Face }

func newFaces() (*faces, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}
	return &faces{m: map[[2]int]font.Face{}}, nil
}

// get returns the regular (bold=false) or bold face at size final pixels.
func (f *faces) get(size int, bold bool) font.Face {
	k := [2]int{size, 0}
	src := fontRegular
	if bold {
		k[1], src = 1, fontBd
	}
	if fc, ok := f.m[k]; ok {
		return fc
	}
	fc, err := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(size * ss), DPI: 72, Hinting: font.HintingFull})
	if err != nil { // a parsed font with a positive size cannot fail
		panic(err)
	}
	f.m[k] = fc
	return fc
}

func (f *faces) regular(size int) font.Face { return f.get(size, false) }
func (f *faces) bold(size int) font.Face    { return f.get(size, true) }

// canvas draws in final-pixel coordinates on a supersampled image.
type canvas struct {
	img  *image.RGBA
	w, h int
}

func newCanvas(w, h int) *canvas {
	c := &canvas{img: image.NewRGBA(image.Rect(0, 0, w*ss, h*ss)), w: w, h: h}
	c.rect(0, 0, float64(w), float64(h), surface)
	return c
}

func (c *canvas) set(x, y int, col color.RGBA) {
	if image.Pt(x, y).In(c.img.Rect) {
		c.img.SetRGBA(x, y, col)
	}
}

// rect fills the box from (x0, y0) to (x1, y1).
func (c *canvas) rect(x0, y0, x1, y1 float64, col color.RGBA) {
	r := image.Rect(int(x0*ss), int(y0*ss), int(math.Ceil(x1*ss)), int(math.Ceil(y1*ss)))
	xdraw.Draw(c.img, r, image.NewUniform(col), image.Point{}, xdraw.Src)
}

// roundRect fills a box with corners of radius r.
func (c *canvas) roundRect(x0, y0, x1, y1, r float64, col color.RGBA) {
	r = math.Min(r, math.Min(x1-x0, y1-y0)/2)
	c.rect(x0+r, y0, x1-r, y1, col)
	c.rect(x0, y0+r, x1, y1-r, col)
	for _, p := range [][2]float64{{x0 + r, y0 + r}, {x1 - r, y0 + r}, {x0 + r, y1 - r}, {x1 - r, y1 - r}} {
		c.disc(p[0], p[1], r, col)
	}
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
	c.rect(x0, y-w/2, x1, y+w/2, col)
}

func (c *canvas) dashed(x0, x1, y, w float64, col color.RGBA) {
	const dash, gap = 12, 8
	for x := x0; x < x1; x += dash + gap {
		c.hline(x, math.Min(x+dash, x1), y, w, col)
	}
}

// text draws s with its baseline at (x, y).
func (c *canvas) text(f font.Face, x, y float64, col color.RGBA, s string) {
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: f,
		Dot: fixed.P(int(x*ss), int(y*ss))}
	d.DrawString(s)
}

// textRight draws s ending at x.
func (c *canvas) textRight(f font.Face, x, y float64, col color.RGBA, s string) {
	c.text(f, x-measure(f, s), y, col, s)
}

// textCenter draws s centred on x.
func (c *canvas) textCenter(f font.Face, x, y float64, col color.RGBA, s string) {
	c.text(f, x-measure(f, s)/2, y, col, s)
}

// measure is s's advance width in final pixels.
func measure(f font.Face, s string) float64 {
	return float64(font.MeasureString(f, s).Ceil()) / ss
}

// png scales the supersampled image down to the final size and encodes it.
func (c *canvas) png() ([]byte, error) {
	out := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	xdraw.CatmullRom.Scale(out, out.Bounds(), c.img, c.img.Bounds(), xdraw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// clip cuts s to at most n runes, marking a cut with "…".
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
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
