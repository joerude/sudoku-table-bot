package usdoku

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateParsesCode(t *testing.T) {
	for _, field := range []string{"code", "gameCode"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Usdoku-Pen") == "" {
				t.Errorf("missing device-id header")
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["difficulty"] != "medium" || body["mode"] != "hardcore" {
				t.Errorf("unexpected body: %v", body)
			}
			io.WriteString(w, `{"`+field+`":"ABCD"}`)
		}))
		c := New().WithBaseURL(srv.URL)
		code, err := c.Create(context.Background(), "medium", "hardcore", "private")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if code != "ABCD" {
			t.Errorf("field %q: got code %q, want ABCD", field, code)
		}
		srv.Close()
	}
}

func TestInfoFinishOrderAndFinished(t *testing.T) {
	// Carol finished first (lower completedAt), then Alice; Bob did not finish.
	payload := `{
	  "players": [
	    {"name":"Alice","completedAt":200,"joinedAt":1},
	    {"name":"Bob","joinedAt":2},
	    {"name":"Carol","completedAt":100,"joinedAt":3}
	  ],
	  "info": {"mode":"hardcore","difficulty":"medium","status":"running"}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, payload)
	}))
	defer srv.Close()

	g, err := New().WithBaseURL(srv.URL).Info(context.Background(), "ABCD")
	if err != nil {
		t.Fatalf("info: %v", err)
	}

	order := g.FinishOrder()
	if len(order) != 2 {
		t.Fatalf("want 2 finishers, got %d", len(order))
	}
	if order[0].Name != "Carol" || order[1].Name != "Alice" {
		t.Errorf("finish order wrong: %s, %s", order[0].Name, order[1].Name)
	}

	// Bob has no completedAt -> not finished yet.
	if g.Finished() {
		t.Errorf("game should not be finished while Bob is unfinished")
	}
}

func TestFinishedByStatusAndSuperseded(t *testing.T) {
	cases := []struct {
		name string
		info Info
		want bool
	}{
		{"pending", Info{Status: "pending"}, false}, // freshly created, not over
		{"running", Info{Status: "running"}, false},
		{"finished status", Info{Status: "finished"}, true},
		{"superseded", Info{Status: "running", SupersededBy: "RAIJ"}, true},
	}
	for _, tc := range cases {
		g := &GameInfo{Info: tc.info, Players: []Player{{Name: "x"}}}
		if got := g.Finished(); got != tc.want {
			t.Errorf("%s: Finished()=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestSolveSeconds(t *testing.T) {
	ms := func(v int64) *int64 { return &v }
	cases := []struct {
		name      string
		p         Player
		startedAt int64
		want      int64
	}{
		{"dnf (no completedAt)", Player{JoinedAt: 1000}, 100, 0},
		{"no base at all → ignored", Player{JoinedAt: 0, CompletedAt: ms(1782286619)}, 0, 0},
		{"startedAt base when joinedAt missing", Player{JoinedAt: 0, CompletedAt: ms(352)}, 100, 252},
		{"startedAt preferred over joinedAt", Player{JoinedAt: 50, CompletedAt: ms(352)}, 100, 252},
		{"joinedAt fallback when no startedAt", Player{JoinedAt: 100, CompletedAt: ms(352)}, 0, 252},
		{"milliseconds scaled down", Player{CompletedAt: ms(1_252_000)}, 1_000_000, 252},
		{"implausible → 0", Player{CompletedAt: ms(99_999_999_999)}, 1, 0},
		{"negative delta", Player{CompletedAt: ms(100)}, 500, 0},
	}
	for _, c := range cases {
		if got := c.p.SolveSeconds(c.startedAt); got != c.want {
			t.Errorf("%s: SolveSeconds()=%d want %d", c.name, got, c.want)
		}
	}
}
