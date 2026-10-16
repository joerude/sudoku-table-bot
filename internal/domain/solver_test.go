package domain

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// --- helpers ----------------------------------------------------------------

// solverParse reads 81 cells row by row; '.' or '0' is empty, whitespace is ignored.
func solverParse(t testing.TB, s string) [9][9]int {
	t.Helper()
	var b [9][9]int
	n := 0
	for _, ch := range s {
		switch {
		case ch == ' ' || ch == '\n' || ch == '\t':
			continue
		case ch == '.' || ch == '0':
			n++
		case ch >= '1' && ch <= '9':
			b[n/9][n%9] = int(ch - '0')
			n++
		default:
			t.Fatalf("bad puzzle char %q", ch)
		}
	}
	if n != 81 {
		t.Fatalf("puzzle has %d cells", n)
	}
	return b
}

// solverBoardOK checks a finished board independently of the solver: every row,
// column and box holds 1..9 once.
func solverBoardOK(b [9][9]int) bool {
	for k := 0; k < 9; k++ {
		var row, col, box uint16
		for j := 0; j < 9; j++ {
			row |= 1 << b[k][j]
			col |= 1 << b[j][k]
			box |= 1 << b[k/3*3+j/3][k%3*3+j%3]
		}
		if row != 0x3FE || col != 0x3FE || box != 0x3FE {
			return false
		}
	}
	return true
}

func keepsGivens(givens, board [9][9]int) bool {
	for r := range givens {
		for c := range givens[r] {
			if givens[r][c] != 0 && givens[r][c] != board[r][c] {
				return false
			}
		}
	}
	return true
}

// bruteSolve counts solutions of b up to limit by backtracking on the most
// constrained cell, and returns the first one it found. rng shuffles the digit
// order (nil = ascending), which is how random full grids are made.
func bruteSolve(b [9][9]int, limit int, rng *rand.Rand) (int, [9][9]int) {
	var rows, cols, boxes [9]uint16
	for r := 0; r < 9; r++ {
		for c := 0; c < 9; c++ {
			if v := b[r][c]; v != 0 {
				m := uint16(1) << v
				rows[r] |= m
				cols[c] |= m
				boxes[r/3*3+c/3] |= m
			}
		}
	}
	var first [9][9]int
	count := 0
	var rec func() bool
	rec = func() bool {
		br, bc, best, bestMask := -1, -1, 10, uint16(0)
		for r := 0; r < 9; r++ {
			for c := 0; c < 9; c++ {
				if b[r][c] != 0 {
					continue
				}
				free := 0x3FE &^ (rows[r] | cols[c] | boxes[r/3*3+c/3])
				if n := popCount(uint16(free)); n < best {
					br, bc, best, bestMask = r, c, n, uint16(free)
				}
			}
		}
		if br < 0 {
			if count == 0 {
				first = b
			}
			count++
			return count >= limit
		}
		digits := make([]int, 0, 9)
		for d := 1; d <= 9; d++ {
			if bestMask&(1<<d) != 0 {
				digits = append(digits, d)
			}
		}
		if rng != nil {
			rng.Shuffle(len(digits), func(i, j int) { digits[i], digits[j] = digits[j], digits[i] })
		}
		for _, d := range digits {
			m := uint16(1) << d
			b[br][bc] = d
			rows[br] |= m
			cols[bc] |= m
			boxes[br/3*3+bc/3] |= m
			stop := rec()
			b[br][bc] = 0
			rows[br] &^= m
			cols[bc] &^= m
			boxes[br/3*3+bc/3] &^= m
			if stop {
				return true
			}
		}
		return false
	}
	rec()
	return count, first
}

// genPuzzle makes a random puzzle with exactly one solution: a random full grid,
// then givens dropped one by one while the solution stays unique.
func genPuzzle(rng *rand.Rand) (puzzle, solution [9][9]int) {
	_, solution = bruteSolve([9][9]int{}, 1, rng)
	puzzle = solution
	for _, i := range rng.Perm(81) {
		keep := puzzle[i/9][i%9]
		puzzle[i/9][i%9] = 0
		if n, _ := bruteSolve(puzzle, 2, nil); n != 1 {
			puzzle[i/9][i%9] = keep
		}
	}
	return puzzle, solution
}

// cell is the index of 1-based row r, column c.
func cell(r, c int) int { return (r-1)*9 + c - 1 }

// fullGrid is a board with nothing placed and every digit still possible
// everywhere: tests carve a technique's picture out of it.
func fullGrid() *solverGrid {
	g := &solverGrid{}
	for i := range g.cand {
		g.cand[i] = allDigits
	}
	return g
}

// cands sets the exact candidates of one cell.
func (g *solverGrid) cands(r, c int, digits ...int) {
	var m uint16
	for _, d := range digits {
		m |= digitBit(d)
	}
	g.cand[cell(r, c)] = m
}

// drop removes digit d from every listed cell.
func (g *solverGrid) drop(d int, cells []int) {
	for _, c := range cells {
		g.cand[c] &^= digitBit(d)
	}
}

func rowCells(r int) (out []int) {
	for c := 1; c <= 9; c++ {
		out = append(out, cell(r, c))
	}
	return out
}

func colCells(c int) (out []int) {
	for r := 1; r <= 9; r++ {
		out = append(out, cell(r, c))
	}
	return out
}

func boxCells(b int) (out []int) { // box 1..9
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			out = append(out, cell((b-1)/3*3+r+1, (b-1)%3*3+c+1))
		}
	}
	return out
}

// except filters the listed cells out.
func except(cells []int, skip ...int) (out []int) {
	for _, c := range cells {
		if !contains(skip, c) {
			out = append(out, c)
		}
	}
	return out
}

func cellName(i int) string { return fmt.Sprintf("r%dc%d", i/9+1, i%9+1) }

// removed lists every candidate that went between two states as "r1c3-5".
func removed(before, after *solverGrid) []string {
	var out []string
	for i := range before.cand {
		lost := before.cand[i] &^ after.cand[i]
		for d := 1; d <= 9; d++ {
			if lost&digitBit(d) != 0 {
				out = append(out, fmt.Sprintf("%s-%d", cellName(i), d))
			}
		}
	}
	sort.Strings(out)
	return out
}

// gone builds the expected removals of digit d from the cells.
func gone(d int, cells ...int) []string {
	var out []string
	for _, c := range cells {
		out = append(out, fmt.Sprintf("%s-%d", cellName(c), d))
	}
	return out
}

func joinGone(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	sort.Strings(out)
	return out
}

// expectStep runs one solver step on g and checks which technique fired (so no
// cheaper one applies) and exactly which candidates it removed.
func expectStep(t *testing.T, g *solverGrid, wantKey string, want []string) {
	t.Helper()
	before := *g
	key, ok := g.step()
	if !ok || key != wantKey {
		t.Fatalf("step = %q (applied %v), want %q", key, ok, wantKey)
	}
	sort.Strings(want)
	if got := removed(&before, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("removed\n got %v\nwant %v", got, want)
	}
}

// --- registry ---------------------------------------------------------------

func TestSolverCoversWholeDictionary(t *testing.T) {
	if len(solverSteps) != len(techniques) {
		t.Fatalf("%d steps for %d techniques", len(solverSteps), len(techniques))
	}
	for i, s := range solverSteps {
		if s.key != techniques[i].Key {
			t.Errorf("step %d is %q, learn.go has %q", i, s.key, techniques[i].Key)
		}
	}
	for key := range solverImpl {
		if _, ok := TechniqueByKey(key); !ok {
			t.Errorf("solver key %q is not in learn.go", key)
		}
	}
}

func TestGradingHardestAndKeys(t *testing.T) {
	g := Grading{Used: map[string]int{"y-wing": 1, "hidden-single": 4, "obvious-pair": 2, "x-wing": 0}}
	if got := g.Hardest(); got != "y-wing" {
		t.Errorf("Hardest = %q", got)
	}
	want := []string{"hidden-single", "obvious-pair", "y-wing"}
	if got := g.Keys(); !reflect.DeepEqual(got, want) {
		t.Errorf("Keys = %v, want %v", got, want)
	}
	empty := Grading{}
	if empty.Hardest() != "" || len(empty.Keys()) != 0 {
		t.Errorf("empty grading: %q %v", empty.Hardest(), empty.Keys())
	}
}

// --- one technique at a time ------------------------------------------------

func TestStepObviousSingle(t *testing.T) {
	g := fullGrid()
	g.cands(1, 1, 5)
	before := *g
	key, ok := g.step()
	if !ok || key != "obvious-single" {
		t.Fatalf("step = %q %v", key, ok)
	}
	if !g.placed[cell(1, 1)] || digitOf(g.cand[cell(1, 1)]) != 5 {
		t.Fatalf("5 not placed in r1c1")
	}
	var peers []int
	for _, c := range append(append(rowCells(1), colCells(1)...), boxCells(1)...) {
		if c != cell(1, 1) && !contains(peers, c) {
			peers = append(peers, c)
		}
	}
	if len(peers) != 20 {
		t.Fatalf("test bug: %d peers", len(peers))
	}
	if got, want := removed(&before, g), joinGone(gone(5, peers...)); !reflect.DeepEqual(got, want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
}

func TestStepHiddenSingle(t *testing.T) {
	g := fullGrid()
	g.drop(7, except(rowCells(1), cell(1, 4)))
	key, ok := g.step()
	if !ok || key != "hidden-single" {
		t.Fatalf("step = %q %v", key, ok)
	}
	if !g.placed[cell(1, 4)] || digitOf(g.cand[cell(1, 4)]) != 7 {
		t.Fatalf("7 not placed in r1c4")
	}
}

func TestStepObviousPair(t *testing.T) {
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 5, 1, 2)
	rest := except(rowCells(1), cell(1, 1), cell(1, 5))
	expectStep(t, g, "obvious-pair", joinGone(gone(1, rest...), gone(2, rest...)))
}

func TestStepObviousPairCleansRowAndBox(t *testing.T) {
	// both cells share a row and a box: one application cleans both
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 2, 1, 2)
	var rest []int
	for _, c := range append(rowCells(1), boxCells(1)...) {
		if c != cell(1, 1) && c != cell(1, 2) && !contains(rest, c) {
			rest = append(rest, c)
		}
	}
	expectStep(t, g, "obvious-pair", joinGone(gone(1, rest...), gone(2, rest...)))
}

func TestStepHiddenPair(t *testing.T) {
	g := fullGrid()
	other := except(rowCells(1), cell(1, 2), cell(1, 6))
	g.drop(3, other)
	g.drop(4, other)
	var want []string
	for _, d := range []int{1, 2, 5, 6, 7, 8, 9} {
		want = append(want, gone(d, cell(1, 2), cell(1, 6))...)
	}
	expectStep(t, g, "hidden-pair", want)
}

func TestStepPointingPairRow(t *testing.T) {
	g := fullGrid()
	g.drop(5, except(boxCells(1), cell(1, 1), cell(1, 2)))
	expectStep(t, g, "pointing-pair", gone(5, cell(1, 4), cell(1, 5), cell(1, 6), cell(1, 7), cell(1, 8), cell(1, 9)))
}

func TestStepPointingPairColumn(t *testing.T) {
	g := fullGrid()
	g.drop(5, except(boxCells(5), cell(4, 5), cell(5, 5)))
	expectStep(t, g, "pointing-pair", gone(5, cell(1, 5), cell(2, 5), cell(3, 5), cell(7, 5), cell(8, 5), cell(9, 5)))
}

func TestStepPointingTriple(t *testing.T) {
	g := fullGrid()
	g.drop(5, except(boxCells(1), cell(1, 1), cell(1, 2), cell(1, 3)))
	expectStep(t, g, "pointing-triple", gone(5, cell(1, 4), cell(1, 5), cell(1, 6), cell(1, 7), cell(1, 8), cell(1, 9)))

	g = fullGrid()
	g.drop(5, except(boxCells(9), cell(7, 9), cell(8, 9), cell(9, 9)))
	expectStep(t, g, "pointing-triple", gone(5, cell(1, 9), cell(2, 9), cell(3, 9), cell(4, 9), cell(5, 9), cell(6, 9)))
}

func TestStepObviousTriple(t *testing.T) {
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 4, 2, 3)
	g.cands(1, 7, 1, 3)
	rest := except(rowCells(1), cell(1, 1), cell(1, 4), cell(1, 7))
	expectStep(t, g, "obvious-triple", joinGone(gone(1, rest...), gone(2, rest...), gone(3, rest...)))
}

func TestStepHiddenTriple(t *testing.T) {
	g := fullGrid()
	trio := []int{cell(1, 1), cell(1, 4), cell(1, 7)}
	for _, d := range []int{1, 2, 3} {
		g.drop(d, except(rowCells(1), trio...))
	}
	var want []string
	for _, d := range []int{4, 5, 6, 7, 8, 9} {
		want = append(want, gone(d, trio...)...)
	}
	expectStep(t, g, "hidden-triple", want)
}

func TestStepBoxLine(t *testing.T) {
	g := fullGrid()
	g.drop(5, except(rowCells(1), cell(1, 1), cell(1, 2)))
	expectStep(t, g, "box-line", gone(5,
		cell(2, 1), cell(2, 2), cell(2, 3), cell(3, 1), cell(3, 2), cell(3, 3)))

	g = fullGrid()
	g.drop(5, except(colCells(9), cell(7, 9), cell(8, 9)))
	expectStep(t, g, "box-line", gone(5,
		cell(7, 7), cell(7, 8), cell(8, 7), cell(8, 8), cell(9, 7), cell(9, 8)))
}

func TestStepXWing(t *testing.T) {
	// 1 sits only in columns 3 and 7 of rows 2 and 6
	g := fullGrid()
	for _, r := range []int{2, 6} {
		g.drop(1, except(rowCells(r), cell(r, 3), cell(r, 7)))
	}
	var hit []int
	for _, r := range []int{1, 3, 4, 5, 7, 8, 9} {
		hit = append(hit, cell(r, 3), cell(r, 7))
	}
	expectStep(t, g, "x-wing", gone(1, hit...))

	// the same turned on its side: columns 2 and 6, rows 3 and 7
	g = fullGrid()
	for _, c := range []int{2, 6} {
		g.drop(1, except(colCells(c), cell(3, c), cell(7, c)))
	}
	hit = nil
	for _, c := range []int{1, 3, 4, 5, 7, 8, 9} {
		hit = append(hit, cell(3, c), cell(7, c))
	}
	expectStep(t, g, "x-wing", gone(1, hit...))
}

func TestStepSwordfish(t *testing.T) {
	g := fullGrid()
	g.drop(1, except(rowCells(2), cell(2, 1), cell(2, 4)))
	g.drop(1, except(rowCells(5), cell(5, 4), cell(5, 7)))
	g.drop(1, except(rowCells(8), cell(8, 1), cell(8, 7)))
	var hit []int
	for _, r := range []int{1, 3, 4, 6, 7, 9} {
		hit = append(hit, cell(r, 1), cell(r, 4), cell(r, 7))
	}
	expectStep(t, g, "swordfish", gone(1, hit...))
}

func TestStepJellyfish(t *testing.T) {
	g := fullGrid()
	g.drop(1, except(rowCells(1), cell(1, 2), cell(1, 4)))
	g.drop(1, except(rowCells(3), cell(3, 4), cell(3, 8)))
	g.drop(1, except(rowCells(5), cell(5, 8), cell(5, 6)))
	g.drop(1, except(rowCells(7), cell(7, 6), cell(7, 2)))
	var hit []int
	for _, r := range []int{2, 4, 6, 8, 9} {
		for _, c := range []int{2, 4, 6, 8} {
			hit = append(hit, cell(r, c))
		}
	}
	expectStep(t, g, "jellyfish", gone(1, hit...))
}

func TestStepYWing(t *testing.T) {
	// pivot r1c1 {1,2}, pincers r1c5 {1,3} and r5c1 {2,3}: only r5c5 sees both
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 5, 1, 3)
	g.cands(5, 1, 2, 3)
	expectStep(t, g, "y-wing", gone(3, cell(5, 5)))

	// pincer in the pivot's box: five cells see both
	g = fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 5, 1, 3)
	g.cands(2, 2, 2, 3)
	expectStep(t, g, "y-wing", gone(3, cell(1, 2), cell(1, 3), cell(2, 4), cell(2, 5), cell(2, 6)))
}

func TestStepXYZWing(t *testing.T) {
	// pivot r1c1 {1,2,3}, pincers r1c5 {1,3} and r2c2 {2,3}: z = 3
	g := fullGrid()
	g.cands(1, 1, 1, 2, 3)
	g.cands(1, 5, 1, 3)
	g.cands(2, 2, 2, 3)
	expectStep(t, g, "xyz-wing", gone(3, cell(1, 2), cell(1, 3)))
}

func TestStepSimpleColoringTrap(t *testing.T) {
	// chain for digit 1: r1c1 = r5c1 (column 1), r5c1 = r4c2 (box 4),
	// r4c2 = r4c8 (row 4). r1c1 and r4c8 have opposite colors, r1c8 sees both.
	g := fullGrid()
	g.drop(1, except(colCells(1), cell(1, 1), cell(5, 1)))
	g.drop(1, except(boxCells(4), cell(5, 1), cell(4, 2)))
	g.drop(1, except(rowCells(4), cell(4, 2), cell(4, 8)))
	expectStep(t, g, "simple-coloring", gone(1, cell(1, 8)))
}

func TestStepSimpleColoringWrap(t *testing.T) {
	// chain r1c1 = r5c1 = r5c6 = r2c6 = r2c2: its two ends share box 1, so their
	// color is false and r1c1, r5c6 and r2c2 lose the digit
	g := fullGrid()
	g.drop(1, except(colCells(1), cell(1, 1), cell(5, 1)))
	g.drop(1, except(rowCells(5), cell(5, 1), cell(5, 6)))
	g.drop(1, except(colCells(6), cell(5, 6), cell(2, 6)))
	g.drop(1, except(rowCells(2), cell(2, 6), cell(2, 2)))
	expectStep(t, g, "simple-coloring", gone(1, cell(1, 1), cell(5, 6), cell(2, 2)))
}

func TestStepWXYZWing(t *testing.T) {
	// r1c1 {1,2}, r1c2 {1,3}, r2c1 {2,3,4}, r1c9 {1,4}: 1, 2, 3 are each held by
	// cells that all see one another, 4 is not (r2c1 and r1c9 do not see each other)
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 2, 1, 3)
	g.cands(2, 1, 2, 3, 4)
	g.cands(1, 9, 1, 4)
	expectStep(t, g, "wxyz-wing", gone(4, cell(1, 3), cell(2, 7), cell(2, 8), cell(2, 9)))
}

func TestStepXYChain(t *testing.T) {
	// r1c1 {1,2} - r1c5 {2,3} - r5c5 {3,4} - r5c9 {4,5} - r9c9 {5,1}: one end is 1
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 5, 2, 3)
	g.cands(5, 5, 3, 4)
	g.cands(5, 9, 4, 5)
	g.cands(9, 9, 5, 1)
	expectStep(t, g, "xy-chain", gone(1, cell(1, 9), cell(9, 1)))
}

// uniqueRectGrid is the rectangle r1/r2 x c1/c4 with the pair 1,2 already
// cleaned from the places a naked pair would clean it from.
func uniqueRectGrid() *solverGrid {
	g := fullGrid()
	g.cands(1, 1, 1, 2)
	g.cands(1, 4, 1, 2)
	return g
}

func TestStepUniqueRectType1(t *testing.T) {
	g := uniqueRectGrid()
	g.cands(2, 1, 1, 2)
	g.cands(2, 4, 1, 2, 3, 4)
	for _, d := range []int{1, 2} {
		g.drop(d, except(rowCells(1), cell(1, 1), cell(1, 4)))
		g.drop(d, except(colCells(1), cell(1, 1), cell(2, 1)))
		g.drop(d, except(boxCells(1), cell(1, 1), cell(2, 1)))
	}
	expectStep(t, g, "unique-rect", joinGone(gone(1, cell(2, 4)), gone(2, cell(2, 4))))
}

func TestStepUniqueRectType2(t *testing.T) {
	// roofs r2c1 and r2c4 both {1,2,3}: one of them is 3
	g := uniqueRectGrid()
	g.cands(2, 1, 1, 2, 3)
	g.cands(2, 4, 1, 2, 3)
	for _, d := range []int{1, 2} {
		g.drop(d, except(rowCells(1), cell(1, 1), cell(1, 4)))
	}
	expectStep(t, g, "unique-rect", gone(3,
		cell(2, 2), cell(2, 3), cell(2, 5), cell(2, 6), cell(2, 7), cell(2, 8), cell(2, 9)))
}

func TestStepUniqueRectType4(t *testing.T) {
	// floors r1c4 and r2c4 are {1,2}; roofs r1c1 {1,2,5} and r2c1 {1,2,6} share
	// column 1 where 1 fits only them, so one of them is 1 and neither may be 2
	g := fullGrid()
	g.cands(1, 4, 1, 2)
	g.cands(2, 4, 1, 2)
	g.cands(1, 1, 1, 2, 5)
	g.cands(2, 1, 1, 2, 6)
	// the rest of the picture is already settled, so only the rectangle is left
	// to act on: no X-Wing or pointing pair has anything to remove
	g.drop(1, except(colCells(1), cell(1, 1), cell(2, 1)))
	g.drop(1, except(rowCells(1), cell(1, 1), cell(1, 4)))
	g.drop(1, except(rowCells(2), cell(2, 1), cell(2, 4)))
	g.drop(1, except(rowCells(3), cell(3, 7), cell(3, 8)))
	for _, d := range []int{1, 2} {
		g.drop(d, except(colCells(4), cell(1, 4), cell(2, 4)))
		g.drop(d, except(boxCells(2), cell(1, 4), cell(2, 4)))
	}
	expectStep(t, g, "unique-rect", gone(2, cell(1, 1), cell(2, 1)))
}

// --- whole puzzles ----------------------------------------------------------

type gradedPuzzle struct {
	name     string
	puzzle   string
	solved   bool
	hardest  string   // "" = don't check
	onlyUsed []string // if set, Used keys must be a subset of these
}

// gradedPuzzles are unique puzzles with the grade this solver gives them. The
// first two come from usdoku and sudokuwiki; the ones named after a technique
// are random puzzles picked because that technique is the hardest thing they
// need, so each also pins how the order in learn.go plays out end to end. The
// step tests above are what show each technique removes the right candidates.
var gradedPuzzles = []gradedPuzzle{
	{
		name: "usdoku easy",
		puzzle: `...69...3
		         ..518...4
		         7.....8.9
		         3.8......
		         .9..4..8.
		         ......2.1
		         5.3.....8
		         8...691..
		         4...38...`,
		solved:   true,
		hardest:  "hidden-single",
		onlyUsed: []string{"obvious-single", "hidden-single"},
	},
	{name: "sudokuwiki x-wing", solved: true, hardest: "x-wing",
		puzzle: "100000569492056108056109240009640801064010000218035604040500016905061402621000005"},
	{name: "x-wing", solved: true, hardest: "x-wing",
		puzzle: "4.63..8..8.........5.....1.6.7..9......81.....8..5.62...4......2..7..4.3......28."},
	{name: "hidden triple", solved: true, hardest: "hidden-triple",
		puzzle: "4...7.8..7......2.8.6.....9......23.67.4...1.5.9.2146.....4........123.....9....1"},
	{name: "y-wing", solved: true, hardest: "y-wing",
		puzzle:   ".1.....98.3.......4...3..5....1.8..9.....9.1.6....7.3.....5..2...76.2...5..9..4..",
		onlyUsed: []string{"obvious-single", "hidden-single", "y-wing"}},
	{name: "swordfish", solved: true, hardest: "swordfish",
		puzzle: ".2.8..9...45..9.........8........2...6..54...21......6...9.16.77..4...1.4...3..9."},
	{name: "xyz-wing", solved: true, hardest: "xyz-wing",
		puzzle: "2..6.9..1..9.....8......7.6.1.4..6......8.....5.1......4..........2.6..3.83.5..29"},
	{name: "jellyfish", solved: true, hardest: "jellyfish",
		puzzle: "8..6....53....5.2.1.29.7.......62.8........14...3..5........6.2.6.1.37....7.5...."},
	{name: "simple coloring", solved: true, hardest: "simple-coloring",
		puzzle: "87..3...9..52..........9.7.68....2.5.....34....9.6....4.351.....1..2.........8..."},
	{name: "wxyz-wing", solved: true, hardest: "wxyz-wing",
		puzzle: "...67...5.4653..1......1......2....1.91....54..3...7..........762.....3.1..4....8"},
	{name: "xy-chain", solved: true, hardest: "xy-chain",
		puzzle: "5..34............2....598.7..71...3........74....8.9.6..6.13.2.79.......4.......3"},
	{name: "unique rectangle", solved: true, hardest: "unique-rect",
		puzzle: ".......3.1..2....5..4.13.............178...4.68......1.523.67...4..7..628...9.3.."},
	// stuck: nothing implemented applies at all, or only a little before it runs dry
	{name: "easter monster", solved: false,
		puzzle: "1.......2.9.4...5...6...7...5.9.3.......7.......85..4.7.....6...3...9.8...2.....1"},
	{name: "inkala 2012", solved: false,
		puzzle: "8..........36......7..9.2...5...7.......457.....1...3...1....68..85...1..9....4.."},
	{name: "stuck after a few moves", solved: false,
		puzzle: "......6.33....5...15.2...4..91......8..69..3....4..5...2.9..415....2........4.2.8"},
}

func TestGradePuzzles(t *testing.T) {
	for _, tc := range gradedPuzzles {
		t.Run(tc.name, func(t *testing.T) {
			givens := solverParse(t, tc.puzzle)
			n, solution := bruteSolve(givens, 2, nil)
			if n != 1 {
				t.Fatalf("test puzzle has %d solutions", n)
			}
			board, gr := solvePuzzle(givens)
			t.Logf("solved=%v hardest=%q used=%v", gr.Solved, gr.Hardest(), gr.Used)
			if gr.Solved != tc.solved {
				t.Fatalf("Solved = %v, want %v", gr.Solved, tc.solved)
			}
			if !keepsGivens(givens, board) {
				t.Fatalf("board lost a given:\n%v", board)
			}
			if gr.Solved && !solverBoardOK(board) {
				t.Fatalf("solved board is not a valid sudoku:\n%v", board)
			}
			for r := range board {
				for c := range board[r] {
					if board[r][c] != 0 && board[r][c] != solution[r][c] {
						t.Fatalf("r%dc%d = %d, solution has %d", r+1, c+1, board[r][c], solution[r][c])
					}
				}
			}
			if tc.hardest != "" && gr.Hardest() != tc.hardest {
				t.Errorf("Hardest = %q, want %q", gr.Hardest(), tc.hardest)
			}
			if tc.hardest != "" && gr.Used[tc.hardest] == 0 {
				t.Errorf("%s not used", tc.hardest)
			}
			if tc.onlyUsed != nil {
				for k := range gr.Used {
					if !hasKey(tc.onlyUsed, k) {
						t.Errorf("unexpected technique %s", k)
					}
				}
			}
			if again := GradePuzzle(givens); !reflect.DeepEqual(again, gr) {
				t.Errorf("GradePuzzle differs from solvePuzzle: %v vs %v", again, gr)
			}
			replaySound(t, givens, solution, map[string]int{})
		})
	}
}

func hasKey(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// --- invalid input ----------------------------------------------------------

// unsolvableVariant changes the first given of b that can take another digit
// without a visible clash while leaving the puzzle with no solution.
func unsolvableVariant(t *testing.T, b [9][9]int) [9][9]int {
	t.Helper()
	for i := 0; i < 81; i++ {
		if b[i/9][i%9] == 0 {
			continue
		}
		for d := 1; d <= 9; d++ {
			v := b
			v[i/9][i%9] = d
			g, ok := newSolverGrid(v)
			if d == b[i/9][i%9] || !ok || !g.valid() {
				continue
			}
			if n, _ := bruteSolve(v, 1, nil); n == 0 {
				return v
			}
		}
	}
	t.Fatal("no unsolvable variant found")
	return b
}

func TestGradeInvalidPuzzles(t *testing.T) {
	easy := solverParse(t, gradedPuzzles[0].puzzle)

	dupRow := easy
	dupRow[0][0], dupRow[0][1] = 5, 5

	dupBox := [9][9]int{}
	dupBox[0][0], dupBox[1][1] = 4, 4

	bigDigit := easy
	bigDigit[0][0] = 10

	negDigit := easy
	negDigit[0][0] = -1

	// r1c9 sees 1..8 in its row and a 9 in its column: no candidate left
	deadCell := [9][9]int{}
	for c := 0; c < 8; c++ {
		deadCell[0][c] = c + 1
	}
	deadCell[4][8] = 9

	// a solved grid with two cells swapped is complete and wrong
	full := solverParse(t, "534678912672195348198342567859761423426853791713924856961537284287419635345286179")
	badFull := full
	badFull[0][0], badFull[0][1] = badFull[0][1], badFull[0][0]

	// one given changed so that nothing clashes on sight but no solution is left
	flipped := unsolvableVariant(t, easy)

	for name, b := range map[string][9][9]int{
		"duplicate in row": dupRow, "duplicate in box": dupBox,
		"digit above nine": bigDigit, "negative digit": negDigit,
		"cell without candidates": deadCell,
		"complete but wrong":      badFull, "flipped given": flipped,
	} {
		t.Run(name, func(t *testing.T) {
			gr := GradePuzzle(b)
			if gr.Solved {
				t.Fatalf("Solved = true for an invalid puzzle")
			}
			if gr.Used == nil {
				t.Errorf("Used is nil")
			}
		})
	}

	// valid() is what catches contradictions that arise mid-solve
	g := fullGrid()
	if !g.valid() {
		t.Errorf("open grid reported invalid")
	}
	g.drop(9, rowCells(1))
	if g.valid() {
		t.Errorf("row without a place for 9 reported valid")
	}
	g = fullGrid()
	g.cands(3, 3)
	if g.valid() {
		t.Errorf("cell without candidates reported valid")
	}

	// the empty grid is not invalid, just far too open
	if gr := GradePuzzle([9][9]int{}); gr.Solved {
		t.Errorf("empty grid reported solved")
	}

	// a finished valid grid needs no technique
	gr := GradePuzzle(full)
	if !gr.Solved || len(gr.Used) != 0 || gr.Hardest() != "" {
		t.Errorf("complete grid: %+v", gr)
	}
}

// --- soundness over random puzzles ------------------------------------------

// replaySound runs the solver step by step and checks after every step that the
// true digit is still a candidate everywhere: a wrong elimination in any
// technique shows up here, however rarely it fires. It adds each application to
// used and reports whether the puzzle got solved.
func replaySound(t *testing.T, puzzle, solution [9][9]int, used map[string]int) bool {
	t.Helper()
	g, ok := newSolverGrid(puzzle)
	if !ok {
		t.Fatalf("puzzle rejected: %s", solverShow(puzzle))
	}
	for !g.complete() {
		key, ok := g.step()
		if !ok {
			return false
		}
		used[key]++
		for i := range g.cand {
			if g.cand[i]&digitBit(solution[i/9][i%9]) == 0 {
				t.Fatalf("%s removed the true digit %d from %s\n%s",
					key, solution[i/9][i%9], cellName(i), solverShow(puzzle))
			}
		}
		if !g.valid() {
			t.Fatalf("%s left a contradiction\n%s", key, solverShow(puzzle))
		}
	}
	if g.board() != solution {
		t.Fatalf("solved to a different board\n%s", solverShow(puzzle))
	}
	return true
}

func TestSolverNeverDropsTheSolution(t *testing.T) {
	n := 150
	if testing.Short() {
		n = 20
	}
	rng := rand.New(rand.NewSource(20260716))
	used := map[string]int{}
	solved := 0
	for p := 0; p < n; p++ {
		puzzle, solution := genPuzzle(rng)
		if replaySound(t, puzzle, solution, used) {
			solved++
		}
	}
	t.Logf("%d/%d solved, applications by technique: %v", solved, n, used)
}

// solverShow prints a puzzle as 81 chars for a failure message.
func solverShow(b [9][9]int) string {
	var sb strings.Builder
	for _, row := range b {
		for _, v := range row {
			if v == 0 {
				sb.WriteByte('.')
			} else {
				sb.WriteByte(byte('0' + v))
			}
		}
	}
	return sb.String()
}
