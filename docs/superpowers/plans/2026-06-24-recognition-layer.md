# Recognition Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an all-time records board (a new `/stats` tab), win/play-day streaks + achievement badges (computed-on-read, shown in `/me`), and a weekly digest — reusing existing render, storage, and reminder infrastructure.

**Architecture:** Pure functions in `internal/domain` (streak/badge logic) + thin SQL aggregates in `internal/storage` + render builders in `internal/bot/messages.go` + wiring into the existing `/stats` tab router and the `runReminders` minute-tick. Computed-on-read: no achievements table. One additive migration (two `chats` columns) for the digest's once-per-week guard + toggle.

**Tech Stack:** Go 1.25, telebot.v3 (HTML parse mode), modernc sqlite. No host `go` — all build/test in docker.

## Global Constraints

- Host has NO `go`. Every build/vet/test runs in docker:
  - Focused: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/<pkg>/ -run <TestName> -v'`
  - Full: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
- Conventional Commits. NO `Co-Authored-By` trailer.
- Telegram HTML parse mode: all player-supplied text (names) through `esc()`.
- Records/streaks/digest exclude duels and non-final games: every query carries `g.status='completed' AND g.deleted=0 AND g.duel_target_id IS NULL`. Records additionally require `gr.duration_secs IS NOT NULL`.
- Do NOT modify scoring (`internal/domain/scoring.go`), season rollover, the picker, auto-record, or duel logic.
- `fmtDuration(secs int) string` (m:ss) and `medal(rank int) string` already exist in `internal/bot/messages.go` — reuse, don't redefine.
- Repo convention: pure functions (domain logic, render builders) get unit tests; SQL queries and Telegram/reminder glue are verified by manual smoke.

## File structure

- `internal/domain/streaks.go` *(new)* — pure `WinStreak`, `DayStreak`, `Badges`, `BadgeInput`, `addDays`.
- `internal/domain/streaks_test.go` *(new)* — unit tests.
- `internal/storage/stats.go` — add `RecordsBoard`, `RecentRanks`, `PlayedTimes`, `CareerStats`, `SeasonsWon`, `GamesSince`, `FastestSince` + their row types.
- `internal/storage/reminders.go` — `ChatSettings` += fields; `AllChats` select; `SetWeeklyDigest`, `SetLastWeekly`.
- `internal/storage/store.go` — two ALTER lines in `migrate()`.
- `internal/storage/schema.sql` — two columns on `chats`.
- `internal/bot/messages.go` — `recordsText`, `streakBadgeText`, `digestText`, time helpers `loadLoc`, `parseDBTime`.
- `internal/bot/keyboards.go` — add `records` to `statsTabs`.
- `internal/bot/handlers_stats.go` — `statsView` records case; `meExtra` helper; append to `meTab` + `onMe`; `/settings weekly` case.
- `internal/bot/reminders.go` — `remindWeekly` + call in `runReminders`.
- `internal/bot/messages_test.go` — render tests.

---

## Task 1: Records board tab (Slice A)

**Files:**
- Modify: `internal/storage/stats.go`
- Modify: `internal/bot/messages.go`
- Modify: `internal/bot/keyboards.go`
- Modify: `internal/bot/handlers_stats.go`
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `storage.RecordRow{Difficulty string; Secs int; Name string}`, `b.st.RecordsBoard(chatID int64) ([]storage.RecordRow, error)`, `recordsText([]storage.RecordRow) string`, `records` tab.
- Consumes: existing `fmtDuration`, `esc`, `titleCase`, `statsKeyboard`, `statsView` switch.

- [ ] **Step 1: Write the failing render test**

Add to `internal/bot/messages_test.go`:

```go
func TestRecordsTextRendersHoldersAndEmpty(t *testing.T) {
	rows := []storage.RecordRow{
		{Difficulty: "easy", Secs: 72, Name: "Ann"},
		{Difficulty: "medium", Secs: 118, Name: "Joe<b>"},
	}
	out := recordsText(rows)
	for _, want := range []string{"1:12", "Ann", "1:58", "Easy", "Medium", "Joe&lt;b&gt;"} {
		if !strings.Contains(out, want) {
			t.Errorf("recordsText: want %q in\n%s", want, out)
		}
	}
	if empty := recordsText(nil); !strings.Contains(empty, "Пока нет") {
		t.Errorf("empty recordsText should hint, got: %s", empty)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestRecordsText -v'`
Expected: FAIL — `undefined: recordsText`, `undefined: storage.RecordRow`.

- [ ] **Step 3: Add the storage type + query**

In `internal/storage/stats.go` add:

```go
// RecordRow is the fastest all-time solve at one difficulty and who set it.
type RecordRow struct {
	Difficulty string
	Secs       int
	Name       string
}

// RecordsBoard returns the fastest auto-recorded solve per difficulty across
// all seasons (duels and deleted/pending games excluded). SQLite returns the
// row matching MIN() for the bare columns in a GROUP BY.
func (s *Store) RecordsBoard(chatID int64) ([]RecordRow, error) {
	rows, err := s.db.Query(`
		SELECT g.difficulty, MIN(gr.duration_secs) AS best, p.name
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id = ? AND g.status = 'completed' AND g.deleted = 0
		  AND g.duel_target_id IS NULL AND gr.duration_secs IS NOT NULL
		  AND g.difficulty IS NOT NULL AND g.difficulty <> ''
		GROUP BY g.difficulty`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordRow
	for rows.Next() {
		var r RecordRow
		if err := rows.Scan(&r.Difficulty, &r.Secs, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Add the render builder**

In `internal/bot/messages.go` add (uses existing `fmtDuration`, `titleCase`, `esc`):

```go
// difficultyRank orders records easy < medium < hard < extreme < other.
var difficultyRank = map[string]int{"easy": 0, "medium": 1, "hard": 2, "extreme": 3}

// recordsText renders the all-time fastest solve per difficulty.
func recordsText(rows []storage.RecordRow) string {
	if len(rows) == 0 {
		return "🏆 <b>Рекорды</b>\nПока нет рекордов — сыграйте авто-игру (нужен /setnick), и время попадёт сюда."
	}
	sorted := make([]storage.RecordRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, ok := difficultyRank[sorted[i].Difficulty]
		if !ok {
			ri = 99
		}
		rj, ok := difficultyRank[sorted[j].Difficulty]
		if !ok {
			rj = 99
		}
		return ri < rj
	})
	var b strings.Builder
	b.WriteString("🏆 <b>Рекорды</b> · лучшее время\n")
	for _, r := range sorted {
		fmt.Fprintf(&b, "<b>%s</b> — %s · %s\n", titleCase(r.Difficulty), fmtDuration(r.Secs), esc(r.Name))
	}
	return b.String()
}
```

Add `"sort"` to the import block of `internal/bot/messages.go` if not present.

- [ ] **Step 5: Run test, verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestRecordsText -v'`
Expected: PASS.

- [ ] **Step 6: Wire the tab**

In `internal/bot/keyboards.go`, add to `statsTabs` after the `history` entry:

```go
	{"records", "🏆 Рекорды"},
```

In `internal/bot/handlers_stats.go` `statsView`, add a case before `default:`:

```go
	case "records":
		recs, e := b.st.RecordsBoard(chatID)
		if e != nil {
			return "", nil, e
		}
		text = recordsText(recs)
```

- [ ] **Step 7: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/storage/stats.go internal/bot/messages.go internal/bot/keyboards.go internal/bot/handlers_stats.go internal/bot/messages_test.go
git commit -m "feat(stats): add all-time records tab"
```

---

## Task 2: Streak + badge computation (Slice B)

**Files:**
- Create: `internal/domain/streaks.go`
- Create: `internal/domain/streaks_test.go`
- Modify: `internal/storage/stats.go`

**Interfaces:**
- Produces: `domain.WinStreak([]int) int`, `domain.DayStreak(dates []string, today string) int`, `domain.Badges(domain.BadgeInput) []string`, `domain.BadgeInput`; `b.st.RecentRanks(chatID, playerID) ([]int, error)`, `b.st.PlayedTimes(chatID, playerID) ([]string, error)`, `b.st.CareerStats(chatID, playerID) (wins, games, bestSecs int, err error)`, `b.st.SeasonsWon(chatID, playerID) (int, error)`.

- [ ] **Step 1: Write the failing domain tests**

Create `internal/domain/streaks_test.go`:

```go
package domain

import "testing"

func TestWinStreak(t *testing.T) {
	cases := []struct {
		ranks []int
		want  int
	}{
		{[]int{1, 1, 2, 1}, 2},
		{[]int{2, 1, 1}, 0},
		{[]int{1, 1, 1}, 3},
		{nil, 0},
	}
	for _, c := range cases {
		if got := WinStreak(c.ranks); got != c.want {
			t.Errorf("WinStreak(%v)=%d want %d", c.ranks, got, c.want)
		}
	}
}

func TestDayStreak(t *testing.T) {
	// played today, yesterday, two days ago → 3
	dates := []string{"2026-06-24", "2026-06-23", "2026-06-22"}
	if got := DayStreak(dates, "2026-06-24"); got != 3 {
		t.Errorf("consecutive: got %d want 3", got)
	}
	// gap breaks it: today + 3 days ago → 1
	if got := DayStreak([]string{"2026-06-24", "2026-06-21"}, "2026-06-24"); got != 1 {
		t.Errorf("gap: got %d want 1", got)
	}
	// not played today but yesterday → streak still alive, counts back
	if got := DayStreak([]string{"2026-06-23", "2026-06-22"}, "2026-06-24"); got != 2 {
		t.Errorf("ended-yesterday: got %d want 2", got)
	}
	// last play older than yesterday → broken
	if got := DayStreak([]string{"2026-06-21"}, "2026-06-24"); got != 0 {
		t.Errorf("stale: got %d want 0", got)
	}
	if got := DayStreak(nil, "2026-06-24"); got != 0 {
		t.Errorf("empty: got %d want 0", got)
	}
}

func TestBadges(t *testing.T) {
	all := Badges(BadgeInput{Wins: 50, Games: 100, BestSecs: 90, WinStreak: 3, DayStreak: 7, SeasonsWon: 1})
	for _, want := range []string{"🏅", "🔥", "⚡", "💪", "💯", "🎯", "📅"} {
		found := false
		for _, b := range all {
			if b == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Badges full set missing %q (got %v)", want, all)
		}
	}
	if none := Badges(BadgeInput{Wins: 1, Games: 1}); len(none) != 0 {
		t.Errorf("no badges expected, got %v", none)
	}
	// BestSecs==0 (no timed games) must NOT grant the sub-2:00 badge
	if b := Badges(BadgeInput{BestSecs: 0}); len(b) != 0 {
		t.Errorf("BestSecs 0 should grant nothing, got %v", b)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/domain/ -run "TestWinStreak|TestDayStreak|TestBadges" -v'`
Expected: FAIL — undefined `WinStreak`, `DayStreak`, `Badges`, `BadgeInput`.

- [ ] **Step 3: Implement the pure functions**

Create `internal/domain/streaks.go`:

```go
package domain

import "time"

// WinStreak counts leading wins (rank==1) in a newest-first rank slice.
func WinStreak(ranks []int) int {
	n := 0
	for _, r := range ranks {
		if r != 1 {
			break
		}
		n++
	}
	return n
}

// addDays returns the YYYY-MM-DD date n days from the given date; on parse
// failure it returns the input unchanged (callers treat that as a broken chain).
func addDays(date string, n int) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// DayStreak counts consecutive calendar days with at least one play, anchored
// at the most recent played day, but only if that day is today or yesterday
// (otherwise the streak is considered broken and returns 0). dates may contain
// duplicates and any order; today is YYYY-MM-DD in the chat's timezone.
func DayStreak(dates []string, today string) int {
	set := make(map[string]bool, len(dates))
	for _, d := range dates {
		set[d] = true
	}
	cur := today
	if !set[cur] {
		cur = addDays(today, -1)
		if !set[cur] {
			return 0
		}
	}
	n := 0
	for set[cur] {
		n++
		cur = addDays(cur, -1)
	}
	return n
}

// BadgeInput is the cross-season career data a player's badges are computed from.
type BadgeInput struct {
	Wins       int
	Games      int
	BestSecs   int
	WinStreak  int
	DayStreak  int
	SeasonsWon int
}

// Badges returns the emoji badges a player has earned, in a fixed display order.
func Badges(in BadgeInput) []string {
	var b []string
	if in.SeasonsWon >= 1 {
		b = append(b, "🏅") // season champion
	}
	if in.WinStreak >= 3 {
		b = append(b, "🔥") // hot streak
	}
	if in.BestSecs > 0 && in.BestSecs < 120 {
		b = append(b, "⚡") // sub-2:00 solve
	}
	if in.Wins >= 10 {
		b = append(b, "💪") // 10+ wins
	}
	if in.Wins >= 50 {
		b = append(b, "💯") // 50+ wins
	}
	if in.Games >= 100 {
		b = append(b, "🎯") // 100+ games
	}
	if in.DayStreak >= 7 {
		b = append(b, "📅") // week-long play streak
	}
	return b
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/domain/ -run "TestWinStreak|TestDayStreak|TestBadges" -v'`
Expected: PASS.

- [ ] **Step 5: Add the storage aggregates**

In `internal/storage/stats.go` add:

```go
// RecentRanks returns a player's finishing ranks for completed non-duel games,
// newest first — for win-streak computation.
func (s *Store) RecentRanks(chatID, playerID int64) ([]int, error) {
	rows, err := s.db.Query(`
		SELECT gr.rank
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND g.status = 'completed' AND g.deleted = 0 AND g.duel_target_id IS NULL
		ORDER BY g.completed_at DESC, g.id DESC`, chatID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var r int
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PlayedTimes returns completed_at timestamps (UTC, "2006-01-02 15:04:05") of a
// player's completed non-duel games, newest first — for play-day streaks.
func (s *Store) PlayedTimes(chatID, playerID int64) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT g.completed_at
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND g.status = 'completed' AND g.deleted = 0 AND g.duel_target_id IS NULL
		  AND g.completed_at IS NOT NULL
		ORDER BY g.completed_at DESC`, chatID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CareerStats returns a player's cross-season totals: wins (rank=1), games
// played (any rank incl. DNF), and best solve seconds (0 if no timed games).
func (s *Store) CareerStats(chatID, playerID int64) (wins, games, bestSecs int, err error) {
	var best sql.NullInt64
	err = s.db.QueryRow(`
		SELECT
		  SUM(CASE WHEN gr.rank = 1 THEN 1 ELSE 0 END),
		  COUNT(*),
		  MIN(gr.duration_secs)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND g.status = 'completed' AND g.deleted = 0 AND g.duel_target_id IS NULL`,
		chatID, playerID).Scan(&wins, &games, &best)
	if err != nil {
		return 0, 0, 0, err
	}
	if best.Valid {
		bestSecs = int(best.Int64)
	}
	return wins, games, bestSecs, nil
}

// SeasonsWon counts archived seasons this player won.
func (s *Store) SeasonsWon(chatID, playerID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM seasons WHERE chat_id = ? AND winner_id = ?`,
		chatID, playerID).Scan(&n)
	return n, err
}
```

Note: `SUM(...)` over zero rows yields SQL NULL, but `COUNT(*)` guarantees the row exists; when a player has rows, `SUM` is non-NULL. A player with zero games scans `wins` from `SUM`→NULL into an `int`, which fails. `CareerStats` is only called for players who appear in standings/results, but to be safe, scan `wins` via `sql.NullInt64` too:

Replace the `wins` scan with a nullable and coalesce:

```go
	var best, winsN sql.NullInt64
	err = s.db.QueryRow(`...`, chatID, playerID).Scan(&winsN, &games, &best)
	if err != nil {
		return 0, 0, 0, err
	}
	if winsN.Valid {
		wins = int(winsN.Int64)
	}
	if best.Valid {
		bestSecs = int(best.Int64)
	}
	return wins, games, bestSecs, nil
```

(`sql` is already imported in stats.go.)

- [ ] **Step 6: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/streaks.go internal/domain/streaks_test.go internal/storage/stats.go
git commit -m "feat(stats): streak + badge computation (pure + storage)"
```

---

## Task 3: Show streaks + badges in /me (Slice C)

**Files:**
- Modify: `internal/bot/messages.go` (`streakBadgeText`, `loadLoc`, `parseDBTime`)
- Modify: `internal/bot/handlers_stats.go` (`meExtra`, append in `meTab` and `onMe`)
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Consumes: Task 2's `domain.WinStreak/DayStreak/Badges/BadgeInput`, `b.st.RecentRanks/PlayedTimes/CareerStats/SeasonsWon`, `b.st.GetChat`.
- Produces: `streakBadgeText(winStreak, dayStreak int, badges []string) string`, `b.meExtra(chatID, playerID int64, tz string) string`.

- [ ] **Step 1: Write the failing render test**

Add to `internal/bot/messages_test.go`:

```go
func TestStreakBadgeText(t *testing.T) {
	out := streakBadgeText(4, 6, []string{"🔥", "⚡"})
	for _, want := range []string{"Серия побед", "4", "Дней подряд", "6", "🔥", "⚡"} {
		if !strings.Contains(out, want) {
			t.Errorf("streakBadgeText: want %q in %q", want, out)
		}
	}
	// zeros + no badges → empty string (nothing appended)
	if s := streakBadgeText(0, 0, nil); s != "" {
		t.Errorf("expected empty, got %q", s)
	}
	// win streak of 1 is not a "streak" worth showing
	if s := streakBadgeText(1, 0, nil); s != "" {
		t.Errorf("streak of 1 should render empty, got %q", s)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestStreakBadgeText -v'`
Expected: FAIL — `undefined: streakBadgeText`.

- [ ] **Step 3: Implement the render builder + time helpers**

In `internal/bot/messages.go` add:

```go
// streakBadgeText renders the streak lines + badge row appended to /me. Returns
// "" when there is nothing noteworthy (streaks < 2 and no badges).
func streakBadgeText(winStreak, dayStreak int, badges []string) string {
	var b strings.Builder
	if winStreak >= 2 {
		fmt.Fprintf(&b, "\n🔥 Серия побед: <b>%d</b>", winStreak)
	}
	if dayStreak >= 2 {
		fmt.Fprintf(&b, "\n📅 Дней подряд: <b>%d</b>", dayStreak)
	}
	if len(badges) > 0 {
		b.WriteString("\n🏆 " + strings.Join(badges, " "))
	}
	return b.String()
}

// loadLoc loads a timezone, falling back to UTC.
func loadLoc(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// parseDBTime parses a SQLite datetime('now') string (UTC) into a time.Time.
func parseDBTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02 15:04:05", s)
}
```

Add `"time"` to the `internal/bot/messages.go` import block if not present.

- [ ] **Step 4: Run test, verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestStreakBadgeText -v'`
Expected: PASS.

- [ ] **Step 5: Add meExtra and append it in both /me surfaces**

In `internal/bot/handlers_stats.go` add the import `"github.com/joerude/sudoku-bot-telegram/internal/domain"` (and `"time"` if not present), then:

```go
// meExtra computes a player's streak lines + badge row (best-effort; returns ""
// on any storage error so the core /me view still renders).
func (b *Bot) meExtra(chatID, playerID int64, tz string) string {
	ranks, err := b.st.RecentRanks(chatID, playerID)
	if err != nil {
		log.Printf("meExtra.ranks: %v", err)
		return ""
	}
	times, err := b.st.PlayedTimes(chatID, playerID)
	if err != nil {
		log.Printf("meExtra.times: %v", err)
		return ""
	}
	loc := loadLoc(tz)
	today := time.Now().In(loc).Format("2006-01-02")
	dates := make([]string, 0, len(times))
	for _, t := range times {
		if parsed, e := parseDBTime(t); e == nil {
			dates = append(dates, parsed.UTC().In(loc).Format("2006-01-02"))
		}
	}
	wins, games, best, err := b.st.CareerStats(chatID, playerID)
	if err != nil {
		log.Printf("meExtra.career: %v", err)
		return ""
	}
	seasonsWon, err := b.st.SeasonsWon(chatID, playerID)
	if err != nil {
		log.Printf("meExtra.seasonsWon: %v", err)
		return ""
	}
	ws := domain.WinStreak(ranks)
	ds := domain.DayStreak(dates, today)
	badges := domain.Badges(domain.BadgeInput{
		Wins: wins, Games: games, BestSecs: best,
		WinStreak: ws, DayStreak: ds, SeasonsWon: seasonsWon,
	})
	return streakBadgeText(ws, ds, badges)
}

// chatTZ returns the chat's timezone string, defaulting to UTC on error.
func (b *Bot) chatTZ(chatID int64) string {
	if ch, err := b.st.GetChat(chatID); err == nil && ch.TZ != "" {
		return ch.TZ
	}
	return "UTC"
}
```

`log` is already imported in handlers_stats.go.

In `meTab`, change the final return (currently `return meText(player.Name, stat, sp, duelW, duelL, season), nil`) to append:

```go
	return meText(player.Name, stat, sp, duelW, duelL, season) +
		b.meExtra(c.Chat().ID, player.ID, b.chatTZ(c.Chat().ID)), nil
```

In `onMe` (same file), change its final `return c.Send(meText(...))` to:

```go
	return c.Send(meText(player.Name, stat, sp, duelW, duelL, season) +
		b.meExtra(c.Chat().ID, player.ID, b.chatTZ(c.Chat().ID)))
```

(Match the exact local variable names already used in `onMe` for player/stat/sp/duelW/duelL/season.)

- [ ] **Step 6: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 7: Manual smoke (deferred — needs deployed bot)**

`/stats` → `👤 Я` tab and `/me` should show streak lines + badges for a player with a win streak. Cannot run without a live bot; note as deferred in the report.

- [ ] **Step 8: Commit**

```bash
git add internal/bot/messages.go internal/bot/handlers_stats.go internal/bot/messages_test.go
git commit -m "feat(stats): show streaks + badges in /me"
```

---

## Task 4: Weekly digest (Slice D)

**Files:**
- Modify: `internal/storage/schema.sql`, `internal/storage/store.go`, `internal/storage/reminders.go`, `internal/storage/stats.go`
- Modify: `internal/bot/messages.go`, `internal/bot/reminders.go`, `internal/bot/handlers_stats.go`
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `chats.weekly_digest`, `chats.last_weekly`; `ChatSettings.WeeklyDigest bool`, `ChatSettings.LastWeekly string`; `b.st.SetWeeklyDigest(chatID int64, on bool) error`, `b.st.SetLastWeekly(chatID int64, date string) error`, `b.st.GamesSince(chatID int64, sinceUTC string) (int, error)`, `b.st.FastestSince(chatID int64, sinceUTC string) (*storage.RecordRow, error)`; `digestText(...) string`; `b.remindWeekly()`.
- Consumes: Task 2's `RecentRanks`+`domain.WinStreak`, existing `Standings`, `ListPlayers`, `loadLoc`, `medal`, `esc`, `fmtDuration`.

- [ ] **Step 1: Write the failing render test**

Add to `internal/bot/messages_test.go`:

```go
func TestDigestText(t *testing.T) {
	se := &storage.Season{Number: 3, Target: 100}
	top := []storage.Standing{
		{Name: "Ann", Points: 24, Wins: 8}, {Name: "Joe", Points: 15, Wins: 5}, {Name: "Max", Points: 9, Wins: 3},
	}
	fastest := &storage.RecordRow{Difficulty: "medium", Secs: 118, Name: "Joe"}
	out := digestText(se, top, fastest, "Ann", 4, 11)
	for _, want := range []string{"Сезон 3", "Ann", "Joe", "Max", "1:58", "🔥", "Ann", "11"} {
		if !strings.Contains(out, want) {
			t.Errorf("digestText: want %q in\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestDigestText -v'`
Expected: FAIL — `undefined: digestText`.

- [ ] **Step 3: Implement digestText**

In `internal/bot/messages.go` add (top is already capped to ≤3 by the caller):

```go
// digestText renders the weekly digest: top-3 standings, the week's fastest
// solve, the longest current win streak, and games played this week. fastest
// and streakName may be empty when there is no data for them.
func digestText(se *storage.Season, top []storage.Standing, fastest *storage.RecordRow, streakName string, streakLen, weekGames int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📅 <b>Итоги недели</b> · Сезон %d\n\n", se.Number)
	for i, s := range top {
		fmt.Fprintf(&b, "%s <b>%s</b> — %d <i>(%d поб)</i>\n", medal(i+1), esc(s.Name), s.Points, s.Wins)
	}
	fmt.Fprintf(&b, "\n🎮 Игр за неделю: <b>%d</b>", weekGames)
	if fastest != nil {
		fmt.Fprintf(&b, "\n⚡ Быстрейшее: <b>%s</b> · %s · %s",
			fmtDuration(fastest.Secs), titleCase(fastest.Difficulty), esc(fastest.Name))
	}
	if streakName != "" && streakLen >= 2 {
		fmt.Fprintf(&b, "\n🔥 Серия побед: <b>%s</b> ×%d", esc(streakName), streakLen)
	}
	return b.String()
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestDigestText -v'`
Expected: PASS.

- [ ] **Step 5: Schema + migration + ChatSettings**

In `internal/storage/schema.sql`, inside the `chats` CREATE, add two columns after `last_daily`:

```sql
    weekly_digest  INTEGER NOT NULL DEFAULT 1,        -- bool: send weekly digest
    last_weekly    TEXT,                              -- date (YYYY-MM-DD) of last digest
```

In `internal/storage/store.go` `migrate()`, append to the ALTER list:

```go
		`ALTER TABLE chats ADD COLUMN weekly_digest INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE chats ADD COLUMN last_weekly TEXT`,
```

In `internal/storage/reminders.go`, add fields to `ChatSettings`:

```go
	WeeklyDigest bool
	LastWeekly   string // YYYY-MM-DD or empty
```

Update `AllChats` query + scan to include them:

```go
	rows, err := s.db.Query(`
		SELECT chat_id, tz, daily_reminder, daily_time, COALESCE(last_daily,''),
		       weekly_digest, COALESCE(last_weekly,'')
		FROM chats`)
```
```go
		var daily, weekly int
		if err := rows.Scan(&c.ChatID, &c.TZ, &daily, &c.DailyTime, &c.LastDaily,
			&weekly, &c.LastWeekly); err != nil {
			return nil, err
		}
		c.DailyReminder = daily != 0
		c.WeeklyDigest = weekly != 0
```

Add setters (near `SetLastDaily`/`SetDailyReminder`):

```go
// SetLastWeekly records that the weekly digest fired for a chat on a date.
func (s *Store) SetLastWeekly(chatID int64, date string) error {
	_, err := s.db.Exec(`UPDATE chats SET last_weekly=? WHERE chat_id=?`, date, chatID)
	return err
}

// SetWeeklyDigest toggles the weekly digest for a chat.
func (s *Store) SetWeeklyDigest(chatID int64, on bool) error {
	v := 0
	if on {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE chats SET weekly_digest=? WHERE chat_id=?`, v, chatID)
	return err
}
```

- [ ] **Step 6: Add week-window storage queries**

In `internal/storage/stats.go` add (reusing the `RecordRow` type from Task 1):

```go
// GamesSince counts completed non-duel games on/after a UTC timestamp.
func (s *Store) GamesSince(chatID int64, sinceUTC string) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games
		WHERE chat_id=? AND status='completed' AND deleted=0
		  AND duel_target_id IS NULL AND completed_at >= ?`, chatID, sinceUTC).Scan(&n)
	return n, err
}

// FastestSince returns the single fastest auto-recorded solve on/after a UTC
// timestamp (nil if none).
func (s *Store) FastestSince(chatID int64, sinceUTC string) (*RecordRow, error) {
	var r RecordRow
	err := s.db.QueryRow(`
		SELECT g.difficulty, gr.duration_secs, p.name
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN players p ON p.id = gr.player_id
		WHERE g.chat_id=? AND g.status='completed' AND g.deleted=0
		  AND g.duel_target_id IS NULL AND gr.duration_secs IS NOT NULL
		  AND g.completed_at >= ?
		ORDER BY gr.duration_secs ASC LIMIT 1`, chatID, sinceUTC).
		Scan(&r.Difficulty, &r.Secs, &r.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
```

(`sql` already imported in stats.go.)

- [ ] **Step 7: Implement remindWeekly + wire the tick**

In `internal/bot/reminders.go`, add `remindWeekly` to the tick loop. Change `runReminders`:

```go
	for range ticker.C {
		b.remindStalePending()
		b.remindDaily()
		b.remindWeekly()
	}
```

Add (imports `time`, `log`, `tele`, and `domain` — add `"github.com/joerude/sudoku-bot-telegram/internal/domain"` to this file):

```go
// remindWeekly posts a Monday digest at the chat's daily_time (chat tz),
// at most once per day, skipped when no games were played in the trailing week.
func (b *Bot) remindWeekly() {
	chats, err := b.st.AllChats()
	if err != nil {
		log.Printf("remindWeekly: %v", err)
		return
	}
	for _, ch := range chats {
		if !ch.WeeklyDigest {
			continue
		}
		loc := loadLoc(ch.TZ)
		now := time.Now().In(loc)
		date := now.Format("2006-01-02")
		if now.Weekday() != time.Monday || now.Format("15:04") < ch.DailyTime || ch.LastWeekly == date {
			continue
		}
		// Mark handled regardless, so we post at most once.
		if err := b.st.SetLastWeekly(ch.ChatID, date); err != nil {
			log.Printf("remindWeekly mark: %v", err)
		}
		sinceUTC := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
		weekGames, err := b.st.GamesSince(ch.ChatID, sinceUTC)
		if err != nil {
			log.Printf("remindWeekly games: %v", err)
			continue
		}
		if weekGames == 0 {
			continue // quiet week, skip the digest
		}
		season, err := b.st.ActiveSeason(ch.ChatID)
		if err != nil {
			log.Printf("remindWeekly season: %v", err)
			continue
		}
		standings, err := b.st.Standings(ch.ChatID, season.ID)
		if err != nil {
			log.Printf("remindWeekly standings: %v", err)
			continue
		}
		top := standings
		if len(top) > 3 {
			top = top[:3]
		}
		fastest, err := b.st.FastestSince(ch.ChatID, sinceUTC)
		if err != nil {
			log.Printf("remindWeekly fastest: %v", err)
		}
		streakName, streakLen := b.longestWinStreak(ch.ChatID)
		if _, err := b.tb.Send(tele.ChatID(ch.ChatID),
			digestText(season, top, fastest, streakName, streakLen, weekGames)); err != nil {
			log.Printf("remindWeekly send: %v", err)
		}
	}
}

// longestWinStreak finds the active player with the longest current win streak.
func (b *Bot) longestWinStreak(chatID int64) (name string, length int) {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		log.Printf("longestWinStreak: %v", err)
		return "", 0
	}
	for _, p := range players {
		ranks, err := b.st.RecentRanks(chatID, p.ID)
		if err != nil {
			continue
		}
		if ws := domain.WinStreak(ranks); ws > length {
			length, name = ws, p.Name
		}
	}
	return name, length
}
```

- [ ] **Step 8: Add the /settings weekly toggle**

In `internal/bot/handlers_stats.go` `onSettings`, add a case to the `switch` (alongside `target`/`points`/`minplayers`/`daily`):

```go
	case "weekly":
		val := strings.ToLower(argAt(args, 1))
		on := val == "on" || val == "вкл" || val == "1"
		off := val == "off" || val == "выкл" || val == "0"
		if !on && !off {
			return b.ephemeral(c, "Включить/выключить недельный дайджест: /settings weekly on|off")
		}
		if err := b.st.SetWeeklyDigest(c.Chat().ID, on); err != nil {
			return b.fail(c, "onSettings.weekly", err)
		}
		if on {
			return c.Send("🗓 Недельный дайджест включён (понедельник).")
		}
		return c.Send("🔕 Недельный дайджест выключен.")
```

Also add a line to `settingsUsage` (messages list in handlers_stats.go):

```
/settings weekly &lt;on|off&gt; — недельный дайджест (понедельник)
```

- [ ] **Step 9: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/storage/schema.sql internal/storage/store.go internal/storage/reminders.go internal/storage/stats.go internal/bot/messages.go internal/bot/reminders.go internal/bot/handlers_stats.go internal/bot/messages_test.go
git commit -m "feat(digest): weekly Monday digest with toggle"
```

---

## Final verification

- [ ] **Full check**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: all PASS.

- [ ] **Migration sanity (prod DB is created from older schema)**

The two new `chats` columns are added idempotently in `migrate()`; on an existing DB the `ALTER` runs once and "duplicate column" is ignored on subsequent boots. Confirm `migrate()`'s existing dup-column handling covers these (same code path as the other ALTERs).

- [ ] **Deploy + smoke**

```bash
docker compose up -d --build --force-recreate
docker compose logs bot | tail
```
Expected: `bot started`. Then: `/stats` → `🏆 Рекорды` tab renders; `/me` shows streak/badges for an active player; `/settings weekly off` then `on` both confirm.

## Self-review notes (coverage)

- Spec Slice A → Task 1; Slice B → Task 2; Slice C → Task 3; Slice D → Task 4.
- All queries carry the `completed/deleted/duel_target_id` filter per Global Constraints; records + fastest also require `duration_secs IS NOT NULL`.
- `CareerStats` `SUM` NULL-safety handled (nullable scan).
- Digest once-per-week guard mirrors the proven `last_daily` pattern; `weekly_digest` defaults on.
- `meExtra` is best-effort (returns "" on error) so a storage failure never breaks the core `/me`.
- No scoring/season/picker/auto-record/duel changes.
