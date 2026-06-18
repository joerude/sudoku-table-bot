// Package usdoku is a thin client for usdoku.com's (undocumented) HTTP API.
//
// Game creation and state are plain POST endpoints under https://api.usdoku.com/v2,
// authenticated only by a random per-device id header.
// Treat this as best-effort: the API can change, so callers should keep a manual
// fallback and poll politely.
package usdoku

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	// DefaultBaseURL is the usdoku v2 API root.
	DefaultBaseURL = "https://api.usdoku.com/v2"
	defaultVersion = "2"
)

// Client talks to the usdoku HTTP API.
type Client struct {
	httpc    *http.Client
	baseURL  string
	deviceID string
	version  string
}

// New returns a client with a freshly generated device id.
func New() *Client {
	return &Client{
		httpc:    &http.Client{Timeout: 15 * time.Second},
		baseURL:  DefaultBaseURL,
		deviceID: newDeviceID(),
		version:  defaultVersion,
	}
}

// WithBaseURL overrides the API root (used in tests).
func (c *Client) WithBaseURL(u string) *Client {
	c.baseURL = u
	return c
}

// Create opens a new game and returns its short code.
// difficulty: easy|medium|hard|extreme, mode: hardcore|original, visibility: public|private.
func (c *Client) Create(ctx context.Context, difficulty, mode, visibility string) (string, error) {
	body := map[string]string{"difficulty": difficulty, "mode": mode, "visibility": visibility}
	// The API has been observed to return the code under "code"; accept "gameCode" too.
	var r struct {
		Code     string `json:"code"`
		GameCode string `json:"gameCode"`
	}
	if err := c.post(ctx, "/create", body, &r); err != nil {
		return "", err
	}
	code := r.Code
	if code == "" {
		code = r.GameCode
	}
	if code == "" {
		return "", fmt.Errorf("usdoku create: empty game code in response")
	}
	return code, nil
}

// Player is one participant in a game.
type Player struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	JoinedAt    int64  `json:"joinedAt"`
	CompletedAt *int64 `json:"completedAt"` // nil => did not finish
}

// Info is the game's metadata.
type Info struct {
	Mode         string `json:"mode"`
	Difficulty   string `json:"difficulty"`
	Visibility   string `json:"visibility"`
	Status       string `json:"status"` // "running", or a terminal state
	StartedAt    int64  `json:"startedAt"`
	SupersededBy string `json:"supersededBy"`
}

// GameInfo is the /info response.
type GameInfo struct {
	Players []Player `json:"players"`
	Info    Info     `json:"info"`
}

// Info fetches the current state of a game by code.
func (c *Client) Info(ctx context.Context, gameCode string) (*GameInfo, error) {
	var g GameInfo
	if err := c.post(ctx, "/info", map[string]string{"gameCode": gameCode}, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// FinishOrder returns the players who finished, ordered first-to-last by
// completion time. Non-finishers (no completedAt) are excluded — they score 0.
func (g *GameInfo) FinishOrder() []Player {
	done := make([]Player, 0, len(g.Players))
	for _, p := range g.Players {
		if p.CompletedAt != nil {
			done = append(done, p)
		}
	}
	sort.SliceStable(done, func(i, j int) bool {
		return *done[i].CompletedAt < *done[j].CompletedAt
	})
	return done
}

// terminalStatuses are usdoku statuses that mean a game is over. Observed live
// statuses include "pending" (created, not started) and "running"; the exact
// terminal string is not documented, so we match a small known set and also
// fall back to heuristics below.
var terminalStatuses = map[string]bool{
	"finished": true, "completed": true, "ended": true, "done": true, "over": true,
}

// Finished reports whether the game looks over. A "pending" or "running" game is
// NOT finished unless a successor exists or every player has completed.
func (g *GameInfo) Finished() bool {
	if terminalStatuses[g.Info.Status] {
		return true
	}
	if g.Info.SupersededBy != "" {
		return true
	}
	// A running game where every (joined) player has finished.
	if g.Info.Status == "running" && len(g.Players) > 0 {
		for _, p := range g.Players {
			if p.CompletedAt == nil {
				return false
			}
		}
		return true
	}
	return false
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Usdoku-Pen", c.deviceID)
	req.Header.Set("X-Usdoku-Version", c.version)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("usdoku %s: http %d: %s", path, resp.StatusCode, snippet(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func snippet(b []byte) string {
	if len(b) > 200 {
		return string(b[:200])
	}
	return string(b)
}

// newDeviceID returns a random UUID-v4 string for the X-Usdoku-Pen header.
func newDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
