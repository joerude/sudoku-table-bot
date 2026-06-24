# UX Simplification — Design

Date: 2026-06-24
Status: Approved
Goal: simplify usage, reduce cognitive load, enhance UI/UX of the Sudoku League Telegram bot (group of ~5 friends).

## Problem

21 slash commands for a 5-person group. Concrete pain:

1. **Onboarding silent-fail** — every player must do `/join` *and* `/setnick <exact usdoku nick>`. Wrong/missing nick silently drops them from auto-record; no feedback until results look wrong.
2. **`/newgame` arg recall** — must remember `easy|medium|hard|extreme` + `[hardcore|original]`.
3. **6 overlapping stat commands** — `/status /season /me /history /speed /duels`; unclear which gives what.
4. **3 separate create paths** — `/newgame /duel /invite` all create a usdoku room but feel unrelated.
5. **Quick-menu thin** — only 3 buttons, shown on welcome only, lost in scrollback.

Preserve (current wins, do NOT touch): finish-order tap picker, auto-record watcher, ephemeral service noise, soft-delete/restore, scoring (`domain/scoring.go`), season logic, DB schema.

## Feasibility constraint

`internal/usdoku/client.go` exposes only `Create(difficulty, mode, visibility)` and `Info(gameCode)`. **No global nick-lookup / search / profile endpoint.** Standalone "does this nick exist?" verification at `/setnick` time is impossible without an undocumented endpoint. Onboarding fix therefore works *contextually* (at game time), not at `/setnick` time.

## Decisions

- Interaction model: **fewer commands + grouping** (slash-first; consolidate overlapping commands). Not a persistent hub message.
- Onboarding: **contextual match + nag** (no API change).
- Stats: **one `/stats` dashboard with inline tabs**, edits in place.
- `/newgame`: **smart default (Medium) + buttons**.
- Create paths: **one `/play` hub command**.
- Old commands stay as **hidden aliases** (handlers unchanged) — no breakage for muscle memory, docs, links.

## Design

### Slice A — Command-menu trim (smallest, ship first)

Trim the Telegram `/` menu list returned by `commands()` ([internal/bot/bot.go:92](../../../internal/bot/bot.go#L92)) to 6 everyday commands. All other handlers stay registered (still callable), just unlisted.

Shown in `/` menu:

| Command | Purpose |
|---|---|
| `/play` | start any game (hub) |
| `/stats` | all statistics (dashboard) |
| `/join` | register |
| `/setnick` | bind usdoku nick |
| `/players` | who plays |
| `/help` | full reference |

Hidden but functional aliases: `/newgame /duel /invite /status /table /season /me /history /speed /duels /settings /backup /removeplayer /export /restore`.

Admin commands (`/settings /backup /removeplayer /export`) optionally listed only when the caller is admin, if telebot scope allows; otherwise kept in `/help` under an Admin section. `/help` text updated to lead with the 6 core commands and list the rest as "ещё / для админа".

Risk: none functional — only the displayed menu changes.

### Slice B — `/stats` dashboard (6 stat commands → 1)

`/stats` posts the season table with an inline tab row, edited in place via `c.Edit` (no message spam, mirrors existing picker pattern):

`[🏆 Таблица] [👤 Я] [⚡ Скорость] [⚔️ Дуэли] [📜 История]`

- **Таблица** — `standingsText` (already includes season number, target, leader progress; absorbs `/season`).
- **Я** — `meText` for the tapping user.
- **Скорость** — `speedText` (default difficulty `medium`).
- **Дуэли** — `duelsText`.
- **История** — `historyText`.

Each tab reuses an existing builder in [internal/bot/messages.go](../../../internal/bot/messages.go) verbatim. New code: a `statsKeyboard(active)` builder + a tab-router callback handler (`cbStatsTab`, payload = tab id). Tapping a tab re-renders text + keyboard with the active tab marked.

Per-user note: **Я** tab is caller-specific. Since the message is shared in a group, the router renders the tab for whoever tapped (callback `c.Sender()`), consistent with how `/me`-style data is already keyed.

`/me`, `/status`, `/season`, `/speed`, `/duels`, `/history` remain as hidden aliases that jump straight to their tab.

### Slice C — `/play` hub (3 create paths → 1)

`/play` posts a chooser:

`[🆕 Обычная] [⚔️ Дуэль] [📣 Позвать всех]`

- **Обычная** (`cbPlayGame`) → instantly creates a **Medium / original** room via the existing `createGameRoom` core, posts the game message with a **[⚙️ Сложность ▾]** control to change difficulty/mode *before* anyone joins, plus the existing record button. No args required. `/newgame <args>` alias still bypasses the picker for power users.
- **Дуэль** (`cbPlayDuel`) → existing `/duel` opponent-picker flow.
- **Позвать всех** (`cbPlayInvite`) → existing `/invite` flow.

Difficulty change before join: re-create or re-key the room. Simplest correct version — the `[⚙️ Сложность ▾]` control is only valid until the first participant joins / the watcher records; afterward it's hidden. If changing difficulty after room creation is non-trivial with usdoku (room difficulty fixed at `Create`), fall back to: `/play → Обычная` shows the difficulty buttons FIRST (one extra tap), then creates. Implementation picks whichever is correct against `createGameRoom`; the user-facing promise is "no command args to remember."

`createGameRoom` already centralizes guards + room creation + watcher; `/play` branches are wiring, not new game logic.

### Slice D — Onboarding silent-fail fix (no API change)

- **Pre-game nag:** when `/play` (or `/newgame`) creates a room, if any `active` player has no `usdoku_nick`, append a line: `⚠️ без ника (авто-учёт не сработает): <names>`. Shown on the game post.
- **Post-game claim:** in the auto-record path ([internal/bot/handlers_auto.go](../../../internal/bot/handlers_auto.go)), when usdoku returns participant nicks that map to no registered player (existing `unknown` set in `autoMappingText`), render one-tap **[Это я: <nick>]** buttons (`cbClaimNick`, payload `<gameID>:<nick>`). Tapping binds that nick to the tapper (`SetNick` equivalent) and, where possible, re-attributes the result. Turns a silent drop into a one-tap correction.
- **`/setnick` reply** softened to an honest confirmation: `✅ ник <nick> сохранён — проверю в ближайшей игре.` (no live lookup exists).

Claim button binds nick only; re-running attribution for the just-finished game is best-effort and may be deferred to the next game if mid-game re-attribution is risky.

## Build order

Independent, separately shippable PRs:

1. **A** — command-menu trim + `/help` rewrite. Instant cognitive-load win, lowest risk.
2. **B** — `/stats` dashboard tabs.
3. **C** — `/play` hub.
4. **D** — onboarding contextual fix.

## Out of scope

- Persistent pinned hub message.
- Hard-removal of old commands (kept as aliases).
- Any usdoku API addition / undocumented-endpoint probing.
- DB schema changes, scoring changes, season-logic changes.

## Testing

- Pure render builders (`statsKeyboard`, updated `/help` text, nag line) — unit tests in `messages_test.go` (docker: `go test ./...`).
- Tab router, `/play` branches, claim-nick callback — Telegram I/O glue, manual smoke in a test group (consistent with repo convention: glue not unit-tested).
- Regression: confirm all hidden aliases still route to their original handlers.
