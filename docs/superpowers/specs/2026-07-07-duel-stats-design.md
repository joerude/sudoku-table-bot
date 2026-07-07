# Duel stats expansion — design

**Date:** 2026-07-07
**Status:** approved (verbal)

## Goal

Enrich duel statistics (the `/duels` command + `/stats`→duels tab, one shared
renderer `duelsText`) with three new metrics derivable from existing data, plus
per-player duel detail in `/me`. Completion % is explicitly **out of scope**
(deferred; see below).

## Scope

In:
1. **Head-to-head (H2H) matrix** — pairwise duel records among active players.
2. **Duel win streaks** — each player's current + best consecutive-win run in duels.
3. **Duel solve time** — avg/best solve seconds in duels (auto-recorded only).
4. `/me` surfaces the caller's own duel solve-time + current/best streak.

Out (deferred to a separate spike):
- **Completion %** (the usdoku 100%/42%). Not in the current API model —
  `usdoku.Player` decodes only `Name/Color/JoinedAt/CompletedAt`. Capturing it
  needs: probe raw `/info` for a percent field → new `game_results` column →
  auto-record wiring → only populates going forward. Blocked on the API probe.

## Data model

No schema change. All three metrics come from existing tables via the
`sqlDuelGames` predicate (`status='completed' AND deleted=0 AND
duel_target_id IS NOT NULL`).

## Storage / domain API (new)

### `storage.HeadToHeadAll(chatID int64) ([]H2HPair, error)`
```
type H2HPair struct {
    AID, BID       int64
    AName, BName   string
    AWins, BWins   int   // wins in duels where BOTH played
}
```
One self-join query over `game_results` on the same duel game, `a.player_id <
b.player_id`, both players `active=1`; only pairs with ≥1 mutual completed duel.
Ordered by total games desc, then names. (Replaces N² `HeadToHead` calls; the
existing single-pair `HeadToHead` stays for the duel-result post.)

### `storage.DuelSpeed(chatID int64) ([]SpeedRow, error)`
Mirrors `Speedboard` but with `sqlDuelGames` and no season/difficulty filter
(duels are few; aggregate across difficulties). Per active player: `AVG/MIN/COUNT`
of `duration_secs` where `NOT NULL`. Ordered avg asc. Reuses existing `SpeedRow`.

### `domain.DuelStreaks(pairs []storage.DuelPair) map[int64]Streak`
```
type Streak struct { Current, Best int }
```
Pure function over `storage.DuelPairs()` (already chronological, oldest first).
Builds each player's W/L sequence, computes best consecutive-win run and current
trailing wins (reset by any loss). DNF-vs-DNF pairs never appear (DuelPairs skips
them). Unit-tested in `internal/domain`.

## Rendering

### `duelsText` (duels tab + `/duels`)
Layout, top to bottom:
1. Leaderboard rows (unchanged) + ` 🔥N` suffix when current duel win-streak ≥ 2.
2. `Личные встречи` block — one line per H2H pair: `Nur 3–1 Joe Rude`.
3. `⏱ В дуэлях` block — per player with ≥1 timed duel: `Name — avg (лучшее best, N)`.
4. `Последние дуэли` log (unchanged).

Group is 3 friends → at most 3 pairs, 3 speed rows: stays well within Telegram's
message limit. All names through `esc()`.

### `meText` (`/me` + me tab)
After the existing `⚔️ Дуэли: W–L` line, when data exists:
- `⏱ В дуэлях: avg (лучшее best)`
- `🔥 Серия дуэлей: cur (лучшая best)`

`meTab`/`onMe` fetch the caller's duel speed (filter `DuelSpeed` by name, or a
small `DuelSpeedFor`) and streak (`DuelStreaks(pairs)[playerID]`).

## Error handling

Storage methods return errors up to handlers (existing `b.fail`/`b.ephemeral`
pattern). The `/me` streak+speed additions are best-effort like `meExtra`: on a
storage error, log and omit the extra lines so the core view still renders.

## Testing (TDD, docker)

- `internal/storage`: `HeadToHeadAll`, `DuelSpeed` — happy path, excludes
  deleted/pending/season games, excludes DNF/NULL-duration, empty case.
- `internal/domain`: `DuelStreaks` — empty, single win, current-streak reset by a
  loss, best > current, player appears only as loser.
- Render funcs: table-driven strings for the new `duelsText`/`meText` blocks
  (empty vs populated).

## Rollout

Build/vet/test in docker (`golang:1.25-alpine`). Deploy per CLAUDE.md
(`--force-recreate`, verify unique string in binary, check `bot started`).
No migration, no prod-data change (the missing 6 Jul duel was already inserted
manually as game 146).
