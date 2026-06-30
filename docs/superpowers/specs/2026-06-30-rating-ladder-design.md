# Rating Ladder (ELO) + Crown — gamification layer

**Date:** 2026-06-30
**Status:** Design approved (decisions locked), pending spec review.

## Problem

The bot already tracks points, seasons, duels, streaks, badges, records, weekly
digest. The pain is **not** coordination — it's **excitement**. A season's points
table gets "solved": once someone is ahead, the others lose hope and interest dies
(затухание). We want a gamification engine that keeps the stakes alive every game.

Group is **3 fixed friends** (CLAUDE.md currently says ~5 — fix to 3 during impl).

## Engine chosen

**Living rating (ELO) + crown**, as the structural backbone, with a thin
**narrative** layer (crown-change announcement) on top.

Why ELO: it's the only layer that **cannot be "solved"** — every game has stakes,
#3 can always climb, the number moves up *and* down. The crown (current #1) can be
stolen in any single game. This directly cures the "season solved → boring" death.

## Locked decisions

| # | Decision | Choice |
|---|----------|--------|
| Engine | primary | Living rating (ELO) + crown; narrative as a thin layer |
| Feed | which games | **All** games (season multiplayer + duels), ≥2 ranked participants |
| Coexist | vs season points | **Parallel.** Season = current campaign; rating = eternal skill ladder. Season points untouched. |
| Algorithm | rank vs speed | **Pure rank** classic ELO. Speed stays in /speed & badges. |
| Reset | season vs eternal | **Eternal**, all-time. No per-season reset. |
| Persistence | storage | **On-demand replay**, no rating table. Pure function of game history. |
| DNF | treatment | **DNF = loss** to all finishers; DNF-vs-DNF ignored. |
| Delivery | v1 surfaces | (1) rating delta in result post, (2) crown-change line, (3) `/rating` ladder |
| Calibration | K-factor | provisional (<10 games) **K=64**, established **K=32**; start **1000** |
| Crown tie | exact equal rating | incumbent keeps crown (no flip-flop) |
| Backfill | first deploy | replay full existing history (free — same code path) |

## Rating algorithm (pure)

Lives in `internal/domain/rating.go`, pure, no I/O — mirrors `scoring.go`,
`streaks.go`. Single source of truth.

**Inputs:** chronological list of games (ordered by `created_at`, tie-break `id`),
each game a list of `(playerID, rank)` participants. `rank ≥ 1` = finish place,
`rank = 0` = DNF. Deleted games already excluded by the caller.

**Per game**, build the set of ordered pairs with a defined outcome:
- finisher vs finisher → lower rank number wins;
- finisher vs DNF → finisher wins;
- DNF vs DNF → **no pair** (ignored).

A game contributes to the rating only if it yields ≥1 pair (i.e. ≥2 participants
and at least one finisher). Solo games change nothing.

**Per-player update for a game:**
- Expected vs opponent b: `E = 1 / (1 + 10^((R_b − R_a)/400))`.
- Actual `S`: win = 1, loss = 0 (ranks strictly ordered → no draws).
- Each player updates with **their own** K (USCF-style asymmetric K), chosen by
  that player's games-played-so-far during the replay: `< 10 → 64`, else `32`.
- **K split:** a player's per-pair K is `K_p / opponents_p`, where `opponents_p`
  is the count of that player's defined pairs this game. This bounds each player's
  total swing per game to ≈ K_p (a 3-player game doesn't move you 2× a duel).
- Sum the per-pair deltas, apply once per game.

**Duel = N=2** falls out of the same formula (one pair, full K). No special case.

**Derived (all from the same replay):**
- **current rating** per player (start 1000);
- **peak** = max rating ever held (tracked across the replay, incl. provisional);
- **games played**, **provisional flag** (`games < 10`);
- **per-game deltas** (replay state before vs after a given game) — feeds the
  result-post footer;
- **crown** = highest current rating in the chat; exact tie → incumbent keeps it;
- **crown-change** = top playerID before vs after the last game.

Determinism: same game list → same output. Replay is idempotent and order-stable.

## Architecture & files

- **`internal/domain/rating.go`** (new, pure): types + `ComputeRatings(games) →`
  result exposing per-player state, per-game deltas, ladder (sorted desc), crown.
- **`internal/domain/rating_test.go`** (new): table tests (see Testing).
- **`internal/storage/rating.go`** (new) or method on existing store:
  `GamesForRating(chatID) → []RatingGame` — all non-deleted games for the chat
  with their `(playerID, rank)` results, ordered `created_at, id`. Includes both
  season games and duels (rating ignores the `duel_target_id` split).
- **`internal/bot/handlers_rating.go`** (new): `onRating` — renders the ladder.
- **`internal/bot/messages.go`**: `ratingLadder(...)`, `ratingDeltaLines(...)`,
  `crownChangeLine(...)` text builders (all user text via `esc()`).
- **`internal/bot/bot.go`**: route `/rating`; no other route changes.
- **Result-post wiring:** a `b.ratingFooter(game) string` helper, appended to the
  `result` text in **both** sites that already render a result:
  - `finalize` (manual) — [handlers_game.go:557](../../internal/bot/handlers_game.go) (`c.Edit(result, ...)`);
  - `autoRecord` (auto) — [handlers_auto.go:213](../../internal/bot/handlers_auto.go) (`b.tb.Send(..., autoResultHeader()+result, ...)`).
  Both funnel through `b.scoreAndCheck(game)`; the footer is computed after it from
  one replay and concatenated. Result posts stay non-ephemeral (valuable).
- **`CLAUDE.md`**: fix group size ~5 → 3; document the rating model under "Модель
  данных" (rating = pure function of history, eternal, parallel to season).

## Data flow

1. Game recorded (manual finalize or auto). Game + results persisted as today.
2. `scoreAndCheck` returns the existing `result` text (season points unchanged).
3. `ratingFooter(game)`: `GamesForRating(chatID)` → `ComputeRatings` → take this
   game's per-player deltas + new ratings + crown-change → render footer:
   ```
   📊 Рейтинг: Зура +14 → 1042, Айбек −9 → 988
   👑 Корона сменилась: Зура свергает Айбека!   (only if changed)
   ```
4. Footer appended to `result`; posted via the existing path (Edit / Send).
5. `/rating` → same `ComputeRatings`, renders the full ladder with 👑, peak,
   provisional marker.

No new writes on the rating path — it is read-only derivation. Mutations
(edit/delete/restore) automatically reflect on the next replay; already-posted
historical footers are not rewritten (they are chat history).

## Edge cases

- Solo game / single finisher with no opponents → no pairs → no rating change,
  footer omitted.
- All-DNF game → no pairs → no change.
- Provisional players shown on the ladder with a marker (e.g. `?` or `(калибр.)`).
- Equal ratings in the ladder → stable order (rating desc, then peak desc, then
  player id) so display doesn't jitter.
- Deleting/editing an old game shifts later history (ELO is path-dependent) — this
  is correct and intended; replay handles it for free.

## Out of scope (v1)

`/stats` tab, `/me` integration, rating trend/history charts, peak leaderboard,
trash-talk commentator beyond the crown line, wagers/betting, handicaps,
Glicko/RD. Add later only if the core proves fun.

## Testing (TDD, run in docker per CLAUDE.md)

Pure `rating_test.go`:
- 2-player win/loss: symmetric deltas, equal starts → ±32 (or ±64 provisional).
- 3-player ranks 1/2/3: pairwise sum, |delta| ≤ K (K-split bound holds).
- DNF = loss to finishers; DNF-vs-DNF ignored (two DNFs don't move each other).
- Provisional boundary: K=64 for games <10, K=32 from the 10th onward (per player).
- Peak tracking: peak ≥ current, captured at the high-water mark.
- Crown: highest rating; exact tie keeps incumbent; crown-change detection.
- Determinism / idempotency: same input → same output; replay stable.
- Delete-game recompute: removing a mid-history game yields the expected replay.

Render tests in `messages_test.go` style: footer lines, crown line, ladder layout.

Telegram I/O glue (`onRating` wiring, `ratingFooter` plumbing) — not unit-tested,
per repo convention.
