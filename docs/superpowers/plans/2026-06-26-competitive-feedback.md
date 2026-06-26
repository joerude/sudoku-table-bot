# Competitive Feedback Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add podium overtake pings, a personal head-to-head `/stats` tab, and push achievement announcements to the Sudoku League bot.

**Architecture:** Pure logic in `internal/domain` (kept a leaf package), SQL aggregates in `internal/storage`, render builders in `internal/bot/messages.go`, wired into the existing `/stats` tab router and the `scoreAndCheck`/`finalize`/`autoRecord` finalize paths. One new table via `schema.sql` (no migrate ALTER).

**Tech Stack:** Go 1.25, telebot.v3 (HTML), modernc sqlite. No host `go` — all build/test in docker.

## Global Constraints

- Host has NO `go`. Build/vet/test only in docker:
  - Focused: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/<pkg>/ -run <TestName> -v'`
  - Full: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
- Conventional Commits. NO `Co-Authored-By` trailer.
- HTML parse mode: all player names through `esc()`.
- `domain` is a LEAF package — it must NOT import `internal/storage` (storage already imports domain; reverse = import cycle). Overtake's pure logic uses `domain.RankEntry`, not `storage.Standing`.
- Season queries exclude duels + non-final games: `g.status='completed' AND g.deleted=0 AND g.duel_target_id IS NULL`. H2H additionally requires both players `rank>0`.
- Do NOT change scoring math (`domain/scoring.go`, `FinalizeGame`), season rollover, the picker, auto-record matching, or duel logic.
- Reuse existing helpers: `fmtDuration`, `medal`, `esc`, `loadLoc`, `parseDBTime`, `streakBadgeText`, `domain.Badges/WinStreak/DayStreak/BadgeInput`, `b.st.RecentRanks/PlayedTimes/CareerStats/SeasonsWon`, `b.chatTZ`.
- Repo convention: pure functions (domain + render builders) get unit tests; SQL queries + Telegram/finalize glue are verified by manual smoke.

## File structure

- `internal/storage/stats.go` — `HeadToHeadAll` + `H2HRow`.
- `internal/storage/achievements.go` *(new)* — `EarnedBadges`, `AddBadge`.
- `internal/storage/schema.sql` — `achievements` table.
- `internal/domain/streaks.go` — `RankEntry`, `PodiumChanges`.
- `internal/bot/messages.go` — `h2hText`, `overtakeText`, `badgeLabel`/`badgeLabels`.
- `internal/bot/keyboards.go` — `h2h` tab in `statsTabs`.
- `internal/bot/handlers_stats.go` — `h2h` case + `h2hTab`; `badgeInput` helper + `meExtra` refactor.
- `internal/bot/handlers_game.go` — `scoreAndCheck` overtake; `toRankEntries`; `syncBadges`, `announceBadges`; call in `finalize`.
- `internal/bot/handlers_auto.go` — call `announceBadges` after the auto result send.
- `internal/domain/streaks_test.go`, `internal/bot/messages_test.go` — tests.

---

## Task 1: Personal H2H tab (Slice A)

**Files:**
- Modify: `internal/storage/stats.go`, `internal/bot/messages.go`, `internal/bot/keyboards.go`, `internal/bot/handlers_stats.go`
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `storage.H2HRow{Name string; Wins int; Losses int}`, `b.st.HeadToHeadAll(chatID, seasonID, playerID int64) ([]storage.H2HRow, error)`, `h2hText(name string, rows []storage.H2HRow) string`, `h2h` tab.
- Consumes: `esc`, `statsKeyboard`, `b.st.PlayerByTg`.

- [ ] **Step 1: Write the failing render test**

Add to `internal/bot/messages_test.go`:

```go
func TestH2HText(t *testing.T) {
	rows := []storage.H2HRow{
		{Name: "Ann", Wins: 7, Losses: 3},
		{Name: "Max<b>", Wins: 5, Losses: 5},
	}
	out := h2hText("Joe", rows)
	for _, want := range []string{"Joe", "Ann", "7", "3", "Max&lt;b&gt;", "5"} {
		if !strings.Contains(out, want) {
			t.Errorf("h2hText: want %q in\n%s", want, out)
		}
	}
	if empty := h2hText("Joe", nil); !strings.Contains(empty, "Пока нет") {
		t.Errorf("empty h2hText should hint, got: %s", empty)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestH2HText -v'`
Expected: FAIL — `undefined: h2hText`, `undefined: storage.H2HRow`.

- [ ] **Step 3: Add the storage type + query**

In `internal/storage/stats.go` add:

```go
// H2HRow is a player's season head-to-head record against one opponent.
type H2HRow struct {
	Name   string
	Wins   int // games where the subject finished ahead of this opponent
	Losses int // games where the subject finished behind
}

// HeadToHeadAll returns the subject player's season record against every other
// active player they shared a completed non-duel game with. "Ahead" means a
// strictly better rank; DNFs (rank 0) are excluded on both sides.
func (s *Store) HeadToHeadAll(chatID, seasonID, playerID int64) ([]H2HRow, error) {
	rows, err := s.db.Query(`
		SELECT oy.name,
		       SUM(CASE WHEN gx.rank < gy.rank THEN 1 ELSE 0 END) AS wins,
		       SUM(CASE WHEN gx.rank > gy.rank THEN 1 ELSE 0 END) AS losses
		FROM game_results gx
		JOIN game_results gy ON gy.game_id = gx.game_id AND gy.player_id <> gx.player_id
		JOIN games g ON g.id = gx.game_id
		JOIN players oy ON oy.id = gy.player_id
		WHERE g.chat_id = ? AND g.season_id = ?
		  AND g.status = 'completed' AND g.deleted = 0 AND g.duel_target_id IS NULL
		  AND gx.player_id = ? AND gx.rank > 0 AND gy.rank > 0 AND oy.active = 1
		GROUP BY gy.player_id, oy.name
		HAVING wins + losses > 0
		ORDER BY oy.name COLLATE NOCASE`, chatID, seasonID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []H2HRow
	for rows.Next() {
		var r H2HRow
		if err := rows.Scan(&r.Name, &r.Wins, &r.Losses); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Add the render builder**

In `internal/bot/messages.go` add:

```go
// h2hText renders the subject's season head-to-head record vs each opponent.
func h2hText(name string, rows []storage.H2HRow) string {
	if len(rows) == 0 {
		return "⚔️ <b>Очные</b>\nПока нет общих игр в этом сезоне."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ <b>Очные</b> · %s (сезон)\n", esc(name))
	for _, r := range rows {
		fmt.Fprintf(&b, "vs <b>%s</b> — %d–%d\n", esc(r.Name), r.Wins, r.Losses)
	}
	return b.String()
}
```

- [ ] **Step 5: Run test, verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestH2HText -v'`
Expected: PASS.

- [ ] **Step 6: Wire the tab**

In `internal/bot/keyboards.go`, add to `statsTabs` after the `records` entry:

```go
	{"h2h", "⚔️ Очные"},
```

In `internal/bot/handlers_stats.go` `statsView`, add a case before `default:`:

```go
	case "h2h":
		text, err = b.h2hTab(c, season)
```

(uses the outer `err`, caught by the existing post-switch `if err != nil` — same pattern as the `me` case.)

Then add the helper:

```go
// h2hTab renders the caller's head-to-head records, or a join hint.
func (b *Bot) h2hTab(c tele.Context, season *storage.Season) (string, error) {
	if c.Sender() == nil {
		return "Не удалось определить пользователя.", nil
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, c.Sender().ID)
	if err != nil {
		return "", err
	}
	if player == nil {
		return "Ты ещё не в игре. Жми /join", nil
	}
	rows, err := b.st.HeadToHeadAll(c.Chat().ID, season.ID, player.ID)
	if err != nil {
		return "", err
	}
	return h2hText(player.Name, rows), nil
}
```

- [ ] **Step 7: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS (note: the `statsTabs` length test from earlier work asserts the count — if a `TestStatsKeyboard*` test hard-codes the tab count, update it from 6 to 7; that is an expected necessary change, not scope creep).

- [ ] **Step 8: Commit**

```bash
git add internal/storage/stats.go internal/bot/messages.go internal/bot/keyboards.go internal/bot/handlers_stats.go internal/bot/messages_test.go
git commit -m "feat(stats): add personal head-to-head tab"
```

---

## Task 2: Podium overtake ping (Slice B)

**Files:**
- Modify: `internal/domain/streaks.go`, `internal/bot/messages.go`, `internal/bot/handlers_game.go`
- Test: `internal/domain/streaks_test.go`, `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `domain.RankEntry{ID int64; Name string; Points int}`, `domain.PodiumChanges(prev, cur []RankEntry) (newLeader string, enteredTop3 []string)`, `overtakeText(newLeader string, entered []string) string`, `toRankEntries([]storage.Standing) []domain.RankEntry`.
- Consumes: `scoreAndCheck`'s existing `Standings` calls.

- [ ] **Step 1: Write the failing domain test**

Add to `internal/domain/streaks_test.go`:

```go
func TestPodiumChanges(t *testing.T) {
	A := RankEntry{ID: 1, Name: "Ann", Points: 10}
	B := RankEntry{ID: 2, Name: "Bob", Points: 8}
	C := RankEntry{ID: 3, Name: "Cy", Points: 6}
	D := RankEntry{ID: 4, Name: "Di", Points: 4}

	// Bob overtakes Ann for the lead.
	prev := []RankEntry{A, B, C, D}
	cur := []RankEntry{{2, "Bob", 12}, A, C, D}
	leader, entered := PodiumChanges(prev, cur)
	if leader != "Bob" {
		t.Errorf("new leader: got %q want Bob", leader)
	}
	if len(entered) != 0 {
		t.Errorf("no new top-3 entrant expected, got %v", entered)
	}

	// Di climbs from 4th into the top 3 (no leader change).
	cur2 := []RankEntry{A, B, {4, "Di", 7}, C}
	leader2, entered2 := PodiumChanges(prev, cur2)
	if leader2 != "" {
		t.Errorf("no leader change expected, got %q", leader2)
	}
	if len(entered2) != 1 || entered2[0] != "Di" {
		t.Errorf("entered top-3: got %v want [Di]", entered2)
	}

	// No movement → nothing.
	if l, e := PodiumChanges(prev, prev); l != "" || len(e) != 0 {
		t.Errorf("stable standings should yield nothing, got %q %v", l, e)
	}

	// Zero-point standings (start of season) → no leader, no entrants.
	zero := []RankEntry{{1, "Ann", 0}, {2, "Bob", 0}}
	if l, e := PodiumChanges(zero, zero); l != "" || len(e) != 0 {
		t.Errorf("all-zero should yield nothing, got %q %v", l, e)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/domain/ -run TestPodiumChanges -v'`
Expected: FAIL — `undefined: RankEntry`, `undefined: PodiumChanges`.

- [ ] **Step 3: Implement the pure logic**

In `internal/domain/streaks.go` add:

```go
// RankEntry is one player's standing position (index 0 = top), points-ordered.
type RankEntry struct {
	ID     int64
	Name   string
	Points int
}

// PodiumChanges compares two ranked standings and reports a new season leader
// (when the top scorer with points changed) and players who newly entered the
// top 3 with points. The new leader is not repeated in enteredTop3.
func PodiumChanges(prev, cur []RankEntry) (newLeader string, enteredTop3 []string) {
	leader := func(s []RankEntry) (int64, string) {
		if len(s) > 0 && s[0].Points > 0 {
			return s[0].ID, s[0].Name
		}
		return 0, ""
	}
	top3 := func(s []RankEntry) map[int64]bool {
		m := make(map[int64]bool)
		for i := 0; i < len(s) && i < 3; i++ {
			if s[i].Points > 0 {
				m[s[i].ID] = true
			}
		}
		return m
	}
	prevID, _ := leader(prev)
	curID, curName := leader(cur)
	if curID != 0 && curID != prevID {
		newLeader = curName
	}
	prevTop := top3(prev)
	for i := 0; i < len(cur) && i < 3; i++ {
		e := cur[i]
		if e.Points > 0 && !prevTop[e.ID] && e.Name != newLeader {
			enteredTop3 = append(enteredTop3, e.Name)
		}
	}
	return newLeader, enteredTop3
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/domain/ -run TestPodiumChanges -v'`
Expected: PASS.

- [ ] **Step 5: Add the render builder + a test**

In `internal/bot/messages.go` add:

```go
// overtakeText renders podium movement after a finalized game; "" when none.
func overtakeText(newLeader string, entered []string) string {
	var parts []string
	if newLeader != "" {
		parts = append(parts, "👑 <b>"+esc(newLeader)+"</b> — новый лидер сезона!")
	}
	if len(entered) > 0 {
		names := make([]string, len(entered))
		for i, n := range entered {
			names[i] = esc(n)
		}
		parts = append(parts, "🔺 В топ-3: <b>"+strings.Join(names, ", ")+"</b>")
	}
	return strings.Join(parts, "\n")
}
```

Add to `internal/bot/messages_test.go`:

```go
func TestOvertakeText(t *testing.T) {
	if s := overtakeText("", nil); s != "" {
		t.Errorf("nothing → empty, got %q", s)
	}
	out := overtakeText("Joe<b>", []string{"Ann"})
	for _, want := range []string{"новый лидер", "Joe&lt;b&gt;", "В топ-3", "Ann"} {
		if !strings.Contains(out, want) {
			t.Errorf("overtakeText: want %q in %q", want, out)
		}
	}
}
```

- [ ] **Step 6: Run both render/domain tests, verify pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestOvertakeText -v && go test ./internal/domain/ -run TestPodiumChanges -v'`
Expected: PASS.

- [ ] **Step 7: Wire into scoreAndCheck**

In `internal/bot/handlers_game.go`, ensure the `domain` package is imported (`"github.com/joerude/sudoku-bot-telegram/internal/domain"`). Add the converter:

```go
// toRankEntries maps ranked standings to the domain's pure RankEntry slice.
func toRankEntries(s []storage.Standing) []domain.RankEntry {
	out := make([]domain.RankEntry, len(s))
	for i, st := range s {
		out[i] = domain.RankEntry{ID: st.PlayerID, Name: st.Name, Points: st.Points}
	}
	return out
}
```

In `scoreAndCheck`, capture standings BEFORE finalizing. Immediately after `season, err := b.st.SeasonByID(game.SeasonID)` (and its error check), add:

```go
	prevStandings, err := b.st.Standings(game.ChatID, season.ID)
	if err != nil {
		return "", "", err
	}
```

Then, just before the final `return result, seasonEnd, nil`, append the overtake line for non-season-ending games (the `standings` variable already holds the post-finalize order):

```go
	if seasonEnd == "" {
		newLeader, entered := domain.PodiumChanges(toRankEntries(prevStandings), toRankEntries(standings))
		if ot := overtakeText(newLeader, entered); ot != "" {
			result += "\n\n" + ot
		}
	}
```

Note: the duel branch returns earlier (before `standings` is computed), so duels are unaffected. Do not modify the duel branch or `FinalizeGame`.

- [ ] **Step 8: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/domain/streaks.go internal/domain/streaks_test.go internal/bot/messages.go internal/bot/handlers_game.go internal/bot/messages_test.go
git commit -m "feat(stats): podium overtake ping on finalize"
```

---

## Task 3: Push achievements (Slice C)

**Files:**
- Modify: `internal/storage/schema.sql`
- Create: `internal/storage/achievements.go`
- Modify: `internal/bot/messages.go`, `internal/bot/handlers_stats.go`, `internal/bot/handlers_game.go`, `internal/bot/handlers_auto.go`
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `achievements` table; `b.st.EarnedBadges(chatID, playerID int64) ([]string, error)`, `b.st.AddBadge(chatID, playerID int64, badge string) error`; `badgeLabel(emoji string) string`; `b.badgeInput(chatID, playerID int64, tz string) (domain.BadgeInput, error)`; `b.syncBadges(chatID, playerID int64, tz string) ([]string, error)`; `b.announceBadges(game *storage.Game)`.
- Consumes: `domain.Badges`, recognition-layer storage (`RecentRanks/PlayedTimes/CareerStats/SeasonsWon`), `b.chatTZ`, `b.st.GameResults`.

- [ ] **Step 1: Write the failing label test**

Add to `internal/bot/messages_test.go`:

```go
func TestBadgeLabel(t *testing.T) {
	cases := map[string]string{"🔥": "Серия побед", "⚡": "Молния", "💯": "50 побед", "📅": "Неделя"}
	for emoji, want := range cases {
		if got := badgeLabel(emoji); !strings.Contains(got, want) {
			t.Errorf("badgeLabel(%q)=%q want substring %q", emoji, got, want)
		}
	}
	if badgeLabel("❓") != "" {
		t.Errorf("unknown emoji should map to empty")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestBadgeLabel -v'`
Expected: FAIL — `undefined: badgeLabel`.

- [ ] **Step 3: Add badgeLabel**

In `internal/bot/messages.go` add (emojis MUST match `domain.Badges` exactly):

```go
// badgeLabels maps each badge emoji (from domain.Badges) to a human name.
var badgeLabels = map[string]string{
	"🏅": "Чемпион сезона",
	"🔥": "Серия побед (3+)",
	"⚡": "Молния (sub-2:00)",
	"💪": "10 побед",
	"💯": "50 побед",
	"🎯": "100 игр",
	"📅": "Неделя подряд",
}

func badgeLabel(emoji string) string {
	return badgeLabels[emoji]
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestBadgeLabel -v'`
Expected: PASS.

- [ ] **Step 5: Add the table + storage**

In `internal/storage/schema.sql`, add at the end:

```sql
CREATE TABLE IF NOT EXISTS achievements (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id    INTEGER NOT NULL,
    player_id  INTEGER NOT NULL,
    badge      TEXT    NOT NULL,
    earned_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(chat_id, player_id, badge)
);
```

Create `internal/storage/achievements.go`:

```go
package storage

// EarnedBadges returns the badge emojis a player has already been awarded.
func (s *Store) EarnedBadges(chatID, playerID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT badge FROM achievements WHERE chat_id=? AND player_id=?`, chatID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AddBadge records a newly earned badge; a re-award is a no-op (unique index).
func (s *Store) AddBadge(chatID, playerID int64, badge string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO achievements(chat_id, player_id, badge) VALUES(?,?,?)`,
		chatID, playerID, badge)
	return err
}
```

- [ ] **Step 6: Extract badgeInput (DRY) + refactor meExtra**

In `internal/bot/handlers_stats.go`, add `badgeInput` and rewrite `meExtra` to use it (behavior identical — same streak lines + badges):

```go
// badgeInput assembles a player's cross-season badge inputs (best-effort caller
// handles errors). tz buckets play-days for the day streak.
func (b *Bot) badgeInput(chatID, playerID int64, tz string) (domain.BadgeInput, error) {
	ranks, err := b.st.RecentRanks(chatID, playerID)
	if err != nil {
		return domain.BadgeInput{}, err
	}
	times, err := b.st.PlayedTimes(chatID, playerID)
	if err != nil {
		return domain.BadgeInput{}, err
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
		return domain.BadgeInput{}, err
	}
	seasonsWon, err := b.st.SeasonsWon(chatID, playerID)
	if err != nil {
		return domain.BadgeInput{}, err
	}
	return domain.BadgeInput{
		Wins: wins, Games: games, BestSecs: best,
		WinStreak: domain.WinStreak(ranks), DayStreak: domain.DayStreak(dates, today),
		SeasonsWon: seasonsWon,
	}, nil
}

func (b *Bot) meExtra(chatID, playerID int64, tz string) string {
	in, err := b.badgeInput(chatID, playerID, tz)
	if err != nil {
		log.Printf("meExtra: %v", err)
		return ""
	}
	return streakBadgeText(in.WinStreak, in.DayStreak, domain.Badges(in))
}
```

Delete the OLD body of `meExtra` (the one that inlined RecentRanks/PlayedTimes/CareerStats/SeasonsWon) — it is fully replaced by the two functions above. Confirm `domain`, `time`, `log` are imported in handlers_stats.go.

- [ ] **Step 7: Add syncBadges + announceBadges and wire finalize paths**

In `internal/bot/handlers_game.go` add:

```go
// syncBadges recomputes a player's badges, persists any newly earned, and
// returns the newly earned emojis (best-effort).
func (b *Bot) syncBadges(chatID, playerID int64, tz string) ([]string, error) {
	in, err := b.badgeInput(chatID, playerID, tz)
	if err != nil {
		return nil, err
	}
	current := domain.Badges(in)
	earned, err := b.st.EarnedBadges(chatID, playerID)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(earned))
	for _, e := range earned {
		have[e] = true
	}
	var fresh []string
	for _, e := range current {
		if !have[e] {
			if err := b.st.AddBadge(chatID, playerID, e); err != nil {
				log.Printf("syncBadges add: %v", err)
				continue
			}
			fresh = append(fresh, e)
		}
	}
	return fresh, nil
}

// announceBadges posts a message for badges newly earned by the game's
// participants. Best-effort: errors are logged, never block the result.
func (b *Bot) announceBadges(game *storage.Game) {
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("announceBadges results: %v", err)
		return
	}
	tz := b.chatTZ(game.ChatID)
	var lines []string
	for _, r := range rows {
		fresh, err := b.syncBadges(game.ChatID, r.PlayerID, tz)
		if err != nil {
			log.Printf("announceBadges sync: %v", err)
			continue
		}
		for _, e := range fresh {
			lines = append(lines, fmt.Sprintf("🏅 <b>%s</b> открыл бейдж: %s %s",
				esc(r.Name), e, badgeLabel(e)))
		}
	}
	if len(lines) > 0 {
		_, _ = b.tb.Send(tele.ChatID(game.ChatID), strings.Join(lines, "\n"))
	}
}
```

Confirm `fmt`, `strings`, `log`, `tele` are imported in handlers_game.go (they are).

In `finalize` (same file), after the result is edited in and the season-end message handled, call the announce. Change the tail of `finalize` so that after `c.Edit(result, ...)` and the `seasonEnd` send, it ends with:

```go
	b.announceBadges(game)
	if seasonEnd != "" {
		return c.Send(seasonEnd)
	}
	return nil
```

(Keep the existing `c.Edit` of the result above this; just insert `b.announceBadges(game)` before the `seasonEnd` return so badges post after the result regardless of season end.)

- [ ] **Step 8: Wire the auto-record path**

In `internal/bot/handlers_auto.go` `autoRecord`, after the successful result send (`b.tb.Send(to, autoResultHeader()+result, resultKeyboard(game.ID))`) and the seasonEnd send, add:

```go
	b.announceBadges(game)
```

Place it after the result send, before/after the `seasonEnd` send is fine — it posts independently. Do not alter the unknown-nick branch, anti-farming guard, or claim-button logic.

- [ ] **Step 9: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 10: Manual smoke (deferred — needs deployed bot)**

After deploy: finish a game where a participant crosses a badge threshold (e.g. 10th win) → expect a "🏅 … открыл бейдж" message once; finishing another game must NOT re-announce it (persisted). Note deferred in the report.

- [ ] **Step 11: Commit**

```bash
git add internal/storage/schema.sql internal/storage/achievements.go internal/bot/messages.go internal/bot/handlers_stats.go internal/bot/handlers_game.go internal/bot/handlers_auto.go internal/bot/messages_test.go
git commit -m "feat(achievements): announce newly earned badges on finalize"
```

---

## Final verification

- [ ] **Full check**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: all PASS.

- [ ] **Deploy + smoke**

```bash
docker compose up -d --build --force-recreate
docker compose logs bot | tail
```
Expected: `bot started`. Then: `/stats` → `⚔️ Очные` shows your H2H; finish a game that changes the podium → overtake line appears on the result; cross a badge threshold → one-time badge announce.

## Self-review notes (coverage)

- Spec Slice A → Task 1; Slice B → Task 2; Slice C → Task 3.
- `domain` stays a leaf: `PodiumChanges` uses `domain.RankEntry`; the bot maps `storage.Standing` → `RankEntry` via `toRankEntries`.
- H2H query: both-rank>0, season-scoped, duels/deleted excluded, opponent active.
- Overtake skipped on season-end and on the duel branch (returns earlier).
- Achievements via `schema.sql` (CREATE IF NOT EXISTS, no migrate); `INSERT OR IGNORE` makes re-award idempotent so no double-announce.
- `meExtra` refactor preserves its output (streak lines + badges) while sharing `badgeInput` with `syncBadges` (DRY).
- No scoring/season-rollover/picker/auto-record-matching/duel changes.
