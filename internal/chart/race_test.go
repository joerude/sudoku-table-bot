package chart

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// save writes a render to $CHART_DIR/<name>.png when set, to eyeball it.
func save(t *testing.T, name string, b []byte) {
	t.Helper()
	if dir := os.Getenv("CHART_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".png"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func decodeSize(t *testing.T, b []byte, w, h int) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if got := img.Bounds().Size(); got != image.Pt(w, h) {
		t.Fatalf("size = %v, want %dx%d", got, w, h)
	}
	return img
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if got := img.Bounds().Size(); got != image.Pt(raceW, raceH) {
		t.Fatalf("size = %v, want %dx%d", got, raceW, raceH)
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
	save(t, "race", b)
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

func TestRenderRating(t *testing.T) {
	r := Rating{Title: "Рейтинг Эло", Subtitle: "все игры, включая дуэли · старт 1000", Games: 12, Series: []Series{
		{Name: "mister", Base: 1000, Points: []int{1032, 1018, 1050, 1066, 1041, 1072, 1090, 1081, 1102, 1120, 1111, 1134}},
		{Name: "Nur", Base: 1000, Points: []int{968, 990, 975, 960, 984, 970, 958, 977, 990, 972, 985, 970}},
		{Name: "Joe Rude", Offset: 4, Base: 1000, Points: []int{1016, 1003, 995, 1012, 1001, 990, 978, 966}},
	}}
	b, err := RenderRating(r)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeSize(t, b, raceW, raceH)
	for i := range r.Series {
		if !has(img, slots[i]) {
			t.Errorf("series %d colour not drawn", i)
		}
	}
	save(t, "rating", b)
}

func TestYRangeFitsDataOrStartsAtZero(t *testing.T) {
	s := []Series{{Base: 1000, Points: []int{1040, 960}}}
	if lo, hi := yRange(lineSpec{series: s}); lo != 960 || hi != 1040 {
		t.Errorf("fit: got %d..%d", lo, hi)
	}
	if lo, hi := yRange(lineSpec{series: []Series{{Points: []int{3, 40}}}, goal: 100, fromZero: true}); lo != 0 || hi != 100 {
		t.Errorf("fromZero: got %d..%d", lo, hi)
	}
}

func TestRenderCalendar(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	counts := map[string]int{}
	for d := 21; d <= 30; d++ {
		counts[day(time.September, d).Format(dayKey)] = d % 4
	}
	for d := 1; d <= 15; d++ {
		counts[day(time.October, d).Format(dayKey)] = (d * 7) % 6
	}
	cal := Calendar{
		Title: "Сезон 6 · игры по дням", Subtitle: "42 из 66 игр · темп 1.7/день · нужно 0.7/день",
		Start: day(time.September, 21), End: day(time.November, 20), Today: day(time.October, 15),
		Counts: counts, Mark: day(time.November, 20), MarkLbl: "последний день",
	}
	b, err := RenderCalendar(cal)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 1080 {
		t.Errorf("width = %d", img.Bounds().Dx())
	}
	for _, col := range dayRamp {
		if !has(img, col) {
			t.Errorf("ramp colour %v not drawn", col)
		}
	}
	save(t, "calendar", b)
}

func TestRenderPoster(t *testing.T) {
	p := Poster{
		Season: 5, Champion: "mister", Points: 100,
		Standings: []PosterRow{{"mister", 100, 30}, {"Nur", 81, 22}, {"Joe Rude", 64, 18}},
		Awards: []Award{
			{"Больше всех побед", "mister · 30"},
			{"Быстрейшее решение", "Nur · 3:41 (medium)"},
			{"Серия побед", "Joe Rude · 6 подряд"},
		},
		Race: Race{Target: 100, Series: []Series{
			{Name: "mister", Points: []int{3, 4, 7, 10, 13, 13, 16, 19, 22, 25}},
			{Name: "Nur", Points: []int{1, 4, 5, 5, 8, 11, 12, 12, 15, 16}},
			{Name: "Joe Rude", Points: []int{0, 0, 1, 4, 4, 5, 5, 8, 9, 10}},
		}},
		Footer: "66 игр · 21 сен → 20 ноя",
	}
	b, err := RenderPoster(p)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if !has(img, medals[0]) || !has(img, medals[1]) || !has(img, medals[2]) {
		t.Errorf("medals not drawn")
	}
	save(t, "poster", b)
}

func TestPointsWord(t *testing.T) {
	for n, want := range map[int]string{1: "очко", 2: "очка", 5: "очков", 11: "очков", 21: "очко", 100: "очков", 104: "очка"} {
		if got := pointsWord(n); got != want {
			t.Errorf("pointsWord(%d) = %s, want %s", n, got, want)
		}
	}
}
