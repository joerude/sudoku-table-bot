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
