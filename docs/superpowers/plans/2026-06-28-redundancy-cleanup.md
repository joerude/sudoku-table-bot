# Redundancy Cleanup & Message Conciseness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove real redundancy in the sudoku bot — a single source of truth for the
"which games count" SQL predicate, fewer/leaner user messages — without changing any
scoring behavior or removing useful features.

**Architecture:** Three independent workstreams, each its own commit on branch
`cleanup/redundancy-and-messages`: (3) storage SQL predicate constants, (2) message
dedup + trim, (1) drop one dead command alias. Storage is behavior-preserving and
fully guarded by the existing test suite; do it first.

**Tech Stack:** Go 1.25, `gopkg.in/telebot.v3`, `modernc.org/sqlite` (no CGO).
Design doc: `docs/superpowers/specs/2026-06-28-redundancy-cleanup-design.md`.

## Global Constraints

- **No `go` on the host.** Every build/vet/test runs in docker:
  ```
  docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c '<go command>'
  ```
  First run pulls the image and downloads modules (slow, several minutes — use a
  generous timeout, e.g. 600000ms). Later runs are faster.
- **Commit style:** Conventional Commits, **no `Co-Authored-By` trailer** (repo style).
- **Behavior must not change** except: (a) auto-record posts one message instead of
  two; (b) duel challenge warning now lists names; (c) `newGameText`/`welcomeText` are
  shorter; (d) `/table` no longer routed. Nothing else — scoring, filters, standings
  stay identical.
- **SQL literals use single quotes** (SQLite reads double quotes as identifiers).
- All work on branch `cleanup/redundancy-and-messages` (already created).
- Run the full gate `go build ./... && go vet ./... && go test ./...` in docker before
  every commit; expect all green.

---

## Task S1: Storage predicate constants + `stats.go`

**Files:**
- Create: `internal/storage/predicates.go`
- Modify: `internal/storage/stats.go` (queries in `SpeedFor`, `Speedboard`,
  `ExportGames`, `RecentGames`, `RecordsBoard`, `RecentRanks`, `PlayedTimes`,
  `CareerStats`, `GamesSince`, `FastestSince`)
- Test: existing `internal/storage/stats_test.go`, `duels_test.go` (no new test here)

**Interfaces:**
- Produces: package-level consts `sqlCompletedGames`, `sqlSeasonalGames`,
  `sqlDuelGames` (string), used by S2 and S3. All assume the `games` table is aliased
  `g` in the query.

This is a **behavior-preserving refactor** guarded by `TestSeasonExcludesDuels`,
`TestSpeedForExcludesManualRows/DNF/DifficultyFilter/SeasonFilter`,
`TestSpeedboardOrderingAndThreshold`, `TestSpeedboardIgnoresDeletedGames`. The test is
"the existing suite stays green before and after."

- [ ] **Step 1: Confirm green baseline**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/storage/...'
```
Expected: `ok  github.com/joerude/sudoku-bot-telegram/internal/storage`

- [ ] **Step 2: Create the constants file**

Create `internal/storage/predicates.go`:
```go
package storage

// SQL predicate fragments for the games table aliased as `g`. Centralised so the
// "which games count" rules live in one place — forgetting a clause silently
// corrupts scores. Every stats query selecting games MUST use one of these.
const (
	// sqlCompletedGames: a game that actually happened and was not removed.
	sqlCompletedGames = "g.status = 'completed' AND g.deleted = 0"
	// sqlSeasonalGames: a completed game that counts toward the season (not a duel).
	sqlSeasonalGames = sqlCompletedGames + " AND g.duel_target_id IS NULL"
	// sqlDuelGames: a completed duel game.
	sqlDuelGames = sqlCompletedGames + " AND g.duel_target_id IS NOT NULL"
)
```

- [ ] **Step 3: Rewrite `stats.go` queries to use `sqlSeasonalGames`**

In `internal/storage/stats.go`, replace the WHERE/JOIN predicate fragments. Each
query keeps every other clause identical; only the
`status/deleted/duel_target_id` group becomes the constant. The constant is spliced
with `` `+sqlSeasonalGames+` `` (break the backtick string, concatenate, resume).

`SpeedFor` — the query string becomes:
```go
	err := s.db.QueryRow(`
		SELECT AVG(gr.duration_secs), MIN(gr.duration_secs), COUNT(gr.duration_secs)
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		WHERE gr.player_id = ? AND g.chat_id = ? AND g.season_id = ?
		  AND `+sqlSeasonalGames+`
		  AND g.difficulty = ? AND gr.duration_secs IS NOT NULL`,
		playerID, chatID, seasonID, difficulty).Scan(&avg, &best, &n)
```

`Speedboard` — WHERE becomes:
```go
		WHERE p.chat_id = ? AND p.active = 1
		  AND g.season_id = ? AND `+sqlSeasonalGames+`
		  AND g.difficulty = ? AND gr.duration_secs IS NOT NULL
		GROUP BY p.id, p.name
		ORDER BY avg_secs ASC, games DESC, p.name COLLATE NOCASE`,
```

`ExportGames` — WHERE becomes:
```go
		WHERE g.chat_id=? AND g.season_id=? AND `+sqlSeasonalGames+`
		ORDER BY g.id`, chatID, seasonID)
```

`RecentGames` — alias the table `g` and use the constant:
```go
	rows, err := s.db.Query(`
		SELECT g.id, date(g.completed_at), COALESCE(g.difficulty,''), COALESCE(g.usdoku_code,'')
		FROM games g
		WHERE g.chat_id=? AND `+sqlSeasonalGames+`
		ORDER BY g.id DESC LIMIT ?`, chatID, n)
```

`RecordsBoard` — WHERE becomes:
```go
		WHERE g.chat_id = ? AND `+sqlSeasonalGames+`
		  AND gr.duration_secs IS NOT NULL
		  AND g.difficulty IS NOT NULL AND g.difficulty <> ''
		GROUP BY g.difficulty`, chatID)
```

`RecentRanks` — WHERE becomes:
```go
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND `+sqlSeasonalGames+`
		ORDER BY g.completed_at DESC, g.id DESC`, chatID, playerID)
```

`PlayedTimes` — WHERE becomes:
```go
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND `+sqlSeasonalGames+`
		  AND g.completed_at IS NOT NULL
		ORDER BY g.completed_at DESC`, chatID, playerID)
```

`CareerStats` — WHERE becomes (the constant is the final clause, so the raw string
ends at `AND ` and the args follow the concatenation):
```go
		WHERE g.chat_id = ? AND gr.player_id = ?
		  AND `+sqlSeasonalGames,
		chatID, playerID).Scan(&winsN, &games, &best)
```

`GamesSince` — alias the table `g`:
```go
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games g
		WHERE g.chat_id=? AND `+sqlSeasonalGames+`
		  AND g.completed_at >= ?`, chatID, sinceUTC).Scan(&n)
```

`FastestSince` — WHERE becomes:
```go
		WHERE g.chat_id=? AND `+sqlSeasonalGames+`
		  AND gr.duration_secs IS NOT NULL
		  AND g.completed_at >= ?
		ORDER BY gr.duration_secs ASC LIMIT 1`, chatID, sinceUTC).
		Scan(&r.Difficulty, &r.Secs, &r.Name)
```

- [ ] **Step 4: Run storage tests — expect still green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./... && go test ./internal/storage/...'
```
Expected: PASS (same tests as Step 1). If any fail, a predicate was altered — diff the
query against the original and fix.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/predicates.go internal/storage/stats.go
git commit -m "refactor(storage): extract game predicate constants, apply in stats"
```

---

## Task S2: Apply predicates in `standings.go` + `duels.go`

**Files:**
- Modify: `internal/storage/standings.go` (`Standings`)
- Modify: `internal/storage/duels.go` (`DuelRecord`, `DuelLeaderboard`, `RecentDuels`,
  `HeadToHead`)
- Test: existing `standings`/`duels` tests

**Interfaces:**
- Consumes: `sqlSeasonalGames`, `sqlDuelGames` from S1.

Behavior-preserving; guarded by `TestGameFlowAndStandings`, `TestSeasonExcludesDuels`,
`TestDuelRecord`, `TestDuelLeaderboard`, `TestRecentDuels`, `TestDuelDNFCountsAsLoss`,
`TestDuelNormalLossStillWorks`, `TestHeadToHead`.

- [ ] **Step 1: Rewrite `Standings` (seasonal)**

In `internal/storage/standings.go`, the `LEFT JOIN games g` ON-clause becomes:
```go
		LEFT JOIN games g
		       ON g.season_id = ? AND `+sqlSeasonalGames+` AND g.chat_id = p.chat_id
		LEFT JOIN game_results gr
		       ON gr.game_id = g.id AND gr.player_id = p.id
		WHERE p.chat_id = ? AND p.active = 1
		GROUP BY p.id, p.name
		ORDER BY pts DESC, wins DESC, p.name COLLATE NOCASE`, seasonID, chatID)
```

- [ ] **Step 2: Rewrite `duels.go` queries (duel)**

`DuelRecord` WHERE becomes (constant is the final clause):
```go
		WHERE gr.player_id=? AND g.chat_id=? AND `+sqlDuelGames,
		playerID, chatID).Scan(&wins, &losses)
```

`DuelLeaderboard` WHERE becomes:
```go
		WHERE p.chat_id=? AND p.active=1 AND `+sqlDuelGames+`
		GROUP BY p.id, p.name
		HAVING (wins + losses) > 0
		ORDER BY wins DESC, (CAST(wins AS REAL)/(wins+losses)) DESC, p.name COLLATE NOCASE`,
		chatID)
```

`RecentDuels` WHERE becomes:
```go
		WHERE g.chat_id=? AND `+sqlDuelGames+`
		GROUP BY g.id
		ORDER BY g.id DESC
		LIMIT ?`, chatID, n)
```

`HeadToHead` WHERE becomes (keep the `gr.game_id IN (...)` subquery unchanged):
```go
		WHERE g.chat_id=? AND `+sqlDuelGames+`
		  AND gr.game_id IN (
		      SELECT game_id FROM game_results WHERE player_id=?
		      INTERSECT
		      SELECT game_id FROM game_results WHERE player_id=?
		  )`,
		aID, bID, chatID, aID, bID).Scan(&aWins, &bWins)
```

- [ ] **Step 3: Run storage tests — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./... && go test ./internal/storage/...'
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/storage/standings.go internal/storage/duels.go
git commit -m "refactor(storage): apply predicate constants in standings and duels"
```

---

## Task S3: `CompletedToday` guard test + apply `sqlCompletedGames`

**Files:**
- Create test: append `TestCompletedTodayCountsDuels` to
  `internal/storage/storage_test.go`
- Modify: `internal/storage/reminders.go` (`CompletedToday`)

**Interfaces:**
- Consumes: `sqlCompletedGames` from S1, helpers `makeNormalGame`/`makeDuelGame` from
  `duels_test.go`, `openTemp` from the storage test suite.

`CompletedToday` intentionally counts duels ("played today" includes duels — CLAUDE.md).
It must use `sqlCompletedGames` (NO duel filter). This new test locks that so the
refactor can't silently add the seasonal filter.

- [ ] **Step 1: Write the failing/guard test**

Append to `internal/storage/storage_test.go`:
```go
func TestCompletedTodayCountsDuels(t *testing.T) {
	st := openTemp(t)
	const chat = int64(-3050)
	st.EnsureChat(chat, 1)
	se, _ := st.ActiveSeason(chat)
	a, _, _ := st.RegisterPlayer(chat, 1, "Alice")
	b, _, _ := st.RegisterPlayer(chat, 2, "Bob")

	// One normal game and one duel game, both completed "now".
	makeNormalGame(t, st, chat, se.ID, a.ID, b.ID)
	makeDuelGame(t, st, chat, se.ID, a.ID, a.ID, b.ID, b.ID)

	today := time.Now().UTC().Format("2006-01-02")
	n, err := st.CompletedToday(chat, today)
	if err != nil {
		t.Fatalf("CompletedToday: %v", err)
	}
	if n != 2 {
		t.Errorf("CompletedToday: want 2 (duel counts as played today), got %d", n)
	}
}
```
Ensure `storage_test.go` imports `"time"` (add to its import block if absent).

- [ ] **Step 2: Run the test against current code — expect PASS**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/storage/ -run TestCompletedTodayCountsDuels -v'
```
Expected: PASS (current `CompletedToday` has no duel filter — this characterizes
existing behavior before the refactor).

- [ ] **Step 3: Refactor `CompletedToday`**

In `internal/storage/reminders.go`, the query becomes (alias `g`, `sqlCompletedGames`,
no duel filter):
```go
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM games g
		WHERE g.chat_id=? AND `+sqlCompletedGames+` AND date(g.completed_at)=?`,
		chatID, date).Scan(&n)
```

- [ ] **Step 4: Run storage tests — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./... && go test ./internal/storage/...'
```
Expected: PASS, including `TestCompletedTodayCountsDuels` (still 2).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/reminders.go internal/storage/storage_test.go
git commit -m "refactor(storage): CompletedToday uses completed-games predicate (still counts duels)"
```

---

## Task M1: Merge auto-record's two posts into one

**Files:**
- Modify: `internal/bot/keyboards.go` (add `recordAndClaimKeyboard`)
- Modify: `internal/bot/handlers_auto.go:127-138` (one `Send`)
- Test: append to `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `recordAndClaimKeyboard(gameID int64, unknown []string) *tele.ReplyMarkup`
  — a markup whose first row is the "📝 Записать результат" button (`cbRec`), followed
  by one "Это я: <nick>" claim button per nick (`cbClaimNick`, payload `<gameID>:<nick>`),
  skipping any nick whose payload exceeds 64 bytes (same rule as `claimNickKeyboard`).

- [ ] **Step 1: Write the failing test**

Append to `internal/bot/messages_test.go`:
```go
func TestRecordAndClaimKeyboard(t *testing.T) {
	m := recordAndClaimKeyboard(42, []string{"joe", "max"})
	if len(m.InlineKeyboard) != 3 {
		t.Fatalf("want 3 rows (record + 2 claims), got %d", len(m.InlineKeyboard))
	}
	if u := m.InlineKeyboard[0][0].Unique; u != cbRec {
		t.Errorf("row 0 should be record button (%q), got %q", cbRec, u)
	}
	var claims []string
	for _, row := range m.InlineKeyboard[1:] {
		for _, btn := range row {
			if btn.Unique != cbClaimNick {
				t.Errorf("claim row has wrong unique %q", btn.Unique)
			}
			claims = append(claims, btn.Data)
		}
	}
	want := []string{"42:joe", "42:max"}
	if len(claims) != len(want) {
		t.Fatalf("want %v claims, got %v", want, claims)
	}
	for i := range want {
		if claims[i] != want[i] {
			t.Errorf("claim[%d]=%q want %q", i, claims[i], want[i])
		}
	}
	// No unknown nicks → just the record row.
	if only := recordAndClaimKeyboard(7, nil); len(only.InlineKeyboard) != 1 {
		t.Errorf("no nicks: want 1 row, got %d", len(only.InlineKeyboard))
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestRecordAndClaimKeyboard'
```
Expected: FAIL to compile — `undefined: recordAndClaimKeyboard`.

- [ ] **Step 3: Implement the keyboard**

Add to `internal/bot/keyboards.go` (after `recordKeyboard`):
```go
// recordAndClaimKeyboard combines the "record result" button with one claim-nick
// button per unrecognised usdoku nick, so the auto-record fallback is a single post.
// Nicks whose payload would exceed Telegram's 64-byte limit are skipped (listed in
// the text body for manual /setnick instead).
func recordAndClaimKeyboard(gameID int64, unknown []string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	rows := []tele.Row{m.Row(m.Data("📝 Записать результат", cbRec, gid(gameID)))}
	for _, n := range unknown {
		data := fmt.Sprintf("%d:%s", gameID, n)
		if len(data) > 64 {
			continue
		}
		rows = append(rows, m.Row(m.Data("Это я: "+n, cbClaimNick, data)))
	}
	m.Inline(rows...)
	return m
}
```

- [ ] **Step 4: Run test — expect PASS**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestRecordAndClaimKeyboard -v'
```
Expected: PASS.

- [ ] **Step 5: Use it in `handlers_auto.go` (one Send)**

Replace the block at `internal/bot/handlers_auto.go:127-138`:
```go
	if len(unknown) > 0 || len(picks) == 0 {
		log.Printf("🤖 game %d finished, finishers=%v unknown=%v mappedJoined=%d → manual",
			game.ID, finisherNicks, unknown, mappedJoined)
		_, _ = b.tb.Send(to, autoMappingText(finisherNicks, unknown),
			recordAndClaimKeyboard(game.ID, unknown))
		return
	}
```
(This drops the second `b.tb.Send(... "Это твой ник?" ...)` entirely; the claim
buttons now ride on the first message. `claimNickKeyboard` remains for any other
caller — leave it defined.)

- [ ] **Step 6: Run the full bot suite — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./... && go test ./internal/bot/...'
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bot/keyboards.go internal/bot/handlers_auto.go internal/bot/messages_test.go
git commit -m "feat(bot): single auto-record fallback post with record + claim buttons"
```

---

## Task M2: Shared `missingNickWarn`, used by newgame and duel

**Files:**
- Modify: `internal/bot/messages.go` (add `missingNickWarn`; change `duelChallengeText`
  signature)
- Modify: `internal/bot/handlers_game.go:140-146` (newgame uses the helper)
- Modify: `internal/bot/handlers_duel.go:85-89` (build names, pass to challenge)
- Test: update `TestDuelChallengeText`; add `TestMissingNickWarn` in
  `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `missingNickWarn(names []string) string` — returns `""` when `names` is
  empty, else `"\n\n⚠️ Без ника (авто-учёт не сработает): <b>NAMES</b> — задайте /setnick."`
  with names HTML-escaped and comma-joined.
- Changes: `duelChallengeText(challenger string, target storage.Player, difficulty,
  code string, missingNicks []string) string` (was `nickWarn bool`).

- [ ] **Step 1: Write/adjust the failing tests**

Add to `internal/bot/messages_test.go`:
```go
func TestMissingNickWarn(t *testing.T) {
	if s := missingNickWarn(nil); s != "" {
		t.Errorf("no names → empty, got %q", s)
	}
	out := missingNickWarn([]string{"Joe<b>", "Max"})
	for _, want := range []string{"setnick", "Joe&lt;b&gt;", "Max"} {
		if !strings.Contains(out, want) {
			t.Errorf("missingNickWarn: want %q in %q", want, out)
		}
	}
}
```
Update `TestDuelChallengeText` (the two `duelChallengeText(...)` calls now take a
`[]string` instead of a bool):
```go
	out := duelChallengeText("Vasya", target, "medium", "TRPK", nil)
	...
	outWarn := duelChallengeText("Vasya", target, "medium", "TRPK", []string{"Vasya"})
	if !strings.Contains(outWarn, "setnick") {
		t.Errorf("duelChallengeText with missing nicks: want 'setnick'\ngot: %s", outWarn)
	}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestMissingNickWarn|TestDuelChallengeText"'
```
Expected: FAIL to compile — `undefined: missingNickWarn` and signature mismatch.

- [ ] **Step 3: Add `missingNickWarn` and change `duelChallengeText`**

In `internal/bot/messages.go`, add near `namesMissingNick`:
```go
// missingNickWarn is the shared "no usdoku nick → auto-record will skip" warning,
// appended after a game/duel post. Returns "" when nobody is missing a nick.
func missingNickWarn(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"\n\n⚠️ Без ника (авто-учёт не сработает): <b>%s</b> — задайте /setnick.",
		esc(strings.Join(names, ", ")))
}
```
Change `duelChallengeText` signature and warning tail:
```go
func duelChallengeText(challenger string, target storage.Player, difficulty, code string, missingNicks []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ <b>%s</b> вызывает %s на дуэль! · %s\n",
		esc(challenger), mention(target), titleCase(difficulty))
	if code != "" {
		fmt.Fprintf(&b, "Комната: https://www.usdoku.com/%s\n", code)
	}
	b.WriteString("\nПринимаешь вызов?")
	b.WriteString(missingNickWarn(missingNicks))
	return b.String()
}
```

- [ ] **Step 4: Update the newgame caller**

In `internal/bot/handlers_game.go`, replace lines 140-146:
```go
	if players, perr := b.st.ListPlayers(c.Chat().ID); perr == nil {
		text += missingNickWarn(namesMissingNick(players))
	}
```

- [ ] **Step 5: Update the duel caller**

In `internal/bot/handlers_duel.go`, replace lines 85-89:
```go
	var missing []string
	if !caller.UsdokuNick.Valid || caller.UsdokuNick.String == "" {
		missing = append(missing, caller.Name)
	}
	if !target.UsdokuNick.Valid || target.UsdokuNick.String == "" {
		missing = append(missing, target.Name)
	}
	_ = c.Respond()
	return c.Edit(duelChallengeText(caller.Name, *target, difficulty, room.code, missing),
		duelKeyboard(room.gameID))
```

- [ ] **Step 6: Run the full bot suite — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./... && go test ./internal/bot/...'
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bot/messages.go internal/bot/handlers_game.go internal/bot/handlers_duel.go internal/bot/messages_test.go
git commit -m "refactor(bot): shared missingNickWarn for newgame and duel"
```

---

## Task M3: Trim `newGameText` and `welcomeText`

**Files:**
- Modify: `internal/bot/messages.go` (`newGameText`, `welcomeText`)
- Test: add `TestNewGameTextConcise` in `internal/bot/messages_test.go`

**Interfaces:** none changed (same function signatures).

Trim the step-by-step instructions; keep the link and the "record result" hint.
`newGameWithCodeText` stays separate (different state — room already created).

- [ ] **Step 1: Write the failing test**

Append to `internal/bot/messages_test.go`:
```go
func TestNewGameTextConcise(t *testing.T) {
	out := newGameText("medium", "hardcore")
	for _, want := range []string{usdokuCreateURL, "Medium", "Записать результат"} {
		if !strings.Contains(out, want) {
			t.Errorf("newGameText: want %q in\n%s", want, out)
		}
	}
	// The verbose numbered steps are gone.
	if strings.Contains(out, "1. Открой") {
		t.Errorf("newGameText should no longer carry numbered steps:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestNewGameTextConcise'
```
Expected: FAIL (current text contains "1. Открой").

- [ ] **Step 3: Trim the two messages**

In `internal/bot/messages.go`, replace `newGameText`:
```go
// newGameText is the message posted by /newgame when no usdoku room was created.
func newGameText(difficulty, mode string) string {
	return fmt.Sprintf(
		"🧩 <b>Новая игра</b> · %s · %s\n\n"+
			"Создайте комнату (%s · %s) и играйте:\n%s\n\n"+
			"Когда доиграете — жми «📝 Записать результат».",
		titleCase(difficulty), titleCase(mode),
		titleCase(difficulty), titleCase(mode), usdokuCreateURL)
}
```
Replace `welcomeText` (shorter "С чего начать"):
```go
const welcomeText = `👋 <b>Привет!</b> Я веду учёт ваших игр в судоку (usdoku.com) — очки, места, сезоны.

Каждый игрок: /join, затем /setnick &lt;ник на usdoku&gt; (для авто-учёта).
Создать игру — /newgame medium. Все команды — /help. Кнопки ниже 👇`
```

- [ ] **Step 4: Run the full bot suite — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go vet ./... && go test ./internal/bot/...'
```
Expected: PASS (`TestNewGameTextConcise` green; no other message test references the
trimmed lines).

- [ ] **Step 5: Commit**

```bash
git add internal/bot/messages.go internal/bot/messages_test.go
git commit -m "refactor(bot): trim newGameText and welcomeText"
```

---

## Task M4: Centralize callback guard strings

**Files:**
- Modify: `internal/bot/messages.go` (add guard constants)
- Modify: `internal/bot/handlers_game.go`, `handlers_duel.go`, `handlers_invite.go`,
  `handlers_auto.go` (replace literals)

**Interfaces:**
- Produces: consts `msgNeedJoin = "Сначала /join"`, `msgWhoAreYou = "Не вижу кто ты"`,
  `msgGameNotFound = "Игра не найдена"`, `msgGameClosed = "Игра уже закрыта"`.

Pure string centralization — no behavior change. Verified by build + existing suite.
One-off guard strings ("Не понял выбор", "Не понял ник", "Не получилось определить
тебя", "Нельзя вызвать самого себя", "Игрок не найден") stay as literals — they appear
once each.

- [ ] **Step 1: Add the constants**

In `internal/bot/messages.go`, near `anonMsg`:
```go
// Common callback-guard responses (shown as ephemeral toasts via c.Respond).
const (
	msgNeedJoin     = "Сначала /join"
	msgWhoAreYou    = "Не вижу кто ты"
	msgGameNotFound = "Игра не найдена"
	msgGameClosed   = "Игра уже закрыта"
)
```

- [ ] **Step 2: Replace the matching literals**

Replace each occurrence of the literal string inside `&tele.CallbackResponse{Text: ...}`
with the constant:
- `"Сначала /join"` → `msgNeedJoin` in `handlers_duel.go:62`, `handlers_auto.go:189`,
  `handlers_invite.go:43`.
- `"Не вижу кто ты"` → `msgWhoAreYou` in `handlers_duel.go:55`, `handlers_duel.go:106`,
  `handlers_invite.go:36`.
- `"Игра не найдена"` → `msgGameNotFound` in `handlers_game.go:263`, `:285`, `:318`,
  `:340`. Also `handlers_game.go:193` (`"Игра не найдена или уже закрыта"`) → `msgGameNotFound`.
- `"Игра уже закрыта"` → `msgGameClosed` in `handlers_game.go:212`, `:242`, `:482`,
  `handlers_invite.go:32`.

Leave `handlers_game.go:391` (`return "🗑 Игра не найдена.", nil, nil`) — different
shape/return type; not a callback toast. Skip it.

- [ ] **Step 3: Build, vet, full suite — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
```
Expected: PASS, no unused-constant errors.

- [ ] **Step 4: Commit**

```bash
git add internal/bot/messages.go internal/bot/handlers_game.go internal/bot/handlers_duel.go internal/bot/handlers_invite.go internal/bot/handlers_auto.go
git commit -m "refactor(bot): centralize callback guard strings"
```

---

## Task C1: Remove the dead `/table` alias

**Files:**
- Modify: `internal/bot/bot.go:132`

**Interfaces:** none.

`/table` duplicates `/status` and is not in the command menu (`SetCommands`), so no test
references it.

- [ ] **Step 1: Delete the route**

In `internal/bot/bot.go`, remove line 132:
```go
	b.tb.Handle("/table", b.onStatus)
```

- [ ] **Step 2: Build, vet, full suite — expect green**

Run:
```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
```
Expected: PASS (`TestBotCommandsAreTheSixEveryday` unaffected — `/table` was never in
the menu).

- [ ] **Step 3: Commit**

```bash
git add internal/bot/bot.go
git commit -m "refactor(bot): drop redundant /table alias (use /status)"
```

---

## Final verification

- [ ] **Run the full gate once more:**

```
docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'
```
Expected: all packages PASS.

- [ ] **Confirm the acceptance criteria from the spec:**
  - `TestSeasonExcludesDuels` green (seasonal queries still exclude duels).
  - `TestCompletedTodayCountsDuels` green (CompletedToday still counts duels).
  - `TestRecordAndClaimKeyboard`, `TestMissingNickWarn`, `TestNewGameTextConcise` green.
  - `grep -rn '/table' internal/bot/bot.go` returns nothing.
  - `grep -rn "duel_target_id IS" internal/storage/*.go` shows the predicate only in
    `predicates.go` (plus `games.go` SetDuelTarget/DuelTargetID, which are writes/reads
    of the column itself, not stat filters).
