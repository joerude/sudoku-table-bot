# Recognition Layer — Design

Date: 2026-06-24
Status: Approved
Goal: add engagement/recognition features to the Sudoku League bot — an all-time records board, win/play-day streaks + achievement badges, and a weekly digest — reusing existing render/storage/reminder infrastructure.

## Decisions

- Streaks + achievements: **computed-on-read** from `games`+`game_results` (no achievements table, no push-on-earn).
- Records board: new **all-time tab in `/stats`** (cross-season), reusing the tab router built in the UX-simplification work.
- Streaks tracked: **win streak** (consecutive `rank=1`) + **play-day streak** (consecutive calendar days with a completed game, chat-tz bucketed).
- Weekly digest: **Monday**, fires at the existing per-chat `daily_time` (tz-aware), toggleable via `/settings weekly on|off`, skipped when zero games in the trailing 7 days. Requires two additive `chats` columns (`weekly_digest`, `last_weekly`).
- Badges: cross-season cumulative, ~6 fixed badges, computed.
- Digest posts to the chat, HTML, reusing `medal`/`mention`/`standingsText`-style render.

## Constraints / invariants preserved

- Records and streaks exclude duels (`duel_target_id IS NULL`) and soft-deleted/ pending games (`status='completed' AND deleted=0`) — same filter `Speedboard` uses.
- Records use only auto-record solve times (`duration_secs IS NOT NULL`).
- No change to scoring, season rollover, the picker, auto-record, or duel logic.

## Slices

### Slice A — Records board (smallest, independent)
- `RecordsBoard(chatID) ([]RecordRow, error)` — per difficulty, the fastest all-time solve + holder. SQLite min-row idiom: `SELECT g.difficulty, MIN(gr.duration_secs), p.name ... GROUP BY g.difficulty`.
- `recordsText(rows)` render; difficulties ordered easy<medium<hard<extreme (sorted in Go).
- New `records` tab in `statsTabs` + `statsView`.

### Slice B — Streak + badge computation (pure + storage; not yet shown)
- Pure (domain): `WinStreak(ranks []int) int`, `DayStreak(dates []string, today string) int` — unit-tested.
- Storage: `RecentRanks(chatID, playerID) ([]int, error)` (completed non-duel games, newest first), `PlayedTimes(chatID, playerID) ([]string, error)` (completed_at UTC strings, newest first), `CareerStats(chatID, playerID) (wins, games, bestSecs int, err error)` (cross-season), `SeasonsWon(chatID, playerID) (int, error)`.
- Pure badge builder `badges(in BadgeInput) []string` with BadgeInput{Wins, Games, BestSecs, WinStreak, DayStreak, SeasonsWon int}.

### Slice C — Display streaks + badges in /me (depends on B)
- Bot helper `meExtra(chatID, playerID, tz) string` assembling streak lines + badge row from B's pieces (parses `PlayedTimes` into chat-tz dates, computes today in tz).
- Appended by both `meTab` (/stats Я) and `onMe` (alias) so both surfaces match.

### Slice D — Weekly digest (depends on B)
- schema.sql `chats` += `weekly_digest INTEGER NOT NULL DEFAULT 1`, `last_weekly TEXT`; same two lines added to `migrate()` ALTER list.
- `ChatSettings` += `WeeklyDigest bool`, `LastWeekly string`; `AllChats` selects them.
- `remindWeekly()` in reminders.go, called from `runReminders` tick: for each chat with `weekly_digest`, when local weekday==Monday and `now>=daily_time` and `last_weekly!=today` → set last_weekly (once-guard), build + send digest (skip if 0 games in trailing 7 days).
- `digestText(...)`: top-3 standings, fastest solve of the last 7 days, longest current win streak, games-this-week count.
- Storage: `GamesSince(chatID, sinceUTC) (int, error)`, `FastestSince(chatID, sinceUTC) (*SpeedRow-ish, error)` for the week window.
- `/settings weekly on|off` toggle via new `SetWeeklyDigest(chatID, bool)`.

## Build order

A → B → C → D (C and D both consume B's pieces; C is the simpler consumer, do it first).

## Out of scope

- Achievements persistence / push-on-earn.
- Elo/rating.
- Daily auto-challenge.
- Per-difficulty records in the digest (digest shows week highlights only).

## Testing

- Pure functions (`WinStreak`, `DayStreak`, `badges`, `recordsText`, `digestText`) → unit tests in `internal/bot/messages_test.go` / `internal/domain`.
- Storage queries + reminder/glue wiring → manual smoke (repo convention).
- All build/test in docker (`golang:1.25-alpine`).
