package usdoku

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Grid is a puzzle's starting position: Grid[r][c] is the given digit of row r,
// column c (0-based), or 0 for a cell the players have to solve.
type Grid [9][9]int

// Givens counts the digits the puzzle starts with.
func (g Grid) Givens() int {
	n := 0
	for _, row := range g {
		for _, v := range row {
			if v != 0 {
				n++
			}
		}
	}
	return n
}

// cell is one board entry as usdoku sends it. "closed" cells are the givens;
// "open" cells carry the SOLUTION in value, so they must never leave this file.
// Who solved an open cell depends on the mode: original lists the players who
// got it right in scorers (first to last); hardcore has no scorers and keeps
// each player's current entry in guesses instead (no order, no history).
type cell struct {
	Row     int      `json:"row"`
	Column  int      `json:"column"`
	Type    string   `json:"type"`
	Value   int      `json:"value"`
	Scorers []string `json:"scorers"`
	Guesses []struct {
		Name  string `json:"name"`
		Value *int   `json:"value"` // null once a player clears the cell
	} `json:"guesses"`
}

// Progress is how far each player has got on the board.
type Progress struct {
	Open   int            // cells to solve
	Solved map[string]int // usdoku nick → cells filled in correctly
}

// progressFrom counts each player's correct open cells. Only counts leave
// here: the solution itself never does.
func progressFrom(cells []cell) Progress {
	p := Progress{Solved: map[string]int{}}
	for _, c := range cells {
		if c.Type != "open" {
			continue
		}
		p.Open++
		seen := map[string]bool{}
		for _, n := range c.Scorers {
			seen[n] = true
		}
		for _, g := range c.Guesses {
			if g.Value != nil && *g.Value == c.Value {
				seen[g.Name] = true
			}
		}
		for n := range seen {
			p.Solved[n]++
		}
	}
	return p
}

// gridFrom keeps the givens of a board and drops everything else.
func gridFrom(cells []cell) (Grid, error) {
	var g Grid
	for _, c := range cells {
		if c.Type != "closed" {
			continue
		}
		if c.Row < 1 || c.Row > 9 || c.Column < 1 || c.Column > 9 || c.Value < 1 || c.Value > 9 {
			return Grid{}, fmt.Errorf("usdoku board: bad cell r%d c%d = %d", c.Row, c.Column, c.Value)
		}
		g[c.Row-1][c.Column-1] = c.Value
	}
	// A sudoku with a unique solution has at least 17 givens.
	if n := g.Givens(); n < 17 {
		return Grid{}, fmt.Errorf("usdoku board: only %d givens", n)
	}
	return g, nil
}

// boardPolls bounds the long-polls one Board call makes while waiting for the
// board. The server answers a register right away, so this is only a backstop.
const boardPolls = 5

// Board fetches a room's starting position.
func (c *Client) Board(ctx context.Context, gameCode string) (Grid, error) {
	cells, err := c.cells(ctx, gameCode)
	if err != nil {
		return Grid{}, err
	}
	return gridFrom(cells)
}

// Progress fetches how many cells each player has solved so far.
func (c *Client) Progress(ctx context.Context, gameCode string) (Progress, error) {
	cells, err := c.cells(ctx, gameCode)
	if err != nil {
		return Progress{}, err
	}
	return progressFrom(cells), nil
}

// cells fetches a room's board. The REST API doesn't expose it; the web app
// gets it over socket.io (event "data" after a "register"), so this speaks just
// enough Engine.IO v4 long-polling to do the same. A register does NOT join the
// game: the room's player list stays as it was, and it works for pending and
// running rooms alike.
func (c *Client) cells(ctx context.Context, gameCode string) ([]cell, error) {
	base := c.socketURL + "/socket.io/?EIO=4&transport=polling"

	// Open: "0{"sid":...}".
	open, err := c.sio(ctx, http.MethodGet, base, "")
	if err != nil {
		return nil, err
	}
	var hs struct {
		SID string `json:"sid"`
	}
	if !strings.HasPrefix(open, "0") || json.Unmarshal([]byte(open[1:]), &hs) != nil || hs.SID == "" {
		return nil, fmt.Errorf("usdoku socket: bad handshake: %s", snippet([]byte(open)))
	}
	url := base + "&sid=" + hs.SID
	defer func() { _, _ = c.sio(context.Background(), http.MethodPost, url, "1") }() // close

	reg, _ := json.Marshal([]any{"register", map[string]string{
		"name": "observer", "gameCode": gameCode, "type": "register",
	}})
	// Connect to the default namespace, then register for the room. Two posts:
	// the server reads one packet per request reliably.
	for _, pkt := range []string{"40", "42" + string(reg)} {
		if _, err := c.sio(ctx, http.MethodPost, url, pkt); err != nil {
			return nil, err
		}
	}

	for i := 0; i < boardPolls; i++ {
		payload, err := c.sio(ctx, http.MethodGet, url, "")
		if err != nil {
			return nil, err
		}
		for _, pkt := range strings.Split(payload, "\x1e") {
			switch {
			case pkt == "2": // ping
				if _, err := c.sio(ctx, http.MethodPost, url, "3"); err != nil {
					return nil, err
				}
			case strings.HasPrefix(pkt, "42"):
				if cells, ok := boardEvent(pkt[2:]); ok {
					return cells, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("usdoku socket: no board for %s", gameCode)
}

// boardEvent reads a socket.io event array and returns the board of a "data"
// event that carries one.
func boardEvent(raw string) ([]cell, bool) {
	var ev []json.RawMessage
	if json.Unmarshal([]byte(raw), &ev) != nil || len(ev) < 2 {
		return nil, false
	}
	var name string
	if json.Unmarshal(ev[0], &name) != nil || name != "data" {
		return nil, false
	}
	var data struct {
		Board []cell `json:"board"`
	}
	if json.Unmarshal(ev[1], &data) != nil || len(data.Board) == 0 {
		return nil, false
	}
	return data.Board, true
}

// sio makes one Engine.IO polling request and returns the response body.
func (c *Client) sio(ctx context.Context, method, url, body string) (string, error) {
	var rd io.Reader
	if body != "" {
		rd = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return "", err
	}
	if body != "" {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("usdoku socket: http %d: %s", resp.StatusCode, snippet(data))
	}
	return string(data), nil
}
