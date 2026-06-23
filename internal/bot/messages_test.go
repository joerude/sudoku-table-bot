package bot

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func season1() *storage.Season {
	return &storage.Season{ID: 1, Number: 1, Target: 30}
}

func TestMeTextWithTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 10, Wins: 3, Points: 12, Rank: 2}
	sp := &storage.SpeedStat{AvgSecs: 270, BestSecs: 192, Games: 7}
	out := meText("Alice", st, sp, 0, 0, season1())
	for _, want := range []string{"4:30", "3:12", "по 7", "⚡ Лучшее"} {
		if !strings.Contains(out, want) {
			t.Errorf("meText with timed games: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestMeTextNoTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 1, Points: 6, Rank: 3}
	sp := &storage.SpeedStat{} // Games == 0
	out := meText("Bob", st, sp, 0, 0, season1())
	if !strings.Contains(out, "Ср. время: —") {
		t.Errorf("meText no timed games: want 'Ср. время: —' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Лучшее") {
		t.Errorf("meText no timed games: should NOT contain 'Лучшее'\ngot: %s", out)
	}
}

func TestMeTextZeroGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 0}
	out := meText("Carol", st, nil, 0, 0, season1())
	if !strings.Contains(out, "пока нет игр") {
		t.Errorf("meText zero games: want 'пока нет игр' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Ср. время") {
		t.Errorf("meText zero games: should NOT contain 'Ср. время'\ngot: %s", out)
	}
}

func TestMeTextZeroGamesWithDuels(t *testing.T) {
	st := &storage.PlayerStat{Games: 0}
	// Games==0 with duel record: must show duel line
	out := meText("Dave", st, nil, 3, 1, season1())
	if !strings.Contains(out, "Дуэли:") {
		t.Errorf("meText zero games with duels: want 'Дуэли:' in output\ngot: %s", out)
	}
	if !strings.Contains(out, "3–1") {
		t.Errorf("meText zero games with duels: want '3–1' in output\ngot: %s", out)
	}
	// Games==0 with no duels: must NOT show duel line
	outNoDuel := meText("Dave", st, nil, 0, 0, season1())
	if strings.Contains(outNoDuel, "Дуэли") {
		t.Errorf("meText zero games no duels: should NOT contain 'Дуэли'\ngot: %s", outNoDuel)
	}
}

func TestMeTextDuelLine(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 2, Points: 8, Rank: 1}
	sp := &storage.SpeedStat{}
	out := meText("Vasya", st, sp, 4, 2, season1())
	if !strings.Contains(out, "Дуэли:") || !strings.Contains(out, "4–2") {
		t.Errorf("meText duel line: want 'Дуэли:' and '4–2' in output\ngot: %s", out)
	}
	outNoDuel := meText("Vasya", st, sp, 0, 0, season1())
	if strings.Contains(outNoDuel, "Дуэли") {
		t.Errorf("meText no duel: should NOT contain 'Дуэли'\ngot: %s", outNoDuel)
	}
}

func TestDuelResultText(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "Vasya", Duration: 252},
		{Rank: 2, Name: "Petya"},
	}
	out := duelResultText(rows, 4, 2, true)
	for _, want := range []string{"Vasya", "побеждает", "Petya", "4:12", "H2H", "4–2"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelResultText: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestDuelResultTextEmpty(t *testing.T) {
	out := duelResultText(nil, 0, 0, false)
	if !strings.Contains(out, "Никто не финишировал") {
		t.Errorf("duelResultText empty: want 'Никто не финишировал' in output\ngot: %s", out)
	}
}

func TestDuelsText(t *testing.T) {
	rows := []storage.DuelStanding{
		{Name: "Vasya", Wins: 8, Losses: 2},
		{Name: "Masha", Wins: 5, Losses: 5},
	}
	out := duelsText(rows, nil)
	for _, want := range []string{"🥇", "Vasya", "8–2", "(80%)", "Masha", "(50%)"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestDuelsTextEmpty(t *testing.T) {
	out := duelsText(nil, nil)
	if !strings.Contains(out, "Ещё не было дуэлей") {
		t.Errorf("duelsText empty: want 'Ещё не было дуэлей' in output\ngot: %s", out)
	}
}

func TestDuelsTextWithRecent(t *testing.T) {
	rows := []storage.DuelStanding{
		{Name: "Vasya", Wins: 3, Losses: 1},
	}
	recent := []storage.DuelMatch{
		{Date: "2026-06-22", Winner: "Vasya", Loser: "Petya"},
	}
	out := duelsText(rows, recent)
	for _, want := range []string{"Последние дуэли", "2026-06-22", "Vasya", "обыграл", "Petya"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText with recent: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestSpeedTextRankedAndFewer(t *testing.T) {
	ranked := []storage.SpeedRow{
		{Name: "Bob", AvgSecs: 228, BestSecs: 181, Games: 5},
		{Name: "Alice", AvgSecs: 270, BestSecs: 192, Games: 7},
	}
	fewer := []storage.SpeedRow{
		{Name: "Carol", AvgSecs: 200, BestSecs: 200, Games: 2},
	}
	out := speedText(season1(), "medium", ranked, fewer, 3)
	checks := []string{"🥇", "3:48", "4:30", "Мало игр (&lt;3): Carol"}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("speedText ranked+fewer: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestSpeedTextEmptyRanked(t *testing.T) {
	out := speedText(season1(), "hard", nil, nil, 3)
	for _, want := range []string{"мало данных", "Hard"} {
		if !strings.Contains(out, want) {
			t.Errorf("speedText empty: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestMentionWithTgID(t *testing.T) {
	p := storage.Player{TgID: sql.NullInt64{Int64: 42, Valid: true}, Name: "Vasya"}
	want := `<a href="tg://user?id=42">Vasya</a>`
	if got := mention(p); got != want {
		t.Errorf("mention with tg id: want %q, got %q", want, got)
	}
}

func TestMentionUsernameFallback(t *testing.T) {
	p := storage.Player{Name: "Bob", Username: sql.NullString{String: "bobby", Valid: true}}
	want := "@bobby"
	if got := mention(p); got != want {
		t.Errorf("mention username fallback: want %q, got %q", want, got)
	}
}

func TestMentionPlainFallback(t *testing.T) {
	p := storage.Player{Name: "A<b>"}
	want := "A&lt;b&gt;"
	if got := mention(p); got != want {
		t.Errorf("mention plain fallback: want %q, got %q", want, got)
	}
}

func TestParseDuelPick(t *testing.T) {
	diff, id := parseDuelPick("medium:42")
	if diff != "medium" || id != 42 {
		t.Errorf("parseDuelPick valid: want (medium, 42), got (%s, %d)", diff, id)
	}
	diff, id = parseDuelPick("bad")
	if diff != "" || id != 0 {
		t.Errorf("parseDuelPick invalid: want (\"\", 0), got (%s, %d)", diff, id)
	}
}

func TestInviteTextNilRoster(t *testing.T) {
	pings := []storage.Player{{Name: "Vasya", TgID: sql.NullInt64{Int64: 7, Valid: true}}}
	out := inviteText("medium", "TRPK", pings, nil)
	for _, want := range []string{"Играем", "tg://user?id=7", "usdoku.com/TRPK", "Medium"} {
		if !strings.Contains(out, want) {
			t.Errorf("inviteText nil roster: want %q in output\ngot: %s", want, out)
		}
	}
	if strings.Contains(out, "В деле:") {
		t.Errorf("inviteText nil roster: should NOT contain 'В деле:'\ngot: %s", out)
	}
}

func TestInviteTextWithRoster(t *testing.T) {
	out := inviteText("medium", "TRPK", nil, []storage.Player{{Name: "Bob"}})
	if !strings.Contains(out, "В деле: Bob") {
		t.Errorf("inviteText with roster: want 'В деле: Bob' in output\ngot: %s", out)
	}
}

func TestResultTextWithDNF(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "A", Points: 3, Duration: 252},
		{Rank: 0, Name: "B"},
	}
	out := resultText(rows, "medium", "hardcore", nil, 0)

	// Finisher A: medal, name, points, time.
	for _, want := range []string{"🥇", "A", "4:12"} {
		if !strings.Contains(out, want) {
			t.Errorf("resultText DNF: want %q in output\ngot: %s", want, out)
		}
	}
	// DNF player B: shown with "не финишировал", no medal.
	if !strings.Contains(out, "B") {
		t.Errorf("resultText DNF: want B in output\ngot: %s", out)
	}
	if !strings.Contains(out, "не финишировал") {
		t.Errorf("resultText DNF: want 'не финишировал' in output\ngot: %s", out)
	}
	// B must NOT have a medal (🥇🥈🥉 or "0.").
	for _, bad := range []string{"🥇 <b>B", "🥈 <b>B", "🥉 <b>B", "0. <b>B"} {
		if strings.Contains(out, bad) {
			t.Errorf("resultText DNF: B must not have a medal/rank, got: %s", out)
		}
	}
}

func TestDuelResultTextAllDNF(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 0, Name: "Vasya"},
		{Rank: 0, Name: "Petya"},
	}
	out := duelResultText(rows, 0, 0, false)
	if !strings.Contains(out, "Никто не финишировал") {
		t.Errorf("duelResultText all-DNF: want 'Никто не финишировал' in output\ngot: %s", out)
	}
	if strings.Contains(out, "побеждает") {
		t.Errorf("duelResultText all-DNF: must NOT contain 'побеждает'\ngot: %s", out)
	}
}

func TestDuelResultTextWithDNF(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "Vasya", Points: 3, Duration: 180},
		{Rank: 0, Name: "Petya"},
	}
	out := duelResultText(rows, 0, 0, false)
	if !strings.Contains(out, "Vasya") || !strings.Contains(out, "побеждает") {
		t.Errorf("duelResultText DNF: want winner info\ngot: %s", out)
	}
	if !strings.Contains(out, "Petya") {
		t.Errorf("duelResultText DNF: want loser name\ngot: %s", out)
	}
	if !strings.Contains(out, "не финишировал") {
		t.Errorf("duelResultText DNF: want 'не финишировал' for loser\ngot: %s", out)
	}
	if strings.Contains(out, "проигрывает") {
		t.Errorf("duelResultText DNF: must NOT say 'проигрывает' when loser is DNF\ngot: %s", out)
	}
}

func TestDuelChallengeText(t *testing.T) {
	target := storage.Player{Name: "Petya", TgID: sql.NullInt64{Int64: 7, Valid: true}}
	out := duelChallengeText("Vasya", target, "medium", "TRPK", false)
	for _, want := range []string{"Vasya", "tg://user?id=7", "usdoku.com/TRPK", "Medium"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelChallengeText: want %q in output\ngot: %s", want, out)
		}
	}
	if strings.Contains(out, "⚠️") {
		t.Errorf("duelChallengeText nickWarn=false: should NOT contain warning\ngot: %s", out)
	}

	outWarn := duelChallengeText("Vasya", target, "medium", "TRPK", true)
	if !strings.Contains(outWarn, "setnick") {
		t.Errorf("duelChallengeText nickWarn=true: want 'setnick' in output\ngot: %s", outWarn)
	}
}
