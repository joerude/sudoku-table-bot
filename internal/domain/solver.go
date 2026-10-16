package domain

import "math/bits"

// Grading is what solving a puzzle by hand takes.
type Grading struct {
	Used   map[string]int // technique Key → times applied (keys from learn.go)
	Solved bool           // false = stuck: needs something beyond the implemented set, or the puzzle is invalid
}

// Hardest returns the key of the hardest technique used, by learn.go order (the
// order of the techniques slice), "" if none.
func (g Grading) Hardest() string {
	best, bestRank := "", -1
	for key, n := range g.Used {
		if r, ok := techniqueRank[key]; ok && n > 0 && r > bestRank {
			best, bestRank = key, r
		}
	}
	return best
}

// Keys returns the used keys in learn.go order.
func (g Grading) Keys() []string {
	var out []string
	for _, t := range techniques {
		if g.Used[t.Key] > 0 {
			out = append(out, t.Key)
		}
	}
	return out
}

// GradePuzzle solves givens (0 = empty, rows then columns, 0-based) with human
// techniques.
func GradePuzzle(givens [9][9]int) Grading {
	_, gr := solvePuzzle(givens)
	return gr
}

// techniqueRank is a key's position in the learn.go dictionary: the difficulty
// order, so the solver never keeps a second list of technique names.
var techniqueRank = func() map[string]int {
	m := make(map[string]int, len(techniques))
	for i, t := range techniques {
		m[t.Key] = i
	}
	return m
}()

// solvePuzzle is GradePuzzle plus the board it reached: the full solution when
// Solved, otherwise whatever got placed before it got stuck (all zeros for an
// invalid puzzle).
func solvePuzzle(givens [9][9]int) ([9][9]int, Grading) {
	gr := Grading{Used: map[string]int{}}
	g, ok := newSolverGrid(givens)
	if !ok || !g.valid() {
		return [9][9]int{}, gr
	}
	for !g.complete() {
		key, ok := g.step()
		if !ok {
			break
		}
		gr.Used[key]++
		if !g.valid() {
			// sound techniques never contradict themselves, so the puzzle
			// has no solution
			return [9][9]int{}, gr
		}
	}
	gr.Solved = g.complete() && g.valid()
	return g.board(), gr
}

// --- geometry ---------------------------------------------------------------

// Cells are numbered 0..80 row by row. Units 0..8 are rows, 9..17 columns,
// 18..26 boxes.
var (
	solverUnits     = buildSolverUnits()
	solverCellUnits = buildSolverCellUnits()
	solverPeers     = buildSolverPeers()
	solverSees      = buildSolverSees()
)

const allDigits uint16 = 1<<9 - 1

func buildSolverUnits() (u [27][9]int) {
	for k := 0; k < 9; k++ {
		for j := 0; j < 9; j++ {
			u[k][j] = k*9 + j
			u[9+k][j] = j*9 + k
			u[18+k][j] = (k/3*3+j/3)*9 + k%3*3 + j%3
		}
	}
	return u
}

// buildSolverCellUnits lists the row, column and box unit of every cell.
func buildSolverCellUnits() (cu [81][3]int) {
	for i := range cu {
		r, c := i/9, i%9
		cu[i] = [3]int{r, 9 + c, 18 + r/3*3 + c/3}
	}
	return cu
}

func buildSolverSees() (s [81][81]bool) {
	for a := 0; a < 81; a++ {
		for b := 0; b < 81; b++ {
			if a == b {
				continue
			}
			for j := 0; j < 3; j++ {
				if solverCellUnits[a][j] == solverCellUnits[b][j] {
					s[a][b] = true
				}
			}
		}
	}
	return s
}

func buildSolverPeers() (p [81][]int) {
	for a := 0; a < 81; a++ {
		for b := 0; b < 81; b++ {
			if a != b && solverSees[a][b] {
				p[a] = append(p[a], b)
			}
		}
	}
	return p
}

func digitBit(d int) uint16 { return 1 << (d - 1) }

// digitOf is the digit of a single-bit mask.
func digitOf(m uint16) int { return bits.TrailingZeros16(m) + 1 }

func popCount(m uint16) int { return bits.OnesCount16(m) }

// --- candidate grid ---------------------------------------------------------

// solverGrid is the pencil-mark state: cand holds a bitmask of possible digits
// per cell (bit d-1 = digit d). A placed cell keeps its digit as its only bit.
type solverGrid struct {
	cand   [81]uint16
	placed [81]bool
}

// newSolverGrid pencils every empty cell and places the givens. False for a
// digit outside 0..9 or a given that clashes with an earlier one.
func newSolverGrid(givens [9][9]int) (*solverGrid, bool) {
	g := &solverGrid{}
	for i := range g.cand {
		g.cand[i] = allDigits
	}
	for r := 0; r < 9; r++ {
		for c := 0; c < 9; c++ {
			v := givens[r][c]
			if v < 0 || v > 9 {
				return nil, false
			}
			if v == 0 {
				continue
			}
			i := r*9 + c
			if g.cand[i]&digitBit(v) == 0 {
				return nil, false // a peer already holds this digit
			}
			g.place(i, v)
		}
	}
	return g, true
}

func (g *solverGrid) place(i, d int) {
	b := digitBit(d)
	g.cand[i] = b
	g.placed[i] = true
	for _, p := range solverPeers[i] {
		if !g.placed[p] {
			g.cand[p] &^= b
		}
	}
}

// remove clears mask from a cell's candidates; true if something went away.
// Placed cells are never touched.
func (g *solverGrid) remove(i int, mask uint16) bool {
	if g.placed[i] || g.cand[i]&mask == 0 {
		return false
	}
	g.cand[i] &^= mask
	return true
}

func (g *solverGrid) complete() bool {
	for _, p := range g.placed {
		if !p {
			return false
		}
	}
	return true
}

// valid is false once the grid contradicts itself: an empty cell with no
// candidates, a digit placed twice in a unit, or a digit with nowhere left to go.
func (g *solverGrid) valid() bool {
	for i := range g.cand {
		if !g.placed[i] && g.cand[i] == 0 {
			return false
		}
	}
	for _, u := range solverUnits {
		var placed, open uint16
		for _, c := range u {
			if g.placed[c] {
				if placed&g.cand[c] != 0 {
					return false
				}
				placed |= g.cand[c]
			} else {
				open |= g.cand[c]
			}
		}
		if placed|open != allDigits {
			return false
		}
	}
	return true
}

// board is the placed digits, 0 for the rest.
func (g *solverGrid) board() (b [9][9]int) {
	for i := range g.cand {
		if g.placed[i] {
			b[i/9][i%9] = digitOf(g.cand[i])
		}
	}
	return b
}

// cellsWith lists the unplaced cells of a unit that still allow digit mask m.
func (g *solverGrid) cellsWith(unit int, m uint16) []int {
	var out []int
	for _, c := range solverUnits[unit] {
		if !g.placed[c] && g.cand[c]&m != 0 {
			out = append(out, c)
		}
	}
	return out
}

// --- technique registry -----------------------------------------------------

// solverImpl maps a learn.go key to its implementation. The order the solver
// tries them in comes from the techniques slice, not from here.
var solverImpl = map[string]func(*solverGrid) bool{
	"obvious-single":  (*solverGrid).obviousSingle,
	"hidden-single":   (*solverGrid).hiddenSingle,
	"obvious-pair":    func(g *solverGrid) bool { return g.nakedSubset(2) },
	"hidden-pair":     func(g *solverGrid) bool { return g.hiddenSubset(2) },
	"pointing-pair":   func(g *solverGrid) bool { return g.pointing(2) },
	"pointing-triple": func(g *solverGrid) bool { return g.pointing(3) },
	"obvious-triple":  func(g *solverGrid) bool { return g.nakedSubset(3) },
	"hidden-triple":   func(g *solverGrid) bool { return g.hiddenSubset(3) },
	"box-line":        (*solverGrid).boxLine,
	"x-wing":          func(g *solverGrid) bool { return g.fish(2) },
	"y-wing":          (*solverGrid).yWing,
	"swordfish":       func(g *solverGrid) bool { return g.fish(3) },
	"xyz-wing":        (*solverGrid).xyzWing,
	"jellyfish":       func(g *solverGrid) bool { return g.fish(4) },
	"simple-coloring": (*solverGrid).simpleColoring,
	"wxyz-wing":       (*solverGrid).wxyzWing,
	"xy-chain":        (*solverGrid).xyChain,
	"unique-rect":     (*solverGrid).uniqueRect,
}

type solverStep struct {
	key   string
	apply func(*solverGrid) bool
}

// solverSteps is solverImpl in learn.go order, cheapest first.
var solverSteps = func() []solverStep {
	var out []solverStep
	for _, t := range techniques {
		if fn, ok := solverImpl[t.Key]; ok {
			out = append(out, solverStep{t.Key, fn})
		}
	}
	return out
}()

// step applies the cheapest technique that changes anything and says which one.
// Each call is one application: a placement, or one pattern's eliminations.
func (g *solverGrid) step() (string, bool) {
	for _, s := range solverSteps {
		if s.apply(g) {
			return s.key, true
		}
	}
	return "", false
}

// --- singles ----------------------------------------------------------------

func (g *solverGrid) obviousSingle() bool {
	for i := range g.cand {
		if !g.placed[i] && popCount(g.cand[i]) == 1 {
			g.place(i, digitOf(g.cand[i]))
			return true
		}
	}
	return false
}

func (g *solverGrid) hiddenSingle() bool {
	for u := range solverUnits {
		for d := 1; d <= 9; d++ {
			if cells := g.cellsWith(u, digitBit(d)); len(cells) == 1 {
				g.place(cells[0], d)
				return true
			}
		}
	}
	return false
}

// --- subsets ----------------------------------------------------------------

// chooseCells walks the k-subsets of items in order. ok prunes a partial pick,
// found sees every full one; the walk stops at the first found that says true.
func chooseCells(items []int, k int, ok func(sel []int) bool, found func(sel []int) bool) bool {
	sel := make([]int, 0, k)
	var rec func(start int) bool
	rec = func(start int) bool {
		if len(sel) == k {
			return found(sel)
		}
		for j := start; j < len(items); j++ {
			sel = append(sel, items[j])
			if ok(sel) && rec(j+1) {
				return true
			}
			sel = sel[:len(sel)-1]
		}
		return false
	}
	return rec(0)
}

// nakedSubset: k cells of a unit whose candidates together are exactly k
// digits own those digits, so the rest of every unit the cells share loses
// them.
func (g *solverGrid) nakedSubset(k int) bool {
	for u := range solverUnits {
		var items []int
		for _, c := range solverUnits[u] {
			if n := popCount(g.cand[c]); !g.placed[c] && n >= 2 && n <= k {
				items = append(items, c)
			}
		}
		union := func(sel []int) (m uint16) {
			for _, c := range sel {
				m |= g.cand[c]
			}
			return m
		}
		if chooseCells(items, k,
			func(sel []int) bool { return popCount(union(sel)) <= k },
			func(sel []int) bool {
				m := union(sel)
				if popCount(m) != k {
					return false
				}
				return g.clearAroundSubset(sel, m)
			}) {
			return true
		}
	}
	return false
}

// clearAroundSubset removes m from every cell outside sel that shares a unit
// with all of sel (a pair in one row and one box cleans both).
func (g *solverGrid) clearAroundSubset(sel []int, m uint16) bool {
	changed := false
	for j := 0; j < 3; j++ {
		u := solverCellUnits[sel[0]][j]
		shared := true
		for _, c := range sel[1:] {
			if solverCellUnits[c][j] != u {
				shared = false
			}
		}
		if !shared {
			continue
		}
		for _, c := range solverUnits[u] {
			if !contains(sel, c) && g.remove(c, m) {
				changed = true
			}
		}
	}
	return changed
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// hiddenSubset: k digits of a unit that fit only into k cells fill those cells,
// so every other candidate in them goes. Digits with a single spot are hidden
// singles and never reach here.
func (g *solverGrid) hiddenSubset(k int) bool {
	for u := range solverUnits {
		// where[d] marks the unit positions (0..8) that still allow digit d+1
		var where [9]uint16
		var digits []int
		for d := 1; d <= 9; d++ {
			for pos, c := range solverUnits[u] {
				if !g.placed[c] && g.cand[c]&digitBit(d) != 0 {
					where[d-1] |= 1 << pos
				}
			}
			if n := popCount(where[d-1]); n >= 2 && n <= k {
				digits = append(digits, d-1)
			}
		}
		union := func(sel []int) (m uint16) {
			for _, d := range sel {
				m |= where[d]
			}
			return m
		}
		if chooseCells(digits, k,
			func(sel []int) bool { return popCount(union(sel)) <= k },
			func(sel []int) bool {
				spots := union(sel)
				if popCount(spots) != k {
					return false
				}
				var keep uint16
				for _, d := range sel {
					keep |= 1 << d
				}
				changed := false
				for pos, c := range solverUnits[u] {
					if spots&(1<<pos) != 0 && g.remove(c, allDigits&^keep) {
						changed = true
					}
				}
				return changed
			}) {
			return true
		}
	}
	return false
}

// --- intersections ----------------------------------------------------------

// pointing: a digit that fits only n cells of a box, all on one line, must sit
// on that line inside the box, so the rest of the line loses it. n=2 is the
// pointing pair, n=3 the pointing triple.
func (g *solverGrid) pointing(n int) bool {
	for box := 18; box < 27; box++ {
		for d := 1; d <= 9; d++ {
			cells := g.cellsWith(box, digitBit(d))
			if len(cells) != n {
				continue
			}
			for j := 0; j < 2; j++ { // 0 = row, 1 = column
				line := solverCellUnits[cells[0]][j]
				aligned := true
				for _, c := range cells[1:] {
					if solverCellUnits[c][j] != line {
						aligned = false
					}
				}
				if !aligned {
					continue
				}
				changed := false
				for _, c := range solverUnits[line] {
					if solverCellUnits[c][2] != box && g.remove(c, digitBit(d)) {
						changed = true
					}
				}
				if changed {
					return true
				}
			}
		}
	}
	return false
}

// boxLine: a digit that fits only inside one box along a row or column must sit
// there, so the rest of the box loses it.
func (g *solverGrid) boxLine() bool {
	for line := 0; line < 18; line++ {
		for d := 1; d <= 9; d++ {
			cells := g.cellsWith(line, digitBit(d))
			if len(cells) < 2 {
				continue // a lone spot is a hidden single
			}
			box := solverCellUnits[cells[0]][2]
			inOne := true
			for _, c := range cells[1:] {
				if solverCellUnits[c][2] != box {
					inOne = false
				}
			}
			if !inOne {
				continue
			}
			changed := false
			for _, c := range solverUnits[box] {
				if !contains(cells, c) && g.remove(c, digitBit(d)) {
					changed = true
				}
			}
			if changed {
				return true
			}
		}
	}
	return false
}

// --- fish -------------------------------------------------------------------

// fish: a digit confined to at most n columns in each of n rows (and spanning
// exactly n columns together) takes those columns in those rows, so the columns
// lose it elsewhere. Same with rows and columns swapped. n=2 X-Wing, 3
// Swordfish, 4 Jellyfish.
func (g *solverGrid) fish(n int) bool {
	for d := 1; d <= 9; d++ {
		for _, byRow := range [2]bool{true, false} {
			if g.fishFor(d, n, byRow) {
				return true
			}
		}
	}
	return false
}

func (g *solverGrid) fishFor(d, n int, byRow bool) bool {
	// at maps (base line, cover line) to a cell: base lines are rows when byRow
	at := func(base, cover int) int {
		if byRow {
			return base*9 + cover
		}
		return cover*9 + base
	}
	m := digitBit(d)
	var spots [9]uint16 // cover lines the digit still fits, per base line
	var bases []int
	for b := 0; b < 9; b++ {
		for cv := 0; cv < 9; cv++ {
			if c := at(b, cv); !g.placed[c] && g.cand[c]&m != 0 {
				spots[b] |= 1 << cv
			}
		}
		if k := popCount(spots[b]); k >= 2 && k <= n {
			bases = append(bases, b)
		}
	}
	union := func(sel []int) (u uint16) {
		for _, b := range sel {
			u |= spots[b]
		}
		return u
	}
	return chooseCells(bases, n,
		func(sel []int) bool { return popCount(union(sel)) <= n },
		func(sel []int) bool {
			u := union(sel)
			if popCount(u) != n {
				return false
			}
			changed := false
			for cv := 0; cv < 9; cv++ {
				if u&(1<<cv) == 0 {
					continue
				}
				for b := 0; b < 9; b++ {
					if !contains(sel, b) && g.remove(at(b, cv), m) {
						changed = true
					}
				}
			}
			return changed
		})
}

// --- wings ------------------------------------------------------------------

// clearSeenBy removes digit mask m from every cell that sees all of from
// (cells of from themselves excluded, a cell never sees itself).
func (g *solverGrid) clearSeenBy(from []int, m uint16) bool {
	changed := false
	for x := 0; x < 81; x++ {
		seen := true
		for _, f := range from {
			if !solverSees[f][x] {
				seen = false
				break
			}
		}
		if seen && g.remove(x, m) {
			changed = true
		}
	}
	return changed
}

// yWing: pivot AB sees pincers AC and BC. Whatever the pivot turns out to be,
// one pincer becomes C, so cells seeing both pincers lose C.
func (g *solverGrid) yWing() bool {
	for p := range g.cand {
		pc := g.cand[p]
		if g.placed[p] || popCount(pc) != 2 {
			continue
		}
		for _, q1 := range solverPeers[p] {
			shared := g.cand[q1] & pc
			if g.placed[q1] || popCount(g.cand[q1]) != 2 || popCount(shared) != 1 {
				continue
			}
			c := g.cand[q1] &^ shared
			want := pc&^shared | c // BC
			for _, q2 := range solverPeers[p] {
				if !g.placed[q2] && g.cand[q2] == want && g.clearSeenBy([]int{q1, q2}, c) {
					return true
				}
			}
		}
	}
	return false
}

// xyzWing: pivot XYZ sees pincers XZ and YZ. One of the three cells is Z in
// every case, so cells seeing all three lose Z.
func (g *solverGrid) xyzWing() bool {
	for p := range g.cand {
		pc := g.cand[p]
		if g.placed[p] || popCount(pc) != 3 {
			continue
		}
		for z := 1; z <= 9; z++ {
			zb := digitBit(z)
			if pc&zb == 0 {
				continue
			}
			rest := pc &^ zb
			xb := rest & -rest // lowest of the other two; the pair is unordered
			yb := rest &^ xb
			for _, q1 := range solverPeers[p] {
				if g.placed[q1] || g.cand[q1] != xb|zb {
					continue
				}
				for _, q2 := range solverPeers[p] {
					if !g.placed[q2] && g.cand[q2] == yb|zb && g.clearSeenBy([]int{p, q1, q2}, zb) {
						return true
					}
				}
			}
		}
	}
	return false
}

// wxyzWing: four cells holding four digits between them, where all digits but
// one (Z) are "restricted": every cell with that digit sees every other. Each
// restricted digit fits into the four cells at most once, so three of them
// cannot fill four cells and Z must be in one of its cells. Cells seeing every
// Z cell lose Z. Y-Wing and XYZ-Wing are the same idea on three cells.
func (g *solverGrid) wxyzWing() bool {
	var items []int
	for i := range g.cand {
		if n := popCount(g.cand[i]); !g.placed[i] && n >= 2 && n <= 4 {
			items = append(items, i)
		}
	}
	union := func(sel []int) (m uint16) {
		for _, c := range sel {
			m |= g.cand[c]
		}
		return m
	}
	return chooseCells(items, 4,
		func(sel []int) bool { return popCount(union(sel)) <= 4 },
		func(sel []int) bool {
			u := union(sel)
			if popCount(u) != 4 {
				return false
			}
			var zb uint16
			var zCells []int
			for d := 1; d <= 9; d++ {
				b := digitBit(d)
				if u&b == 0 {
					continue
				}
				var holders []int
				for _, c := range sel {
					if g.cand[c]&b != 0 {
						holders = append(holders, c)
					}
				}
				if allSee(holders) {
					continue
				}
				if zb != 0 {
					return false // two unrestricted digits: not a wing
				}
				zb, zCells = b, holders
			}
			if zb == 0 {
				return false
			}
			return g.clearSeenBy(zCells, zb)
		})
}

// allSee reports whether every pair of cells sees each other.
func allSee(cells []int) bool {
	for i, a := range cells {
		for _, b := range cells[i+1:] {
			if !solverSees[a][b] {
				return false
			}
		}
	}
	return true
}

// xyChain: bivalue cells linked by shared digits, start (Z,A) → (A,B) → ... →
// (Y,Z). If the start is not Z it is A, which forces the next cell to B and so
// on down to the end being Z. So one of the two ends is Z and cells seeing both
// lose it. A state is (cell, digit that cell holds if the start is not Z);
// walking states breadth first finds every chain without enumerating paths.
func (g *solverGrid) xyChain() bool {
	var bivalue []int
	for i := range g.cand {
		if !g.placed[i] && popCount(g.cand[i]) == 2 {
			bivalue = append(bivalue, i)
		}
	}
	type state struct {
		cell int
		hold uint16
	}
	for _, start := range bivalue {
		for rem := g.cand[start]; rem != 0; rem &= rem - 1 {
			zb := rem & -rem
			var visited [81]uint16
			queue := []state{{start, g.cand[start] &^ zb}}
			for len(queue) > 0 {
				cur := queue[0]
				queue = queue[1:]
				for _, n := range solverPeers[cur.cell] {
					if n == start || g.placed[n] || popCount(g.cand[n]) != 2 || g.cand[n]&cur.hold == 0 {
						continue
					}
					next := g.cand[n] &^ cur.hold // n cannot hold cur.hold, so it holds this
					if visited[n]&next != 0 {
						continue
					}
					visited[n] |= next
					if next == zb && g.clearSeenBy([]int{start, n}, zb) {
						return true
					}
					queue = append(queue, state{n, next})
				}
			}
		}
	}
	return false
}

// --- coloring ---------------------------------------------------------------

// simpleColoring: for one digit, units with exactly two spots link their cells
// into chains; coloring alternately splits each chain in two sets of which
// exactly one holds the digit. A color that has two cells in one unit is false
// (the other color is true, the digit goes away from the false cells). A cell
// outside the chain that sees both colors loses the digit.
func (g *solverGrid) simpleColoring() bool {
	for d := 1; d <= 9; d++ {
		m := digitBit(d)
		links := map[int][]int{}
		for u := range solverUnits {
			if cells := g.cellsWith(u, m); len(cells) == 2 {
				links[cells[0]] = append(links[cells[0]], cells[1])
				links[cells[1]] = append(links[cells[1]], cells[0])
			}
		}
		colour := map[int]int{}
		for start := 0; start < 81; start++ {
			if _, linked := links[start]; !linked {
				continue
			}
			if _, done := colour[start]; done {
				continue
			}
			var chain [2][]int
			colour[start] = 0
			queue := []int{start}
			broken := false
			for len(queue) > 0 {
				c := queue[0]
				queue = queue[1:]
				chain[colour[c]] = append(chain[colour[c]], c)
				for _, n := range links[c] {
					if cn, seen := colour[n]; !seen {
						colour[n] = 1 - colour[c]
						queue = append(queue, n)
					} else if cn == colour[c] {
						broken = true // odd loop: not a consistent chain
					}
				}
			}
			if broken {
				continue
			}
			for k := 0; k < 2; k++ {
				if !anySee(chain[k]) {
					continue
				}
				changed := false
				for _, c := range chain[k] {
					if g.remove(c, m) {
						changed = true
					}
				}
				if changed {
					return true
				}
			}
			changed := false
			for x := 0; x < 81; x++ {
				if g.placed[x] || g.cand[x]&m == 0 || contains(chain[0], x) || contains(chain[1], x) {
					continue
				}
				if seesAny(x, chain[0]) && seesAny(x, chain[1]) && g.remove(x, m) {
					changed = true
				}
			}
			if changed {
				return true
			}
		}
	}
	return false
}

// anySee reports whether some pair of cells sees each other.
func anySee(cells []int) bool {
	for i, a := range cells {
		for _, b := range cells[i+1:] {
			if solverSees[a][b] {
				return true
			}
		}
	}
	return false
}

func seesAny(x int, cells []int) bool {
	for _, c := range cells {
		if solverSees[x][c] {
			return true
		}
	}
	return false
}

// --- unique rectangle -------------------------------------------------------

// uniqueRect: a valid puzzle has one solution, so four cells in two rows, two
// columns and two boxes must never all end up as the same pair AB (swapping
// the pair would give a second solution). When three or four of the cells
// are about to, the extra candidates in the rest must hold. Only the types
// that follow directly from that are implemented; none of them is safe on a
// puzzle with several solutions.
//
// Type 1: three cells are exactly AB, the fourth drops A and B.
// Type 2: two cells are exactly AB, the other two are ABX: one of them is X,
// so cells seeing both lose X.
// Type 4: two cells are exactly AB, the other two share a unit where A fits
// only into those two: one of them is A, so both lose B (and likewise B/A).
func (g *solverGrid) uniqueRect() bool {
	for r1 := 0; r1 < 9; r1++ {
		for r2 := r1 + 1; r2 < 9; r2++ {
			for c1 := 0; c1 < 9; c1++ {
				for c2 := c1 + 1; c2 < 9; c2++ {
					// exactly two boxes: same band xor same stack
					if (r1/3 == r2/3) == (c1/3 == c2/3) {
						continue
					}
					rect := [4]int{r1*9 + c1, r1*9 + c2, r2*9 + c1, r2*9 + c2}
					if g.rectangleHit(rect) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (g *solverGrid) rectangleHit(rect [4]int) bool {
	for _, c := range rect {
		if g.placed[c] {
			return false
		}
	}
	for _, seed := range rect {
		pair := g.cand[seed]
		if popCount(pair) != 2 {
			continue
		}
		exact, roof, ok := g.splitRect(rect, pair)
		if !ok {
			continue
		}
		switch len(exact) {
		case 3: // type 1
			if g.remove(roof[0], pair) {
				return true
			}
		case 2:
			extra0, extra1 := g.cand[roof[0]]&^pair, g.cand[roof[1]]&^pair
			if extra0 == extra1 && popCount(extra0) == 1 && g.clearSeenBy(roof, extra0) { // type 2
				return true
			}
			if g.rectType4(roof, pair) {
				return true
			}
		}
	}
	return false
}

// splitRect sorts the corners into those that are exactly pair and those that
// hold pair plus more; false if any corner lacks part of pair.
func (g *solverGrid) splitRect(rect [4]int, pair uint16) (exact, roof []int, ok bool) {
	for _, c := range rect {
		switch {
		case g.cand[c] == pair:
			exact = append(exact, c)
		case g.cand[c]&pair == pair:
			roof = append(roof, c)
		default:
			return nil, nil, false
		}
	}
	return exact, roof, true
}

// rectType4: the roof cells share a unit in which one pair digit fits only
// them, so the other pair digit is out of both.
func (g *solverGrid) rectType4(roof []int, pair uint16) bool {
	for j := 0; j < 3; j++ {
		u := solverCellUnits[roof[0]][j]
		if solverCellUnits[roof[1]][j] != u {
			continue
		}
		for rem := pair; rem != 0; rem &= rem - 1 {
			a := rem & -rem
			cells := g.cellsWith(u, a)
			if len(cells) != 2 || !contains(cells, roof[0]) || !contains(cells, roof[1]) {
				continue
			}
			changed := false
			for _, c := range roof {
				if g.remove(c, pair&^a) {
					changed = true
				}
			}
			if changed {
				return true
			}
		}
	}
	return false
}
