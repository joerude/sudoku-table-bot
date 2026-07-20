package bot

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// closeExpiredSeason has four decision branches (see internal/bot/reminders.go):
// assign a missing deadline, rewrite an unreadable one, no-op while the
// deadline is still in the future, extend an expired-but-empty season, and
// close-and-advance a season that was actually played. The first four touch
// only b.st, so they're exercised below against a real temp *storage.Store
// with a bare &Bot{st: st} — no telebot double needed, matching this repo's
// "test the DB logic, skip the Telegram I/O glue" convention (see CLAUDE.md).
//
// The close-and-advance (winner) branch always ends with b.tb.Send(...), which
// needs a working *tele.Bot. Checked before skipping it: telebot.v3's
// Settings.Offline only skips the getMe() handshake during NewBot — Send
// itself still performs a real HTTP POST to the Bot API, and this repo has no
// fake-transport double for that anywhere. Building one (an httptest server
// standing in for api.telegram.org) would be new Telegram-I/O test
// infrastructure this codebase deliberately doesn't have, so that branch is
// left uncovered here, consistent with the stated convention.

// openTempStore opens a fresh on-disk SQLite store for a single test, the
// same pattern as internal/storage's own openTemp helper.
func openTempStore(t *testing.T) *storage.Store {
	t.Helper()
	st, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCloseExpiredSeason_AssignsDeadlineWhenMissing(t *testing.T) {
	st := openTempStore(t)
	const chat = int64(-9001)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	if se.Deadline.Valid {
		t.Fatalf("fresh season already has a deadline: %+v", se.Deadline)
	}

	b := &Bot{st: st}
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	before := time.Now().UTC()
	if err := b.closeExpiredSeason(ch); err != nil {
		t.Fatalf("closeExpiredSeason: %v", err)
	}
	after := time.Now().UTC()

	got, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after: %v", err)
	}
	if got.ID != se.ID {
		t.Fatalf("assign branch must not close/replace the season: got id %d, want %d", got.ID, se.ID)
	}
	if !got.Deadline.Valid || got.Deadline.String == "" {
		t.Fatalf("deadline was not assigned: %+v", got.Deadline)
	}
	dl, err := parseDBTime(got.Deadline.String)
	if err != nil {
		t.Fatalf("assigned deadline unparseable: %q: %v", got.Deadline.String, err)
	}
	// domain.SeasonDeadline is called with time.Now() inside the function, a
	// moment after `before` — bracket both instants so the assertion isn't
	// flaky right at a month boundary.
	want1 := domain.SeasonDeadline(before, time.UTC)
	want2 := domain.SeasonDeadline(after, time.UTC)
	if !dl.Equal(want1) && !dl.Equal(want2) {
		t.Errorf("assigned deadline = %s, want %s (or %s if a month rolled over mid-call)", dl, want1, want2)
	}
}

func TestCloseExpiredSeason_NoopWhenDeadlineInFuture(t *testing.T) {
	st := openTempStore(t)
	const chat = int64(-9002)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	futureDeadline := fmtDBTime(time.Now().UTC().Add(48 * time.Hour))
	if err := st.SetSeasonDeadline(se.ID, futureDeadline); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}

	b := &Bot{st: st}
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	if err := b.closeExpiredSeason(ch); err != nil {
		t.Fatalf("closeExpiredSeason: %v", err)
	}

	got, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after: %v", err)
	}
	if got.ID != se.ID || got.Number != se.Number {
		t.Fatalf("no-op branch must leave the same active season: got %+v, want id=%d number=%d", got, se.ID, se.Number)
	}
	if got.Deadline.String != futureDeadline {
		t.Errorf("deadline changed: got %q, want unchanged %q", got.Deadline.String, futureDeadline)
	}
	archived, err := st.ArchivedNumbers(chat)
	if err != nil {
		t.Fatalf("ArchivedNumbers: %v", err)
	}
	if len(archived) != 0 {
		t.Errorf("no-op branch must not archive any season, got %v", archived)
	}
}

func TestCloseExpiredSeason_ExtendsWhenExpiredAndNothingScored(t *testing.T) {
	st := openTempStore(t)
	const chat = int64(-9003)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	// Players exist (so Standings returns rows) but nobody has played a game,
	// so every row is 0 points — the "nothing scored" half of the guard.
	if _, _, err := st.RegisterPlayer(chat, 1, "Alice"); err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}
	if _, _, err := st.RegisterPlayer(chat, 2, "Bob"); err != nil {
		t.Fatalf("RegisterPlayer: %v", err)
	}
	staleDeadline := "2020-01-01 00:00:00"
	if err := st.SetSeasonDeadline(se.ID, staleDeadline); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}
	staleDL, err := parseDBTime(staleDeadline)
	if err != nil {
		t.Fatalf("parseDBTime(stale): %v", err)
	}

	b := &Bot{st: st}
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	before := time.Now().UTC()
	if err := b.closeExpiredSeason(ch); err != nil {
		t.Fatalf("closeExpiredSeason: %v", err)
	}
	after := time.Now().UTC()

	got, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after: %v", err)
	}
	if got.ID != se.ID {
		t.Fatalf("empty-extend branch must not close the season: got id %d, want %d", got.ID, se.ID)
	}
	archived, err := st.ArchivedNumbers(chat)
	if err != nil {
		t.Fatalf("ArchivedNumbers: %v", err)
	}
	if len(archived) != 0 {
		t.Errorf("empty-extend branch must not archive any season, got %v", archived)
	}

	dl, err := parseDBTime(got.Deadline.String)
	if err != nil {
		t.Fatalf("extended deadline unparseable: %q: %v", got.Deadline.String, err)
	}
	if !dl.After(staleDL) {
		t.Errorf("extended deadline %s is not after the stale one %s", dl, staleDL)
	}
	want1 := domain.ExtendSeasonDeadline(staleDL, before, time.UTC)
	want2 := domain.ExtendSeasonDeadline(staleDL, after, time.UTC)
	if !dl.Equal(want1) && !dl.Equal(want2) {
		t.Errorf("extended deadline = %s, want %s (or %s if a month rolled over mid-call)", dl, want1, want2)
	}
}

// TestCloseExpiredSeason_ExtendsWhenLeaderBelowPointThreshold covers the
// nonzero-but-quiet case the old `Points == 0` check missed: a couple of
// games did happen, the leader has some points, but still under
// minSeasonPointsToClose — still too quiet to crown anyone.
func TestCloseExpiredSeason_ExtendsWhenLeaderBelowPointThreshold(t *testing.T) {
	st := openTempStore(t)
	const chat = int64(-9004)
	if err := st.EnsureChat(chat, 1); err != nil {
		t.Fatalf("EnsureChat: %v", err)
	}
	se, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason: %v", err)
	}
	alice, _, err := st.RegisterPlayer(chat, 1, "Alice")
	if err != nil {
		t.Fatalf("RegisterPlayer Alice: %v", err)
	}
	bob, _, err := st.RegisterPlayer(chat, 2, "Bob")
	if err != nil {
		t.Fatalf("RegisterPlayer Bob: %v", err)
	}
	// Two games, Alice wins both: 3 + 3 = 6 points — nonzero, still < 10.
	for i := 0; i < 2; i++ {
		gid, err := st.CreatePendingGame(chat, se.ID, 1, "medium", "hardcore")
		if err != nil {
			t.Fatalf("CreatePendingGame: %v", err)
		}
		if err := st.AddPick(gid, alice.ID); err != nil {
			t.Fatalf("AddPick alice: %v", err)
		}
		if err := st.AddPick(gid, bob.ID); err != nil {
			t.Fatalf("AddPick bob: %v", err)
		}
		if err := st.FinalizeGame(gid, se.PointsTable); err != nil {
			t.Fatalf("FinalizeGame: %v", err)
		}
	}
	standings, err := st.Standings(chat, se.ID)
	if err != nil {
		t.Fatalf("Standings: %v", err)
	}
	if len(standings) == 0 || standings[0].Points == 0 || standings[0].Points >= minSeasonPointsToClose {
		t.Fatalf("test setup: leader has %v points, want a nonzero value below %d",
			standings, minSeasonPointsToClose)
	}

	staleDeadline := "2020-01-01 00:00:00"
	if err := st.SetSeasonDeadline(se.ID, staleDeadline); err != nil {
		t.Fatalf("SetSeasonDeadline: %v", err)
	}

	b := &Bot{st: st}
	ch := storage.ChatSettings{ChatID: chat, TZ: "UTC"}
	if err := b.closeExpiredSeason(ch); err != nil {
		t.Fatalf("closeExpiredSeason: %v", err)
	}

	got, err := st.ActiveSeason(chat)
	if err != nil {
		t.Fatalf("ActiveSeason after: %v", err)
	}
	if got.ID != se.ID {
		t.Fatalf("below-threshold leader must not close the season: got id %d, want %d", got.ID, se.ID)
	}
	if got.Deadline.String == staleDeadline {
		t.Errorf("deadline was not extended, still %q", got.Deadline.String)
	}
}
