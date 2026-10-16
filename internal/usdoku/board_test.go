package usdoku

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// testBoard is a full usdoku board: the first 20 cells (row-major) are givens
// with digit (i%9)+1, the rest are open cells whose value is the solution.
func testBoard() []map[string]any {
	var b []map[string]any
	for i := 0; i < 81; i++ {
		typ, v := "open", 7
		if i < 20 {
			typ, v = "closed", i%9+1
		}
		b = append(b, map[string]any{"row": i/9 + 1, "column": i%9 + 1, "type": typ, "value": v,
			"scorers": []string{}, "guesses": []any{}})
	}
	return b
}

// fakeSocket mimics usdoku's Engine.IO v4 polling endpoint: it answers a
// register with a ping and then the "data" event.
func fakeSocket(t *testing.T, board []map[string]any) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var posts []string
	queue := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/socket.io/" || r.URL.Query().Get("EIO") != "4" {
			t.Errorf("unexpected request %s", r.URL)
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("sid") == "" {
			io.WriteString(w, `0{"sid":"S1","pingInterval":25000}`)
			return
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			posts = append(posts, string(body))
			switch {
			case string(body) == "40":
				queue = append(queue, `40{"sid":"X"}`)
			case strings.HasPrefix(string(body), `42["register"`):
				data, _ := json.Marshal([]any{"data", map[string]any{"board": board, "players": []any{}}})
				queue = append(queue, "2", "42"+string(data))
			}
			io.WriteString(w, "ok")
			return
		}
		io.WriteString(w, strings.Join(queue, "\x1e"))
		queue = nil
	}))
	return srv, &posts
}

func TestBoardKeepsOnlyGivens(t *testing.T) {
	srv, posts := fakeSocket(t, testBoard())
	defer srv.Close()

	g, err := New().WithBaseURL(srv.URL+"/v2").Board(context.Background(), "QWER")
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if g.Givens() != 20 {
		t.Errorf("givens = %d, want 20", g.Givens())
	}
	if g[0][0] != 1 || g[0][8] != 9 || g[2][1] != 2 {
		t.Errorf("givens misplaced: %v", g[:3])
	}
	// Open cells carry the solution (7 here): it must not reach the grid.
	if g[2][2] != 0 || g[8][8] != 0 {
		t.Errorf("solution leaked into the grid: %v", g)
	}

	var sawReg, sawPong bool
	for _, p := range *posts {
		if strings.HasPrefix(p, "42") && strings.Contains(p, `"gameCode":"QWER"`) {
			sawReg = true
		}
		if p == "3" {
			sawPong = true
		}
	}
	if !sawReg || !sawPong {
		t.Errorf("posts = %q: want a register for QWER and a pong", *posts)
	}
}

func TestBoardRejectsTooFewGivens(t *testing.T) {
	b := testBoard()
	for i := 10; i < 20; i++ {
		b[i]["type"] = "open"
	}
	srv, _ := fakeSocket(t, b)
	defer srv.Close()
	if _, err := New().WithBaseURL(srv.URL).Board(context.Background(), "QWER"); err == nil {
		t.Fatal("want an error for a board with 10 givens")
	}
}

func TestGridFromRejectsBadCell(t *testing.T) {
	if _, err := gridFrom([]cell{{Row: 10, Column: 1, Type: "closed", Value: 3}}); err == nil {
		t.Fatal("want an error for row 10")
	}
}
