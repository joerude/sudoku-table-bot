# Rating Ladder (ELO) + Crown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an eternal ELO rating ladder with a stealable #1 crown that runs parallel to the season, surfacing a rating delta after every game.

**Architecture:** Rating is a **pure function of game history** — one `domain.ComputeRatings(games)` replays all completed games (season + duels) and derives current ratings, peaks, the crown, and per-game deltas. No rating table, no incremental state: every read replays from scratch (data is tiny). The bot appends a rating footer to the existing result post and adds a `/rating` ladder command.

**Tech Stack:** Go 1.25, `gopkg.in/telebot.v3`, `modernc.org/sqlite` (no CGO). Module path `github.com/joerude/sudoku-bot-telegram`.

## Global Constraints

- **No `go` on host.** Run ALL go tooling in docker:
  `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c '<cmd>'`
- **Conventional Commits, NO `Co-Authored-By` trailer** (repo style).
- All user-facing text through `esc()`; HTML parse mode is the bot default.
- Pure logic lives in `internal/domain` (like `scoring.go`, `streaks.go`); storage is raw `database/sql`; bot text builders live in `messages.go`.
- Rating ignores the season/duel split: it feeds on **all** completed, non-deleted games (`sqlCompletedGames`).
- Tuning (locked): start 1000; provisional `< 10` games → K=64, else K=32; pairwise with per-player K split by opponent count; DNF = loss to all finishers, DNF-vs-DNF ignored; crown ties keep the incumbent.

---

### Task 1: Pure rating engine (`domain/rating.go`)

The whole ELO computation as one pure, I/O-free unit. This is the heart; everything else reads from it.

**Files:**
- Create: `internal/domain/rating.go`
- Test: `internal/domain/rating_test.go`

**Interfaces:**
- Consumes: nothing (pure).
- Produces:
  - `domain.RatingResult{ PlayerID int64; Rank int }` (rank 0 = DNF, ≥1 = place)
  - `domain.RatingGame{ ID int64; Participants []RatingResult }`
  - `domain.PlayerRating{ PlayerID int64; Rating, Peak, Games int; Provisional bool }`
  - `domain.GameRating{ GameID int64; Delta, NewRating map[int64]int; CrownBefore, CrownAfter int64 }`
  - `domain.Ratings{ Players map[int64]PlayerRating; Ladder []PlayerRating; Crown int64; PerGame []GameRating }`
  - `func ComputeRatings(games []RatingGame) Ratings`

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/rating_test.go`:

```go
package domain

import "testing"

// helper: finishers in the given order (rank 1..n), then DNFs (rank 0).
func game(id int64, finishers []int64, dnf ...int64) RatingGame {
	g := RatingGame{ID: id}
	for i, pid := range finishers {
		g.Participants = append(g.Participants, RatingResult{PlayerID: pid, Rank: i + 1})
	}
	for _, pid := range dnf {
		g.Participants = append(g.Participants, RatingResult{PlayerID: pid, Rank: 0})
	}
	return g
}

func TestKFactor(t *testing.T) {
	for _, tc := range []struct {
		played int
		want   float64
	}{{0, 64}, {9, 64}, {10, 32}, {50, 32}} {
		if got := kFactor(tc.played); got != tc.want {
			t.Errorf("kFactor(%d)=%v want %v", tc.played, got, tc.want)
		}
	}
}

func TestTwoPlayerFirstGame(t *testing.T) {
	// Both start 1000, provisional K=64, E=0.5 -> winner +32, loser -32.
	r := ComputeRatings([]RatingGame{game(1, []int64{10, 20})})
	if r.Players[10].Rating != 1032 || r.Players[20].Rating != 968 {
		t.Fatalf("ratings: 10=%d 20=%d want 1032/968", r.Players[10].Rating, r.Players[20].Rating)
	}
	if r.Crown != 10 {
		t.Errorf("crown=%d want 10", r.Crown)
	}
	if len(r.PerGame) != 1 || r.PerGame[0].Delta[10] != 32 || r.PerGame[0].Delta[20] != -32 {
		t.Errorf("per-game delta wrong: %+v", r.PerGame)
	}
	if r.PerGame[0].CrownBefore != 0 || r.PerGame[0].CrownAfter != 10 {
		t.Errorf("crown change: before=%d after=%d want 0/10", r.PerGame[0].CrownBefore, r.PerGame[0].CrownAfter)
	}
}

func TestThreePlayerKSplit(t *testing.T) {
	// ranks 10>20>30, all start 1000, K=64 split by 2 opponents -> per-pair 32.
	// 10:+16+16=+32, 20:-16+16=0, 30:-16-16=-32. |delta| <= 64.
	r := ComputeRatings([]RatingGame{game(1, []int64{10, 20, 30})})
	if r.Players[10].Rating != 1032 || r.Players[20].Rating != 1000 || r.Players[30].Rating != 968 {
		t.Fatalf("ratings: 10=%d 20=%d 30=%d want 1032/1000/968",
			r.Players[10].Rating, r.Players[20].Rating, r.Players[30].Rating)
	}
}

func TestDNFIsLoss(t *testing.T) {
	// 10 finishes, 20 DNF -> 20 loses to 10 like a normal loss.
	r := ComputeRatings([]RatingGame{game(1, []int64{10}, 20)})
	if r.Players[10].Rating != 1032 || r.Players[20].Rating != 968 {
		t.Fatalf("ratings: 10=%d 20=%d want 1032/968", r.Players[10].Rating, r.Players[20].Rating)
	}
}

func TestDNFvsDNFIgnored(t *testing.T) {
	// 10 finishes; 20 and 30 both DNF. 20 and 30 don't play each other, so they
	// only lose to 10 and end up equal.
	r := ComputeRatings([]RatingGame{game(1, []int64{10}, 20, 30)})
	if r.Players[20].Rating != r.Players[30].Rating {
		t.Errorf("DNF-vs-DNF should not move them: 20=%d 30=%d", r.Players[20].Rating, r.Players[30].Rating)
	}
	if r.Players[10].Rating <= 1000 {
		t.Errorf("finisher should rise: 10=%d", r.Players[10].Rating)
	}
}

func TestPeakTracksHighWaterMark(t *testing.T) {
	// 10 wins (1032), then loses to 20 (who is now higher) — peak stays 1032.
	r := ComputeRatings([]RatingGame{
		game(1, []int64{10, 20}),
		game(2, []int64{20, 10}),
	})
	if r.Players[10].Peak != 1032 {
		t.Errorf("peak=%d want 1032", r.Players[10].Peak)
	}
	if r.Players[10].Rating >= 1032 {
		t.Errorf("current should have dropped below peak: %d", r.Players[10].Rating)
	}
}

func TestProvisionalFlag(t *testing.T) {
	var games []RatingGame
	for i := int64(1); i <= 10; i++ {
		games = append(games, game(i, []int64{10, 20}))
	}
	r := ComputeRatings(games)
	if r.Players[10].Games != 10 || r.Players[10].Provisional {
		t.Errorf("after 10 games: games=%d provisional=%v want 10/false",
			r.Players[10].Games, r.Players[10].Provisional)
	}
}

func TestTopPlayerIncumbentKeepsOnTie(t *testing.T) {
	// Exact tie: incumbent 20 keeps the crown; without an incumbent it breaks to
	// the lowest id (10).
	tie := map[int64]int{10: 1000, 20: 1000}
	if got := topPlayer(tie, 20); got != 20 {
		t.Errorf("incumbent should keep crown on tie: got %d want 20", got)
	}
	if got := topPlayer(tie, 0); got != 10 {
		t.Errorf("no incumbent -> lowest id wins tie: got %d want 10", got)
	}
	// A strictly higher rating always takes the crown regardless of incumbency.
	if got := topPlayer(map[int64]int{10: 1100, 20: 1000}, 20); got != 10 {
		t.Errorf("higher rating must take crown: got %d want 10", got)
	}
}

func TestDeterministic(t *testing.T) {
	games := []RatingGame{game(1, []int64{10, 20, 30}), game(2, []int64{30, 10}, 20)}
	a := ComputeRatings(games)
	b := ComputeRatings(games)
	for id := range a.Players {
		if a.Players[id] != b.Players[id] {
			t.Errorf("non-deterministic for %d: %+v vs %+v", id, a.Players[id], b.Players[id])
		}
	}
}

func TestSoloGameNoImpact(t *testing.T) {
	r := ComputeRatings([]RatingGame{game(1, []int64{10})}) // single finisher, no opponent
	if len(r.PerGame) != 0 {
		t.Errorf("solo game must produce no rating impact, got %+v", r.PerGame)
	}
	if len(r.Players) != 0 {
		t.Errorf("solo game must not register players, got %+v", r.Players)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/domain/ -run Rating -v'`
Expected: FAIL — `undefined: ComputeRatings`, `undefined: kFactor`, etc.

- [ ] **Step 3: Write the implementation**

Create `internal/domain/rating.go`:

```go
package domain

import (
	"math"
	"sort"
)

// Rating tuning. The eternal ELO ladder is a pure function of game history.
const (
	StartRating      = 1000
	ProvisionalGames = 10
	KProvisional     = 64
	KEstablished     = 32
)

// RatingResult is one participant's finish: Rank 0 = DNF, >=1 = finishing place.
type RatingResult struct {
	PlayerID int64
	Rank     int
}

// RatingGame is one completed game feeding the rating, in chronological order.
type RatingGame struct {
	ID           int64
	Participants []RatingResult
}

// PlayerRating is a player's standing on the eternal ladder.
type PlayerRating struct {
	PlayerID    int64
	Rating      int
	Peak        int
	Games       int
	Provisional bool
}

// GameRating is the rating impact of a single game (drives the result-post footer).
type GameRating struct {
	GameID      int64
	Delta       map[int64]int // playerID -> rating change this game
	NewRating   map[int64]int // playerID -> rating after this game
	CrownBefore int64         // top playerID before this game (0 = none)
	CrownAfter  int64         // top playerID after this game (0 = none)
}

// Ratings is the full result of replaying a chat's game history.
type Ratings struct {
	Players map[int64]PlayerRating
	Ladder  []PlayerRating // rating desc, peak desc, id asc
	Crown   int64          // top playerID (0 = none)
	PerGame []GameRating   // one entry per game with rating impact, chronological
}

func kFactor(gamesPlayed int) float64 {
	if gamesPlayed < ProvisionalGames {
		return KProvisional
	}
	return KEstablished
}

func expectedScore(rA, rB int) float64 {
	return 1 / (1 + math.Pow(10, float64(rB-rA)/400))
}

// outcome returns {winnerID, loserID} and ok. No pair when both DNF (ignored).
func outcome(a, b RatingResult) (win, lose int64, ok bool) {
	aF, bF := a.Rank >= 1, b.Rank >= 1
	switch {
	case aF && bF:
		if a.Rank == b.Rank {
			return 0, 0, false // ranks are unique in practice
		}
		if a.Rank < b.Rank {
			return a.PlayerID, b.PlayerID, true
		}
		return b.PlayerID, a.PlayerID, true
	case aF && !bF:
		return a.PlayerID, b.PlayerID, true // finisher beats DNF
	case !aF && bF:
		return b.PlayerID, a.PlayerID, true
	default:
		return 0, 0, false // both DNF
	}
}

// topPlayer returns the highest-rated player. On an exact tie the incumbent keeps
// the crown; otherwise ties break by lowest playerID for determinism.
func topPlayer(rating map[int64]int, incumbent int64) int64 {
	if len(rating) == 0 {
		return 0
	}
	bestR := math.MinInt
	for _, r := range rating {
		if r > bestR {
			bestR = r
		}
	}
	if incumbent != 0 && rating[incumbent] == bestR {
		return incumbent
	}
	best := int64(0)
	for id, r := range rating {
		if r == bestR && (best == 0 || id < best) {
			best = id
		}
	}
	return best
}

// ComputeRatings replays games in order, deriving current ratings, peaks, the
// crown, and per-game deltas. Pure and deterministic: same input -> same output.
func ComputeRatings(games []RatingGame) Ratings {
	type pair struct{ win, lose int64 }

	rating := map[int64]int{}
	peak := map[int64]int{}
	played := map[int64]int{}
	ensure := func(id int64) {
		if _, ok := rating[id]; !ok {
			rating[id] = StartRating
			peak[id] = StartRating
		}
	}

	var perGame []GameRating
	crown := int64(0)

	for _, g := range games {
		var pairs []pair
		for i := 0; i < len(g.Participants); i++ {
			for j := i + 1; j < len(g.Participants); j++ {
				if w, l, ok := outcome(g.Participants[i], g.Participants[j]); ok {
					pairs = append(pairs, pair{win: w, lose: l})
				}
			}
		}
		if len(pairs) == 0 {
			continue // solo / all-DNF: no impact, no footer entry
		}

		opp := map[int64]int{}
		for _, pr := range pairs {
			ensure(pr.win)
			ensure(pr.lose)
			opp[pr.win]++
			opp[pr.lose]++
		}

		before := make(map[int64]int, len(opp))
		for id := range opp {
			before[id] = rating[id]
		}
		crownBefore := crown

		delta := make(map[int64]float64, len(opp))
		for _, pr := range pairs {
			kW := kFactor(played[pr.win]) / float64(opp[pr.win])
			kL := kFactor(played[pr.lose]) / float64(opp[pr.lose])
			eW := expectedScore(before[pr.win], before[pr.lose])
			eL := expectedScore(before[pr.lose], before[pr.win])
			delta[pr.win] += kW * (1 - eW)
			delta[pr.lose] += kL * (0 - eL)
		}

		gr := GameRating{
			GameID:      g.ID,
			Delta:       make(map[int64]int, len(opp)),
			NewRating:   make(map[int64]int, len(opp)),
			CrownBefore: crownBefore,
		}
		for id := range opp {
			d := int(math.Round(delta[id]))
			rating[id] = before[id] + d
			if rating[id] > peak[id] {
				peak[id] = rating[id]
			}
			played[id]++
			gr.Delta[id] = d
			gr.NewRating[id] = rating[id]
		}
		crown = topPlayer(rating, crown)
		gr.CrownAfter = crown
		perGame = append(perGame, gr)
	}

	players := make(map[int64]PlayerRating, len(rating))
	ladder := make([]PlayerRating, 0, len(rating))
	for id, r := range rating {
		pr := PlayerRating{
			PlayerID:    id,
			Rating:      r,
			Peak:        peak[id],
			Games:       played[id],
			Provisional: played[id] < ProvisionalGames,
		}
		players[id] = pr
		ladder = append(ladder, pr)
	}
	sort.Slice(ladder, func(i, j int) bool {
		if ladder[i].Rating != ladder[j].Rating {
			return ladder[i].Rating > ladder[j].Rating
		}
		if ladder[i].Peak != ladder[j].Peak {
			return ladder[i].Peak > ladder[j].Peak
		}
		return ladder[i].PlayerID < ladder[j].PlayerID
	})

	return Ratings{Players: players, Ladder: ladder, Crown: crown, PerGame: perGame}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/domain/ -v'`
Expected: PASS (all rating tests green).

- [ ] **Step 5: Vet + commit**

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./internal/domain/ && go test ./internal/domain/'
git add internal/domain/rating.go internal/domain/rating_test.go
git commit -m "feat(domain): pure ELO rating engine with crown + per-game deltas"
```

---

### Task 2: Storage — `GamesForRating` (`storage/rating.go`)

Feed the engine: every completed, non-deleted game for a chat (season + duels), in chronological (id) order, as `[]domain.RatingGame`.

**Files:**
- Create: `internal/storage/rating.go`
- Test: `internal/storage/rating_test.go`

**Interfaces:**
- Consumes: `domain.RatingGame`, `domain.RatingResult` (Task 1).
- Produces: `func (s *Store) GamesForRating(chatID int64) ([]domain.RatingGame, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/storage/rating_test.go`:

```go
package storage

import "testing"

func TestGamesForRating(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-100)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatal(err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatal(err)
	}
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")
	cp, _, _ := st.RegisterPlayer(chat, 3, "Carol")

	// Game 1: Alice 1st, Bob 2nd, Carol DNF.
	g1, _ := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
	if err := st.AddPick(g1, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddPick(g1, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDNF(g1, cp.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.FinalizeGame(g1, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	// Game 2: Bob 1st, Alice 2nd.
	g2, _ := st.CreatePendingGame(chat, se.ID, 1, "easy", "hardcore")
	_ = st.AddPick(g2, b.ID)
	_ = st.AddPick(g2, a.ID)
	if err := st.FinalizeGame(g2, se.PointsTable); err != nil {
		t.Fatal(err)
	}

	games, err := st.GamesForRating(chat)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("got %d games want 2", len(games))
	}
	if games[0].ID != g1 || games[1].ID != g2 {
		t.Errorf("chronological order broken: %d,%d want %d,%d",
			games[0].ID, games[1].ID, g1, g2)
	}
	if len(games[0].Participants) != 3 {
		t.Errorf("game1 participants=%d want 3", len(games[0].Participants))
	}
	// Carol must appear as DNF (rank 0) in game1.
	var sawDNF bool
	for _, p := range games[0].Participants {
		if p.PlayerID == cp.ID && p.Rank == 0 {
			sawDNF = true
		}
	}
	if !sawDNF {
		t.Errorf("Carol DNF (rank 0) missing from game1: %+v", games[0].Participants)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/storage/ -run TestGamesForRating -v'`
Expected: FAIL — `st.GamesForRating undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/storage/rating.go`:

```go
package storage

import "github.com/joerude/sudoku-bot-telegram/internal/domain"

// GamesForRating returns every completed, non-deleted game for a chat (season
// games AND duels) with its participants, ordered chronologically by game id.
// This is the full input to domain.ComputeRatings — the rating ignores the
// season/duel split entirely.
func (s *Store) GamesForRating(chatID int64) ([]domain.RatingGame, error) {
	rows, err := s.db.Query(`
		SELECT g.id, gr.player_id, gr.rank
		FROM games g
		JOIN game_results gr ON gr.game_id = g.id
		WHERE g.chat_id=? AND `+sqlCompletedGames+`
		ORDER BY g.id, gr.rank`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RatingGame
	idx := map[int64]int{}
	for rows.Next() {
		var gid, pid int64
		var rank int
		if err := rows.Scan(&gid, &pid, &rank); err != nil {
			return nil, err
		}
		i, ok := idx[gid]
		if !ok {
			out = append(out, domain.RatingGame{ID: gid})
			i = len(out) - 1
			idx[gid] = i
		}
		out[i].Participants = append(out[i].Participants,
			domain.RatingResult{PlayerID: pid, Rank: rank})
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/storage/ -run TestGamesForRating -v'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./internal/storage/ && go test ./internal/storage/'
git add internal/storage/rating.go internal/storage/rating_test.go
git commit -m "feat(storage): GamesForRating feeds the ELO engine from full history"
```

---

### Task 3: Result-post footer (`messages.go` builders + bot wiring)

Render the rating delta + crown-change line and append it to the existing result post in both the manual (`finalize`) and auto (`autoRecord`) paths.

**Files:**
- Modify: `internal/bot/messages.go` (add builders; ensure `domain` import)
- Modify: `internal/bot/handlers_game.go` (helper `ratingFooter`; append in `finalize`)
- Modify: `internal/bot/handlers_auto.go:213` (append footer before send)
- Test: `internal/bot/messages_test.go` (add builder tests)

**Interfaces:**
- Consumes: `domain.ComputeRatings`, `domain.GameRating` (Task 1); `Store.GamesForRating` (Task 2); `Store.ListPlayers` (existing).
- Produces:
  - `func ratingDeltaLines(gr domain.GameRating, names map[int64]string) string`
  - `func crownChangeLine(gr domain.GameRating, names map[int64]string) string`
  - `func (b *Bot) ratingFooter(game *storage.Game) string`

- [ ] **Step 1: Write the failing builder tests**

Add to `internal/bot/messages_test.go`:

```go
func TestRatingDeltaLines(t *testing.T) {
	names := map[int64]string{10: "Alice", 20: "Bob"}
	gr := domain.GameRating{
		GameID:      1,
		Delta:       map[int64]int{10: 14, 20: -9},
		NewRating:   map[int64]int{10: 1042, 20: 988},
		CrownBefore: 20,
		CrownAfter:  10,
	}
	got := ratingDeltaLines(gr, names)
	if !strings.Contains(got, "Alice +14 → 1042") {
		t.Errorf("missing winner line: %q", got)
	}
	if !strings.Contains(got, "Bob -9 → 988") {
		t.Errorf("missing loser line: %q", got)
	}
	if !strings.Contains(got, "👑") || !strings.Contains(got, "Alice") {
		t.Errorf("missing crown change: %q", got)
	}
}

func TestCrownChangeLineNoChange(t *testing.T) {
	names := map[int64]string{10: "Alice"}
	gr := domain.GameRating{CrownBefore: 10, CrownAfter: 10}
	if got := crownChangeLine(gr, names); got != "" {
		t.Errorf("expected empty when crown unchanged, got %q", got)
	}
}

func TestCrownChangeLineFirstCrown(t *testing.T) {
	names := map[int64]string{10: "Alice"}
	gr := domain.GameRating{CrownBefore: 0, CrownAfter: 10}
	got := crownChangeLine(gr, names)
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "👑") {
		t.Errorf("first crown line wrong: %q", got)
	}
}
```

(If `messages_test.go` does not already import `domain` / `strings`, add them.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "RatingDeltaLines|CrownChange" -v'`
Expected: FAIL — `undefined: ratingDeltaLines`, `undefined: crownChangeLine`.

- [ ] **Step 3: Add the builders**

Add to `internal/bot/messages.go` (ensure the import block has
`"github.com/joerude/sudoku-bot-telegram/internal/domain"`, `"sort"`, `"strings"`, `"fmt"`):

```go
// ratingDeltaLines renders a game's rating impact as the result-post footer:
// players ordered by delta (biggest gainer first), plus a crown-change line.
func ratingDeltaLines(gr domain.GameRating, names map[int64]string) string {
	type row struct {
		id, d, nr int64
	}
	rows := make([]row, 0, len(gr.Delta))
	for id, d := range gr.Delta {
		rows = append(rows, row{id: id, d: int64(d), nr: int64(gr.NewRating[id])})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].d != rows[j].d {
			return rows[i].d > rows[j].d
		}
		return rows[i].id < rows[j].id
	})
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		sign := ""
		if r.d >= 0 {
			sign = "+"
		}
		parts = append(parts, fmt.Sprintf("%s %s%d → %d", esc(names[r.id]), sign, r.d, r.nr))
	}
	out := "\n\n📊 Рейтинг: " + strings.Join(parts, ", ")
	if line := crownChangeLine(gr, names); line != "" {
		out += "\n" + line
	}
	return out
}

// crownChangeLine announces a new #1 after a game (empty when unchanged).
func crownChangeLine(gr domain.GameRating, names map[int64]string) string {
	if gr.CrownAfter == 0 || gr.CrownAfter == gr.CrownBefore {
		return ""
	}
	if gr.CrownBefore == 0 {
		return fmt.Sprintf("👑 %s забирает корону!", esc(names[gr.CrownAfter]))
	}
	return fmt.Sprintf("👑 Корона сменилась: %s свергает %s!",
		esc(names[gr.CrownAfter]), esc(names[gr.CrownBefore]))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "RatingDeltaLines|CrownChange" -v'`
Expected: PASS.

- [ ] **Step 5: Add the `ratingFooter` helper and wire both result paths**

Add to `internal/bot/handlers_game.go` (near `scoreAndCheck`):

```go
// ratingFooter computes the rating impact of the just-recorded game and renders
// the result-post footer (delta lines + optional crown change). Best-effort:
// returns "" on any error or when the game had no rating impact (solo/all-DNF),
// so rating never blocks or breaks the result post.
func (b *Bot) ratingFooter(game *storage.Game) string {
	games, err := b.st.GamesForRating(game.ChatID)
	if err != nil {
		log.Printf("ratingFooter.games: %v", err)
		return ""
	}
	r := domain.ComputeRatings(games)
	var gr *domain.GameRating
	for i := range r.PerGame {
		if r.PerGame[i].GameID == game.ID {
			gr = &r.PerGame[i]
			break
		}
	}
	if gr == nil {
		return ""
	}
	players, err := b.st.ListPlayers(game.ChatID)
	if err != nil {
		log.Printf("ratingFooter.players: %v", err)
		return ""
	}
	names := make(map[int64]string, len(players))
	for _, p := range players {
		names[p.ID] = p.Name
	}
	return ratingDeltaLines(*gr, names)
}
```

Ensure `handlers_game.go` imports `"github.com/joerude/sudoku-bot-telegram/internal/domain"`.

In `finalize` ([handlers_game.go:557](../../internal/bot/handlers_game.go)), append the footer to `result` right after the `scoreAndCheck` block and before the `c.Edit`. Change:

```go
	result, seasonEnd, err := b.scoreAndCheck(game)
	if err != nil {
		return b.fail(c, "finalize", err)
	}
	if len(noNick) > 0 {
		result += fmt.Sprintf("\n\n⏱ Время не подтянулось: <b>%s</b> — задайте /setnick",
			esc(strings.Join(noNick, ", ")))
	}
	result += b.ratingFooter(game)
	if err := c.Edit(result, resultKeyboard(game.ID)); err != nil {
```

In `autoRecord` ([handlers_auto.go:206-213](../../internal/bot/handlers_auto.go)), append before the send:

```go
	result, seasonEnd, err := b.scoreAndCheck(game)
	if err != nil {
		log.Printf("autoRecord.score: %v", err)
		_, _ = b.tb.Send(to, "⚠️ Не удалось авто-записать результат. Запишите вручную:",
			recordKeyboard(game.ID))
		return
	}
	result += b.ratingFooter(game)
	_, _ = b.tb.Send(to, autoResultHeader()+result, resultKeyboard(game.ID))
```

- [ ] **Step 6: Build + full test suite**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS (compiles; all tests green).

- [ ] **Step 7: Commit**

```bash
git add internal/bot/messages.go internal/bot/messages_test.go internal/bot/handlers_game.go internal/bot/handlers_auto.go
git commit -m "feat(bot): append rating delta + crown change to result posts"
```

---

### Task 4: `/rating` ladder command (`handlers_rating.go` + route + help)

A command that prints the eternal ladder with the crown, peak, and provisional markers.

**Files:**
- Create: `internal/bot/handlers_rating.go`
- Modify: `internal/bot/messages.go` (add `ratingLadder`)
- Modify: `internal/bot/bot.go:136` area (route `/rating`)
- Modify: `internal/bot/messages.go` help text (add `/rating` line)
- Test: `internal/bot/messages_test.go` (ladder render test)

**Interfaces:**
- Consumes: `domain.ComputeRatings`, `domain.Ratings` (Task 1); `Store.GamesForRating` (Task 2); `Store.ListPlayers` (existing); `b.fail` (existing).
- Produces:
  - `func ratingLadder(r domain.Ratings, names map[int64]string) string`
  - `func (b *Bot) onRating(c tele.Context) error`

- [ ] **Step 1: Write the failing ladder test**

Add to `internal/bot/messages_test.go`:

```go
func TestRatingLadder(t *testing.T) {
	names := map[int64]string{10: "Alice", 20: "Bob"}
	r := domain.Ratings{
		Crown: 10,
		Ladder: []domain.PlayerRating{
			{PlayerID: 10, Rating: 1042, Peak: 1042, Games: 12, Provisional: false},
			{PlayerID: 20, Rating: 988, Peak: 1010, Games: 4, Provisional: true},
		},
	}
	got := ratingLadder(r, names)
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "1042") {
		t.Errorf("missing leader: %q", got)
	}
	if !strings.Contains(got, "👑") {
		t.Errorf("missing crown on leader: %q", got)
	}
	if !strings.Contains(got, "калибр") {
		t.Errorf("missing provisional marker on Bob: %q", got)
	}
}

func TestRatingLadderEmpty(t *testing.T) {
	got := ratingLadder(domain.Ratings{}, map[int64]string{})
	if !strings.Contains(got, "нет") {
		t.Errorf("empty ladder should say there are no games: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestRatingLadder -v'`
Expected: FAIL — `undefined: ratingLadder`.

- [ ] **Step 3: Add the `ratingLadder` builder**

Add to `internal/bot/messages.go`:

```go
// ratingLadder renders the eternal rating ladder: rank, name, rating, peak, with
// a 👑 on the current crown and a marker on provisional (calibrating) players.
func ratingLadder(r domain.Ratings, names map[int64]string) string {
	if len(r.Ladder) == 0 {
		return "📊 <b>Рейтинг</b>\n\nПока нет сыгранных игр."
	}
	var sb strings.Builder
	sb.WriteString("📊 <b>Рейтинг</b>")
	for i, p := range r.Ladder {
		crown := ""
		if p.PlayerID == r.Crown {
			crown = " 👑"
		}
		prov := ""
		if p.Provisional {
			prov = " <i>(калибр.)</i>"
		}
		sb.WriteString(fmt.Sprintf("\n%d. <b>%s</b> — %d (пик %d)%s%s",
			i+1, esc(names[p.PlayerID]), p.Rating, p.Peak, crown, prov))
	}
	return sb.String()
}
```

- [ ] **Step 4: Add the handler**

Create `internal/bot/handlers_rating.go`:

```go
package bot

import (
	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	tele "gopkg.in/telebot.v3"
)

// onRating shows the eternal ELO ladder for the chat.
func (b *Bot) onRating(c tele.Context) error {
	games, err := b.st.GamesForRating(c.Chat().ID)
	if err != nil {
		return b.fail(c, "rating", err)
	}
	players, err := b.st.ListPlayers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "rating", err)
	}
	names := make(map[int64]string, len(players))
	for _, p := range players {
		names[p.ID] = p.Name
	}
	return c.Send(ratingLadder(domain.ComputeRatings(games), names))
}
```

- [ ] **Step 5: Register the route**

In `internal/bot/bot.go`, in the stats group (after `b.tb.Handle("/me", b.onMe)` around [bot.go:133](../../internal/bot/bot.go)), add:

```go
	b.tb.Handle("/rating", b.onRating)
```

- [ ] **Step 6: Add `/rating` to the help text**

In `internal/bot/messages.go`, find the help/command list (the builder used by `onHelp`; grep for the line containing `/stats`) and add a line next to the stats commands:

```
/rating — лестница рейтинга (ELO) + корона 👑
```

Match the surrounding formatting exactly (same prefix/escaping as adjacent command lines).

- [ ] **Step 7: Build + full test suite**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/bot/handlers_rating.go internal/bot/messages.go internal/bot/messages_test.go internal/bot/bot.go
git commit -m "feat(bot): /rating eternal ELO ladder with crown"
```

---

### Task 5: Docs — CLAUDE.md group size + rating model

Fix the stale group size and document the rating model for future contributors.

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Fix the group size**

In `CLAUDE.md`, change the opening description from "Группа из ~5 друзей." to "Группа из 3 друзей."

- [ ] **Step 2: Document the rating model**

In `CLAUDE.md` under "## Модель данных (неочевидное)", add a bullet:

```
- **Рейтинг (ELO)**: чистая функция истории — `domain.ComputeRatings` прогоняет
  ВСЕ завершённые игры (`sqlCompletedGames`, сезон + дуэли), без таблицы и без
  инкрементального стейта (`storage.GamesForRating` — вход). Вечный (не сбрасывается
  по сезонам), параллелен очкам сезона. Чистый ранк, многопользовательская =
  попарно (K делится на число оппонентов), DNF = поражение финишёрам (DNF-vs-DNF
  игнор). Старт 1000; provisional <10 игр K=64, иначе K=32. Корона = текущий №1
  (при равенстве держит incumbent). Дельта пишется в пост результата
  (`ratingFooter`), полная лестница — `/rating`. Edit/delete игры → следующий
  replay сам пересчитает.
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: record rating model and correct group size in CLAUDE.md"
```

---

## Self-Review

**Spec coverage:**
- Engine (ELO, pure rank, pairwise, K-split, DNF=loss, eternal, on-demand replay, provisional, peak, crown, crown-change) → Task 1. ✓
- Feed from all completed games (season + duels), chronological → Task 2. ✓
- Delivery: delta in result post + crown line → Task 3; `/rating` ladder → Task 4. ✓
- Backfill on first deploy → free: `GamesForRating` already returns full history, no special task needed. ✓
- CLAUDE.md group size + rating model → Task 5. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code. The one
"grep for the help line" step (Task 4 Step 6) is a locate-then-insert with the
exact line to add — acceptable since the help text is existing user copy not worth
reproducing in full.

**Type consistency:** `ComputeRatings`, `RatingGame`, `RatingResult`,
`PlayerRating`, `GameRating`, `Ratings`, `GamesForRating`, `ratingDeltaLines`,
`crownChangeLine`, `ratingFooter`, `ratingLadder`, `onRating` — names and
signatures match across all tasks and the spec.
```
