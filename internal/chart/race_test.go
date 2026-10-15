package chart

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if got := img.Bounds().Size(); got != image.Pt(width, height) {
		t.Fatalf("size = %v, want %dx%d", got, width, height)
	}
	return img
}

// has reports whether some pixel is (nearly) exactly col: line interiors keep
// their colour after the downscale.
func has(img image.Image, col color.RGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			if near(r, col.R) && near(g, col.G) && near(bb, col.B) {
				return true
			}
		}
	}
	return false
}

func near(v uint32, c uint8) bool {
	d := int(v>>8) - int(c)
	return d > -4 && d < 4
}

func TestRenderRaceDrawsEverySeries(t *testing.T) {
	r := Race{Title: "Сезон 6 · гонка до 100", Target: 100, Series: []Series{
		{Name: "mister", Points: []int{3, 6, 7, 10, 13, 13, 16}},
		{Name: "Nur", Points: []int{1, 1, 4, 5, 5, 8, 9}},
		{Name: "Joe Rude", Points: []int{0, 3, 3, 3, 4, 5, 5}},
	}}
	b, err := RenderRace(r)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, b)
	for i := range r.Series {
		if !has(img, slots[i]) {
			t.Errorf("series %d colour %v not drawn", i, slots[i])
		}
	}
	if out := os.Getenv("CHART_OUT"); out != "" { // eyeball: CHART_OUT=/tmp/x.png go test
		_ = os.WriteFile(out, b, 0o644)
	}
}

func TestRenderRaceNoGames(t *testing.T) {
	b, err := RenderRace(Race{Title: "Сезон 1", Target: 100, Series: []Series{{Name: "A"}, {Name: "B"}}})
	if err != nil {
		t.Fatal(err)
	}
	decode(t, b)
}

func TestNiceStep(t *testing.T) {
	for _, c := range []struct{ span, n, want int }{
		{100, 6, 20}, {42, 8, 5}, {7, 8, 1}, {0, 8, 1}, {250, 6, 50},
	} {
		if got := niceStep(c.span, c.n); got != c.want {
			t.Errorf("niceStep(%d, %d) = %d, want %d", c.span, c.n, got, c.want)
		}
	}
}
