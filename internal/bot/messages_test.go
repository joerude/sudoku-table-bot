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
	out := meText("Alice", st, sp, season1())
	for _, want := range []string{"4:30", "3:12", "по 7", "⚡ Лучшее"} {
		if !strings.Contains(out, want) {
			t.Errorf("meText with timed games: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestMeTextNoTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 1, Points: 6, Rank: 3}
	sp := &storage.SpeedStat{} // Games == 0
	out := meText("Bob", st, sp, season1())
	if !strings.Contains(out, "Ср. время: —") {
		t.Errorf("meText no timed games: want 'Ср. время: —' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Лучшее") {
		t.Errorf("meText no timed games: should NOT contain 'Лучшее'\ngot: %s", out)
	}
}

func TestMeTextZeroGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 0}
	out := meText("Carol", st, nil, season1())
	if !strings.Contains(out, "пока нет игр") {
		t.Errorf("meText zero games: want 'пока нет игр' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Ср. время") {
		t.Errorf("meText zero games: should NOT contain 'Ср. время'\ngot: %s", out)
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
