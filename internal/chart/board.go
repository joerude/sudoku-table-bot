package chart

// Board is a sudoku's starting position for the picture posted at game start.
type Board struct {
	Code  string    // usdoku room code, e.g. "QWER"
	Tag   string    // e.g. "Easy · Original"
	Cells [9][9]int // 0 = empty
}

const (
	boardCell = 72.0
	boardPad  = 40.0
	boardHead = 120.0
	boardW    = boardPad*2 + boardCell*9
)

// boxShade tints every other 3×3 box so the boxes read at a glance; cell lines
// are a step darker than gridColor so they still show on the tint.
var (
	boxShade  = rgb(0xf1f0ec)
	cellLines = rgb(0xd6d5cf)
)

// RenderBoard draws the grid with its givens, headed by the room code.
func RenderBoard(b Board) ([]byte, error) {
	fs, err := newFaces()
	if err != nil {
		return nil, err
	}
	h := boardHead + boardCell*9 + boardPad
	c := newCanvas(int(boardW), int(h))

	c.text(fs.bold(52), boardPad, 78, inkMain, b.Code)
	if b.Tag != "" {
		c.textRight(fs.regular(24), boardW-boardPad, 76, inkMuted, b.Tag)
	}

	x0, y0 := boardPad, boardHead
	for r := 0; r < 9; r++ {
		for col := 0; col < 9; col++ {
			if (r/3+col/3)%2 == 1 {
				x, y := x0+float64(col)*boardCell, y0+float64(r)*boardCell
				c.rect(x, y, x+boardCell, y+boardCell, boxShade)
			}
		}
	}
	end := boardCell * 9
	for i := 0; i <= 9; i++ {
		w, col := 1.5, cellLines
		if i%3 == 0 {
			w, col = 3.5, inkMain
		}
		p := float64(i) * boardCell
		c.rect(x0+p-w/2, y0-w/2, x0+p+w/2, y0+end+w/2, col) // vertical
		c.hline(x0-w/2, x0+end+w/2, y0+p, w, col)
	}

	digit := fs.bold(40)
	for r, row := range b.Cells {
		for col, v := range row {
			if v < 1 || v > 9 {
				continue
			}
			cx := x0 + (float64(col)+0.5)*boardCell
			cy := y0 + (float64(r)+0.5)*boardCell
			c.textCenter(digit, cx, cy+14, inkMain, itoa(v)) // cap height of 40px Go Bold ≈ 28
		}
	}
	return c.png()
}
