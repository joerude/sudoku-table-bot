# UX Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut the Sudoku League bot's everyday command surface from ~18 listed commands to 6, behind two consolidating commands (`/play`, `/stats`), and kill the `/setnick` silent-fail — without touching scoring, seasons, or the DB schema.

**Architecture:** Pure additive UI layer. New callback-driven menus (`/play` hub, `/stats` tabbed dashboard) reuse the existing `*Text()` render builders and the shared `createGameRoom` core. Old commands stay registered as hidden aliases (handlers unchanged) and are simply dropped from the Telegram `/` menu. Onboarding fixes are contextual (game-time), since usdoku exposes no nick-lookup API.

**Tech Stack:** Go 1.25, `gopkg.in/telebot.v3` (HTML parse mode, inline keyboards), `modernc.org/sqlite`. No host `go` toolchain — all build/test runs in docker.

## Global Constraints

- Host has **no `go`**. Every build/vet/test runs in docker:
  `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c '<go cmd>'`
- Single-test run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run <TestName> -v'`
- Full check: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
- **Conventional Commits**, **no `Co-Authored-By` trailer**.
- Telegram HTML parse mode: all user-supplied text through `esc()`.
- Inline-button transformations use `c.Edit`; new posts use `c.Send`; callbacks must call `c.Respond()`.
- Do NOT modify: `internal/domain/scoring.go`, season logic, `internal/storage/schema.sql`, the finish-order picker, the auto-record scoring path.
- Existing repo convention: pure render/keyboard/SQL functions get unit tests (`internal/bot/messages_test.go`); Telegram I/O glue is verified by manual smoke, not unit tests.

## Build order rationale

Spec slices are A (menu trim), B (/stats), C (/play), D (onboarding). Implemented as **B → C → A → D**: the menu trim (A) lists `/play` and `/stats`, so their handlers must exist first or the `/` menu would show dead commands.

## File structure

- `internal/bot/keyboards.go` — add callback ids + `statsKeyboard`, `playMenuKeyboard`, `playDiffKeyboard`, `claimNickKeyboard`. Pure builders.
- `internal/bot/handlers_stats.go` — add `onStats`, `onStatsTab`, `statsView`.
- `internal/bot/handlers_play.go` *(new)* — `onPlay`, `onPlayGame`, `onPlayDiff`, `onPlayDuel`, `onPlayInvite`. Keeps the hub glue out of the already-large `handlers_game.go`.
- `internal/bot/handlers_game.go` — `startNewGame` appends the no-nick nag line.
- `internal/bot/handlers_auto.go` — `autoRecord` attaches `claimNickKeyboard` on unknown nicks; add `onClaimNick`.
- `internal/bot/handlers_basic.go` — soften `/setnick` reply.
- `internal/bot/messages.go` — rewrite `helpText`; add `namesMissingNick` helper.
- `internal/bot/bot.go` — `botCommands()` trimmed to 6; routes for new handlers.
- `internal/bot/messages_test.go` — new unit tests.

---

## Task 1: `/stats` tabbed dashboard (Slice B)

**Files:**
- Modify: `internal/bot/keyboards.go`
- Modify: `internal/bot/handlers_stats.go`
- Modify: `internal/bot/bot.go:114-163` (routes)
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Consumes (existing): `standingsText`, `meText`, `speedText`, `duelsText`, `historyText` from `messages.go`; `b.ensure`, `b.st.Standings/StatFor/SpeedFor/DuelRecord/Speedboard/DuelLeaderboard/RecentDuels/RecentGames/PlayerByTg/Leader`.
- Produces: `statsKeyboard(active string) *tele.ReplyMarkup`; const `cbStatsTab = "ststab"`; `b.onStats`, `b.onStatsTab`, `b.statsView`.

- [ ] **Step 1: Write the failing test**

Add to `internal/bot/messages_test.go`:

```go
func TestStatsKeyboardMarksActive(t *testing.T) {
	m := statsKeyboard("me")
	var flat []tele.InlineButton
	for _, row := range m.InlineKeyboard {
		flat = append(flat, row...)
	}
	if len(flat) != 5 {
		t.Fatalf("want 5 tab buttons, got %d", len(flat))
	}
	ids := map[string]string{} // data -> text
	for _, btn := range flat {
		if btn.Unique != cbStatsTab {
			t.Errorf("button %q has unique %q, want %q", btn.Text, btn.Unique, cbStatsTab)
		}
		ids[btn.Data] = btn.Text
	}
	for _, want := range []string{"table", "me", "speed", "duels", "history"} {
		if _, ok := ids[want]; !ok {
			t.Errorf("missing tab %q", want)
		}
	}
	if !strings.Contains(ids["me"], "•") {
		t.Errorf("active tab 'me' not marked: %q", ids["me"])
	}
	if strings.Contains(ids["table"], "•") {
		t.Errorf("inactive tab 'table' wrongly marked: %q", ids["table"])
	}
}
```

Add `tele "gopkg.in/telebot.v3"` to the test file's imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestStatsKeyboardMarksActive -v'`
Expected: FAIL — `undefined: statsKeyboard` and `undefined: cbStatsTab`.

- [ ] **Step 3: Implement the keyboard**

In `internal/bot/keyboards.go`, add to the const block (near the quick-action ids):

```go
	cbStatsTab = "ststab" // payload: "<tab>" — table|me|speed|duels|history
```

Then add:

```go
// statsTabs are the /stats dashboard tabs in display order.
var statsTabs = []struct{ id, label string }{
	{"table", "🏆 Таблица"},
	{"me", "👤 Я"},
	{"speed", "⚡ Скорость"},
	{"duels", "⚔️ Дуэли"},
	{"history", "📜 История"},
}

// statsKeyboard renders the dashboard tab row; the active tab gets a "•" marker.
func statsKeyboard(active string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, t := range statsTabs {
		label := t.label
		if t.id == active {
			label = "• " + label
		}
		btns = append(btns, m.Data(label, cbStatsTab, t.id))
	}
	var rows []tele.Row
	for i := 0; i < len(btns); i += 3 {
		end := i + 3
		if end > len(btns) {
			end = len(btns)
		}
		rows = append(rows, m.Row(btns[i:end]...))
	}
	m.Inline(rows...)
	return m
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run TestStatsKeyboardMarksActive -v'`
Expected: PASS.

- [ ] **Step 5: Add the dashboard handlers**

In `internal/bot/handlers_stats.go`, add:

```go
// statsView builds the dashboard text + tab keyboard for one tab. The "me" tab
// is rendered for whoever triggered the update (c.Sender()).
func (b *Bot) statsView(c tele.Context, tab string) (string, *tele.ReplyMarkup, error) {
	season, err := b.ensure(c)
	if err != nil {
		return "", nil, err
	}
	chatID := c.Chat().ID
	var text string
	switch tab {
	case "me":
		text, err = b.meTab(c, season)
	case "speed":
		ranked, fewer, e := b.st.Speedboard(chatID, season.ID, "medium", speedMinGames)
		if e != nil {
			return "", nil, e
		}
		text = speedText(season, "medium", ranked, fewer, speedMinGames)
	case "duels":
		rows, e := b.st.DuelLeaderboard(chatID)
		if e != nil {
			return "", nil, e
		}
		recent, e := b.st.RecentDuels(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = duelsText(rows, recent)
	case "history":
		games, e := b.st.RecentGames(chatID, 8)
		if e != nil {
			return "", nil, e
		}
		text = historyText(games)
	default:
		tab = "table"
		standings, e := b.st.Standings(chatID, season.ID)
		if e != nil {
			return "", nil, e
		}
		text = standingsText(season, standings)
	}
	if err != nil {
		return "", nil, err
	}
	return text, statsKeyboard(tab), nil
}

// meTab renders the caller's personal stats, or a join hint if unregistered.
func (b *Bot) meTab(c tele.Context, season *storage.Season) (string, error) {
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
	stat, err := b.st.StatFor(c.Chat().ID, season.ID, player.ID)
	if err != nil {
		return "", err
	}
	sp, err := b.st.SpeedFor(c.Chat().ID, season.ID, player.ID, "medium")
	if err != nil {
		return "", err
	}
	duelW, duelL, err := b.st.DuelRecord(c.Chat().ID, player.ID)
	if err != nil {
		return "", err
	}
	return meText(player.Name, stat, sp, duelW, duelL, season), nil
}

func (b *Bot) onStats(c tele.Context) error {
	text, markup, err := b.statsView(c, "table")
	if err != nil {
		return b.fail(c, "onStats", err)
	}
	return c.Send(text, markup)
}

func (b *Bot) onStatsTab(c tele.Context) error {
	text, markup, err := b.statsView(c, c.Data())
	if err != nil {
		return b.fail(c, "onStatsTab", err)
	}
	_ = c.Respond()
	return c.Edit(text, markup)
}
```

The `duels` tab mirrors `onDuels` (handlers_duel.go:145-158) verbatim: `b.st.DuelLeaderboard(chatID)` + `b.st.RecentDuels(chatID, 8)` → `duelsText(rows, recent)`.

- [ ] **Step 6: Wire routes**

In `internal/bot/bot.go` `routes()`, add after the existing stat routes (around line 139):

```go
	b.tb.Handle("/stats", b.onStats)
	b.tb.Handle(&tele.Btn{Unique: cbStatsTab}, b.onStatsTab)
```

- [ ] **Step 7: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS, no vet errors.

- [ ] **Step 8: Commit**

```bash
git add internal/bot/keyboards.go internal/bot/handlers_stats.go internal/bot/bot.go internal/bot/messages_test.go
git commit -m "feat(stats): add /stats tabbed dashboard"
```

---

## Task 2: `/play` hub (Slice C)

**Files:**
- Modify: `internal/bot/keyboards.go`
- Create: `internal/bot/handlers_play.go`
- Modify: `internal/bot/bot.go` (routes)
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Consumes (existing): `b.startNewGame(c, difficulty, mode)` (handlers_game.go:126), `b.onDuel` (handlers_duel.go:10), `b.onInvite` (handlers_invite.go:8), `validDifficulty` map (handlers_game.go:15).
- Produces: consts `cbPlayGame="pgame"`, `cbPlayDuel="pduel"`, `cbPlayInvite="pinv"`, `cbPlayDiff="pdiff"`; `playMenuKeyboard()`, `playDiffKeyboard()`; `b.onPlay`, `b.onPlayGame`, `b.onPlayDiff`, `b.onPlayDuel`, `b.onPlayInvite`.

**Design note:** `[🆕 Обычная]` opens a difficulty chooser (one tap) rather than instant-creating then mutating difficulty. usdoku fixes room difficulty at `Create`; changing post-create would require soft-deleting and recreating the pending game, colliding with the one-pending-game invariant and the running watcher. Difficulty-first delivers the same "no args to remember" with zero race risk. Medium is the prominent first button.

- [ ] **Step 1: Write the failing test**

Add to `internal/bot/messages_test.go`:

```go
func TestPlayDiffKeyboardOffersAllDifficulties(t *testing.T) {
	m := playDiffKeyboard()
	got := map[string]bool{}
	for _, row := range m.InlineKeyboard {
		for _, btn := range row {
			if btn.Unique == cbPlayDiff {
				got[btn.Data] = true
			}
		}
	}
	for _, want := range []string{"easy", "medium", "hard", "extreme"} {
		if !got[want] {
			t.Errorf("playDiffKeyboard missing difficulty %q", want)
		}
	}
}

func TestPlayMenuKeyboardHasThreeModes(t *testing.T) {
	m := playMenuKeyboard()
	uniques := map[string]bool{}
	for _, row := range m.InlineKeyboard {
		for _, btn := range row {
			uniques[btn.Unique] = true
		}
	}
	for _, want := range []string{cbPlayGame, cbPlayDuel, cbPlayInvite} {
		if !uniques[want] {
			t.Errorf("play menu missing button %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestPlay" -v'`
Expected: FAIL — `undefined: playDiffKeyboard`, `undefined: cbPlayDiff`, etc.

- [ ] **Step 3: Implement the keyboards**

In `internal/bot/keyboards.go` const block add:

```go
	cbPlayGame   = "pgame" // no payload — opens difficulty chooser
	cbPlayDuel   = "pduel" // no payload — routes to duel flow
	cbPlayInvite = "pinv"  // no payload — routes to invite flow
	cbPlayDiff   = "pdiff" // payload: "<difficulty>" — creates a normal game
```

Then add:

```go
// playMenuKeyboard is the /play hub: pick a game mode.
func playMenuKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data("🆕 Обычная игра", cbPlayGame)),
		m.Row(
			m.Data("⚔️ Дуэль", cbPlayDuel),
			m.Data("📣 Позвать всех", cbPlayInvite),
		),
	)
	return m
}

// playDiffKeyboard chooses difficulty for a normal game; Medium is prominent.
func playDiffKeyboard() *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	m.Inline(
		m.Row(m.Data("⚡ Medium (обычная)", cbPlayDiff, "medium")),
		m.Row(
			m.Data("🟢 Easy", cbPlayDiff, "easy"),
			m.Data("🔴 Hard", cbPlayDiff, "hard"),
			m.Data("💀 Extreme", cbPlayDiff, "extreme"),
		),
	)
	return m
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestPlay" -v'`
Expected: PASS.

- [ ] **Step 5: Implement the hub handlers**

Create `internal/bot/handlers_play.go`:

```go
package bot

import tele "gopkg.in/telebot.v3"

// onPlay posts the play hub: normal game / duel / invite.
func (b *Bot) onPlay(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onPlay.ensure", err)
	}
	return c.Send("🎮 <b>Что играем?</b>", playMenuKeyboard())
}

// onPlayGame swaps the hub for the difficulty chooser.
func (b *Bot) onPlayGame(c tele.Context) error {
	_ = c.Respond()
	return c.Edit("🧩 <b>Сложность?</b>", playDiffKeyboard())
}

// onPlayDiff creates a normal game at the chosen difficulty (default mode).
func (b *Bot) onPlayDiff(c tele.Context) error {
	diff := c.Data()
	if !validDifficulty[diff] {
		diff = "medium"
	}
	_ = c.Respond()
	return b.startNewGame(c, diff, "hardcore")
}

// onPlayDuel routes to the existing duel opponent picker.
func (b *Bot) onPlayDuel(c tele.Context) error {
	_ = c.Respond()
	return b.onDuel(c)
}

// onPlayInvite routes to the existing invite flow.
func (b *Bot) onPlayInvite(c tele.Context) error {
	_ = c.Respond()
	return b.onInvite(c)
}
```

Note: `startNewGame` uses `c.Send`, so the chooser message stays visible. A second tap on it is harmless — `createGameRoom`'s `ActivePendingGame` guard (handlers_game.go:91-99) blocks a duplicate pending game. Verify `onDuel`/`onInvite` read no `c.Message().Payload` that a callback context lacks; both only use `c.Args()` (empty from a callback → default difficulty), confirmed at handlers_duel.go:42 and handlers_invite.go:9.

- [ ] **Step 6: Wire routes**

In `internal/bot/bot.go` `routes()`, add near the game routes:

```go
	b.tb.Handle("/play", b.onPlay)
	b.tb.Handle(&tele.Btn{Unique: cbPlayGame}, b.onPlayGame)
	b.tb.Handle(&tele.Btn{Unique: cbPlayDiff}, b.onPlayDiff)
	b.tb.Handle(&tele.Btn{Unique: cbPlayDuel}, b.onPlayDuel)
	b.tb.Handle(&tele.Btn{Unique: cbPlayInvite}, b.onPlayInvite)
```

- [ ] **Step 7: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 8: Manual smoke (no host go; do in a Telegram test chat after deploy)**

In a private chat with the bot: `/play` → tap `🆕 Обычная игра` → tap `⚡ Medium` → expect a game post with the usdoku link + `📝 Записать результат`. Back to `/play` → `⚔️ Дуэль` → expect opponent picker. `/play` → `📣 Позвать всех` → expect invite post.

- [ ] **Step 9: Commit**

```bash
git add internal/bot/keyboards.go internal/bot/handlers_play.go internal/bot/bot.go internal/bot/messages_test.go
git commit -m "feat(play): add /play hub for game/duel/invite"
```

---

## Task 3: Trim `/` menu + rewrite help (Slice A)

**Files:**
- Modify: `internal/bot/bot.go:90-112` (`botCommands`)
- Modify: `internal/bot/messages.go:13-38` (`helpText`)
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Produces: `botCommands()` returns exactly the 6 everyday commands; `helpText` leads with `/play` and `/stats`.

- [ ] **Step 1: Write the failing test**

Add to `internal/bot/messages_test.go`:

```go
func TestBotCommandsAreTheSixEveryday(t *testing.T) {
	cmds := botCommands()
	got := make([]string, len(cmds))
	for i, c := range cmds {
		got[i] = c.Text
	}
	want := []string{"play", "stats", "join", "setnick", "players", "help"}
	if len(got) != len(want) {
		t.Fatalf("want %d menu commands, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("menu[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHelpTextLeadsWithPlayAndStats(t *testing.T) {
	for _, want := range []string{"/play", "/stats"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("helpText missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestBotCommands|TestHelpText" -v'`
Expected: FAIL — current `botCommands()` has 18 entries; `helpText` has no `/play`/`/stats`.

- [ ] **Step 3: Trim botCommands**

Replace the body of `botCommands()` in `internal/bot/bot.go` with:

```go
func botCommands() []tele.Command {
	return []tele.Command{
		{Text: "play", Description: "новая игра / дуэль / позвать всех"},
		{Text: "stats", Description: "таблица, моё, скорость, дуэли, история"},
		{Text: "join", Description: "зарегистрироваться"},
		{Text: "setnick", Description: "ник на usdoku (для авто-учёта)"},
		{Text: "players", Description: "игроки и их usdoku-ники"},
		{Text: "help", Description: "справка и все команды"},
	}
}
```

- [ ] **Step 4: Rewrite helpText**

Replace the `helpText` const in `internal/bot/messages.go` with:

```go
const helpText = `🧩 <b>Sudoku League</b> — учёт ваших игр в судоку (usdoku.com): очки, места, сезоны, дуэли.

<b>Главное</b>
/play — новая игра, дуэль или позвать всех (кнопки)
/stats — вся статистика: таблица, моё, скорость, дуэли, история
/join [имя] — встать в игру
/setnick &lt;ник&gt; — привязать ник usdoku (для авто-учёта)
/players — кто играет

<b>Ещё</b>
/result — записать результат вручную (если авто-учёт не сработал)
/newgame, /duel, /invite — то же, что /play, но командой
/status, /me, /speed, /duels, /history, /season — то же, что вкладки /stats
/export — выгрузить игры сезона в CSV

<b>Админ</b>
/settings — порог сезона, очки, мин. игроков, напоминания
/removeplayer &lt;имя&gt; — убрать игрока (прошлые игры сохранятся)
/backup — скачать базу; восстановить — пришли файл с подписью /restore`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestBotCommands|TestHelpText" -v'`
Expected: PASS.

- [ ] **Step 6: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bot/bot.go internal/bot/messages.go internal/bot/messages_test.go
git commit -m "feat(menu): trim / menu to 6 core commands, rewrite help"
```

---

## Task 4: Onboarding silent-fail fix (Slice D)

**Files:**
- Modify: `internal/bot/messages.go` (add `namesMissingNick`)
- Modify: `internal/bot/handlers_game.go` (`startNewGame` nag)
- Modify: `internal/bot/keyboards.go` (add `claimNickKeyboard` + const)
- Modify: `internal/bot/handlers_auto.go` (`autoRecord` attaches claim buttons; add `onClaimNick`)
- Modify: `internal/bot/handlers_basic.go` (soften `/setnick` reply)
- Modify: `internal/bot/bot.go` (route `onClaimNick`)
- Test: `internal/bot/messages_test.go`

**Interfaces:**
- Consumes (existing): `storage.Player` (`UsdokuNick sql.NullString`, `Name string`), `b.st.ListPlayers`, `b.st.PlayerByTg`, `b.st.SetNick`, `b.tb.Send`, `autoMappingText`.
- Produces: `namesMissingNick(players []storage.Player) []string`; const `cbClaimNick="claim"`; `claimNickKeyboard(gameID int64, nicks []string) *tele.ReplyMarkup`; `b.onClaimNick`.

- [ ] **Step 1: Write the failing test**

Add to `internal/bot/messages_test.go`:

```go
func TestNamesMissingNick(t *testing.T) {
	withNick := storage.Player{Name: "Ann", UsdokuNick: sql.NullString{String: "ann", Valid: true}}
	emptyNick := storage.Player{Name: "Bob", UsdokuNick: sql.NullString{String: "", Valid: true}}
	noNick := storage.Player{Name: "Cy"} // UsdokuNick invalid
	got := namesMissingNick([]storage.Player{withNick, emptyNick, noNick})
	want := []string{"Bob", "Cy"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestClaimNickKeyboardOneButtonPerNick(t *testing.T) {
	m := claimNickKeyboard(42, []string{"joe", "max"})
	var data []string
	for _, row := range m.InlineKeyboard {
		for _, btn := range row {
			if btn.Unique != cbClaimNick {
				t.Errorf("unexpected unique %q", btn.Unique)
			}
			data = append(data, btn.Data)
		}
	}
	want := []string{"42:joe", "42:max"}
	if len(data) != len(want) {
		t.Fatalf("want %v, got %v", want, data)
	}
	for i := range want {
		if data[i] != want[i] {
			t.Errorf("data[%d]=%q want %q", i, data[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestNamesMissingNick|TestClaimNick" -v'`
Expected: FAIL — `undefined: namesMissingNick`, `undefined: claimNickKeyboard`, `undefined: cbClaimNick`.

- [ ] **Step 3: Implement the pure helpers**

In `internal/bot/messages.go`, add:

```go
// namesMissingNick returns the names of players with no usdoku nick set, in
// input order — used to warn before a game that auto-record will skip them.
func namesMissingNick(players []storage.Player) []string {
	var out []string
	for _, p := range players {
		if !p.UsdokuNick.Valid || p.UsdokuNick.String == "" {
			out = append(out, p.Name)
		}
	}
	return out
}
```

In `internal/bot/keyboards.go`, add the const:

```go
	cbClaimNick = "claim" // payload: "<gameID>:<usdokuNick>" — bind nick to tapper
```

and the builder:

```go
// claimNickKeyboard offers one tap-to-claim button per unrecognised usdoku nick.
func claimNickKeyboard(gameID int64, nicks []string) *tele.ReplyMarkup {
	m := &tele.ReplyMarkup{}
	var rows []tele.Row
	for _, n := range nicks {
		rows = append(rows, m.Row(
			m.Data("Это я: "+n, cbClaimNick, fmt.Sprintf("%d:%s", gameID, n)),
		))
	}
	m.Inline(rows...)
	return m
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go test ./internal/bot/ -run "TestNamesMissingNick|TestClaimNick" -v'`
Expected: PASS.

- [ ] **Step 5: Pre-game nag in startNewGame**

In `internal/bot/handlers_game.go` `startNewGame` (lines 126-138), replace the two `return c.Send(...)` lines with a nag-appended version:

```go
func (b *Bot) startNewGame(c tele.Context, difficulty, mode string) error {
	room, err := b.createGameRoom(c, difficulty, mode)
	if err != nil {
		return b.fail(c, "startNewGame", err)
	}
	if room == nil {
		return nil // a guard already replied
	}
	var text string
	if room.code == "" {
		text = newGameText(difficulty, mode)
	} else {
		text = newGameWithCodeText(difficulty, mode, room.code)
	}
	if players, perr := b.st.ListPlayers(c.Chat().ID); perr == nil {
		if missing := namesMissingNick(players); len(missing) > 0 {
			text += fmt.Sprintf(
				"\n\n⚠️ Без ника (авто-учёт не сработает): <b>%s</b> — задайте /setnick.",
				esc(strings.Join(missing, ", ")))
		}
	}
	return c.Send(text, recordKeyboard(room.gameID))
}
```

`fmt` and `strings` are already imported in `handlers_game.go`.

- [ ] **Step 6: Attach claim buttons in autoRecord**

In `internal/bot/handlers_auto.go` `autoRecord`, the unknown-nick branch (lines 117-122) currently sends `autoMappingText(...)` with `recordKeyboard(game.ID)`. When there ARE unknown nicks, also offer claim buttons. Replace that branch with:

```go
	if len(unknown) > 0 || len(picks) == 0 {
		log.Printf("🤖 game %d finished, finishers=%v unknown=%v mappedJoined=%d → manual",
			game.ID, finisherNicks, unknown, mappedJoined)
		_, _ = b.tb.Send(to, autoMappingText(finisherNicks, unknown), recordKeyboard(game.ID))
		if len(unknown) > 0 {
			_, _ = b.tb.Send(to,
				"Это твой ник? Привяжу его к тебе одним тапом:",
				claimNickKeyboard(game.ID, unknown))
		}
		return
	}
```

- [ ] **Step 7: Add the onClaimNick handler**

Add to `internal/bot/handlers_auto.go`:

```go
// onClaimNick binds an unrecognised usdoku nick to the tapping player, so future
// games auto-record for them. Re-attribution of the just-finished game is left to
// a manual /result if needed (keeps this a safe, single-write action).
func (b *Bot) onClaimNick(c tele.Context) error {
	data := c.Data()
	i := strings.IndexByte(data, ':')
	if i < 0 {
		return c.Respond(&tele.CallbackResponse{Text: "Не понял ник"})
	}
	nick := data[i+1:]
	sender := realSender(c)
	if sender == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не получилось определить тебя"})
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, sender.ID)
	if err != nil {
		return b.fail(c, "onClaimNick.player", err)
	}
	if player == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала /join"})
	}
	if err := b.st.SetNick(player.ID, nick); err != nil {
		return b.fail(c, "onClaimNick.set", err)
	}
	_ = c.Respond(&tele.CallbackResponse{Text: "Готово: ник " + nick + " привязан"})
	return c.Send(fmt.Sprintf("✅ <b>%s</b> = usdoku <code>%s</code>. Дальше учёт сам.",
		esc(player.Name), esc(nick)))
}
```

`strings` and `fmt` are already imported in `handlers_auto.go`.

- [ ] **Step 8: Soften /setnick reply**

In `internal/bot/handlers_basic.go` `onSetNick` (lines 96-97), replace the success `c.Send` with:

```go
	return c.Send(fmt.Sprintf(
		"✅ usdoku-ник: <b>%s</b> сохранён — проверю в ближайшей игре.", esc(nick)))
```

- [ ] **Step 9: Route onClaimNick**

In `internal/bot/bot.go` `routes()`, add near the auto/record callbacks:

```go
	b.tb.Handle(&tele.Btn{Unique: cbClaimNick}, b.onClaimNick)
```

- [ ] **Step 10: Build + full test**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/bot/messages.go internal/bot/keyboards.go internal/bot/handlers_game.go internal/bot/handlers_auto.go internal/bot/handlers_basic.go internal/bot/bot.go internal/bot/messages_test.go
git commit -m "feat(onboarding): nag missing nicks + one-tap nick claim on auto-record"
```

---

## Final verification

- [ ] **Run the full check once more**

Run: `docker run --rm -v "$PWD":/src -w /src golang:1.25-alpine sh -c 'go build ./... && go vet ./... && go test ./...'`
Expected: all PASS.

- [ ] **Deploy + smoke (CLAUDE.md deploy rules)**

```bash
docker compose up -d --build --force-recreate
docker compose logs bot | tail
```
Expected: `bot started`. Then in a Telegram test chat: confirm `/` menu shows exactly 6 commands; `/play` and `/stats` work; an old alias (`/status`, `/me`) still responds; `/setnick` shows the softened reply.

## Self-review notes (coverage)

- Spec Slice A → Task 3. Slice B → Task 1. Slice C → Task 2. Slice D → Task 4.
- Hidden-alias regression covered by the deploy smoke step (old commands still routed; only `botCommands()` menu list changed, routes untouched).
- `/season` folded into the `table` tab header (spec §B): `standingsText` already renders season number + target + leader progress, so no separate season tab is needed; `/season` remains as an alias.
- usdoku no-lookup constraint honoured: nick verification is contextual (pre-game nag + post-game claim), no API calls added at `/setnick` time.
