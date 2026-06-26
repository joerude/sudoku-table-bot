# Competitive Feedback Layer — Design

Date: 2026-06-26
Status: Approved
Goal: add three competitive-feedback features — podium overtake pings, a personal head-to-head tab, and push achievement announcements — building on the recognition layer.

## Decisions

- **Overtake ping**: podium-only — fire when a finalized game produces a new season leader OR a new top-3 entrant. Appended to the game result message (no separate spam). Skipped when the game ends the season.
- **H2H**: personal — a new `/stats` tab showing the tapping player's record vs each other active player (who finished ahead more often), season-scoped, computed.
- **Push achievements**: a new `achievements` table; after each finalized game, recompute participants' badges, persist + announce newly earned ones. The computed badge row in `/me` stays.

## Constraints / invariants preserved

- `domain` stays a leaf package (storage→domain; domain must NOT import storage). Overtake's pure logic uses a local `domain.RankEntry`, not `storage.Standing`.
- All season queries exclude duels + non-final games (`status='completed' AND deleted=0 AND duel_target_id IS NULL`); H2H additionally requires both players `rank>0` (DNF not a head-to-head result).
- No change to scoring math, season rollover, picker, auto-record matching, or duel logic. `scoreAndCheck` gains an appended overtake line + a participant-badge announce hook; its scoring behavior is unchanged.
- New table via `schema.sql` `CREATE TABLE IF NOT EXISTS` (runs every Open) — no `migrate()` ALTER.

## Slices

### Slice A — H2H tab (smallest, independent, no finalize changes)
- Storage `HeadToHeadAll(chatID, seasonID, playerID) ([]H2HRow, error)` — self-join `game_results` on shared games; per opponent count `wins` (tapper.rank < opp.rank) / `losses` (tapper.rank > opp.rank), both `rank>0`.
- `h2hText(name string, rows []H2HRow) string`.
- New tab `{"h2h", "⚔️ Очные"}` in `statsTabs`; `statsView` `h2h` case resolves the tapper via `PlayerByTg` (hint if unregistered), like the `me` tab.

### Slice B — Overtake ping (podium-only)
- Pure `domain.RankEntry{ID int64; Name string; Points int}` + `domain.PodiumChanges(prev, cur []RankEntry) (newLeader string, enteredTop3 []string)`.
- In `scoreAndCheck`: capture `Standings` BEFORE `FinalizeGame` (prev) and reuse the AFTER standings (cur); map both to `[]RankEntry`; if no season-end, append `overtakeText(...)` to the result.
- `overtakeText(newLeader string, entered []string) string` (empty when nothing notable).

### Slice C — Push achievements (table + announce)
- `schema.sql`: `achievements(id, chat_id, player_id, badge TEXT, earned_at)` with `UNIQUE(chat_id, player_id, badge)`.
- Storage `EarnedBadges(chatID, playerID) ([]string, error)`, `AddBadge(chatID, playerID int64, badge string) error` (INSERT OR IGNORE).
- `badgeLabel(emoji) string` (human name per badge) in messages.go.
- Bot `syncBadges(chatID, playerID) ([]string, error)` — recompute `domain.Badges` for the player (reuse recognition-layer storage), diff vs `EarnedBadges`, persist new, return newly earned emojis. `announceBadges(game)` iterates the game's participants, collects new badges, posts one announce message.
- Wired after the result send in BOTH `finalize` (manual) and `autoRecord` (auto).

## Build order

A → B → C. A touches no finalize code; B and C both touch the finalize path — done after A and sequenced to avoid overlap.

## Out of scope

- Per-pair H2H picker (personal-only this pass).
- Overtake notifications below the podium.
- Elo / handicap, PNG charts.
- Backfilling achievements for historical games (badges start accruing from first finalize after deploy; computed `/me` badges already reflect history).

## Testing

- Pure functions (`PodiumChanges`, `h2hText`, `overtakeText`, `badgeLabel`) → unit tests.
- Storage queries + finalize/announce glue → manual smoke (repo convention).
- All build/test in docker.
