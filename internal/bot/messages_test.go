package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func season1() *storage.Season {
	return &storage.Season{ID: 1, Number: 1, Target: 30}
}

func TestMeTextWithTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 10, Wins: 3, Points: 12, Rank: 2}
	sp := &storage.SpeedStat{AvgSecs: 270, BestSecs: 192, Games: 7}
	out := meText("Alice", st, sp, 0, 0, nil, domain.Streak{}, season1())
	for _, want := range []string{"4:30", "3:12", "по 7", "⚡ Лучшее"} {
		if !strings.Contains(out, want) {
			t.Errorf("meText with timed games: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestMeTextNoTimedGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 1, Points: 6, Rank: 3}
	sp := &storage.SpeedStat{} // Games == 0
	out := meText("Bob", st, sp, 0, 0, nil, domain.Streak{}, season1())
	if !strings.Contains(out, "Ср. время: —") {
		t.Errorf("meText no timed games: want 'Ср. время: —' in output\ngot: %s", out)
	}
	if strings.Contains(out, "Лучшее") {
		t.Errorf("meText no timed games: should NOT contain 'Лучшее'\ngot: %s", out)
	}
}

func TestMeTextZeroGames(t *testing.T) {
	st := &storage.PlayerStat{Games: 0}
	out := meText("Carol", st, nil, 0, 0, nil, domain.Streak{}, season1())
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
	out := meText("Dave", st, nil, 3, 1, nil, domain.Streak{}, season1())
	if !strings.Contains(out, "Дуэли:") {
		t.Errorf("meText zero games with duels: want 'Дуэли:' in output\ngot: %s", out)
	}
	if !strings.Contains(out, "3–1") {
		t.Errorf("meText zero games with duels: want '3–1' in output\ngot: %s", out)
	}
	// Games==0 with no duels: must NOT show duel line
	outNoDuel := meText("Dave", st, nil, 0, 0, nil, domain.Streak{}, season1())
	if strings.Contains(outNoDuel, "Дуэли") {
		t.Errorf("meText zero games no duels: should NOT contain 'Дуэли'\ngot: %s", outNoDuel)
	}
}

func TestMeTextDuelLine(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 2, Points: 8, Rank: 1}
	sp := &storage.SpeedStat{}
	out := meText("Vasya", st, sp, 4, 2, nil, domain.Streak{}, season1())
	if !strings.Contains(out, "Дуэли:") || !strings.Contains(out, "4–2") {
		t.Errorf("meText duel line: want 'Дуэли:' and '4–2' in output\ngot: %s", out)
	}
	outNoDuel := meText("Vasya", st, sp, 0, 0, nil, domain.Streak{}, season1())
	if strings.Contains(outNoDuel, "Дуэли") {
		t.Errorf("meText no duel: should NOT contain 'Дуэли'\ngot: %s", outNoDuel)
	}
}

func TestMeTextDuelDetails(t *testing.T) {
	st := &storage.PlayerStat{Games: 5, Wins: 2, Points: 8, Rank: 1}
	duelSp := &storage.SpeedStat{AvgSecs: 240, BestSecs: 180, Games: 4}
	out := meText("Nur", st, nil, 6, 2, duelSp, domain.Streak{Current: 3, Best: 5}, season1())
	for _, want := range []string{"В дуэлях", "4:00", "3:00", "Серия дуэлей: <b>3</b>", "лучшая <b>5</b>"} {
		if !strings.Contains(out, want) {
			t.Errorf("meText duel details: want %q in output\ngot: %s", want, out)
		}
	}
	// No timed duels and a short streak → both extra lines omitted.
	out2 := meText("Nur", st, nil, 6, 2, &storage.SpeedStat{}, domain.Streak{Current: 1, Best: 1}, season1())
	if strings.Contains(out2, "В дуэлях") {
		t.Errorf("meText: should omit duel time when no timed duels\ngot: %s", out2)
	}
	if strings.Contains(out2, "Серия дуэлей") {
		t.Errorf("meText: should omit streak when best<2\ngot: %s", out2)
	}
}

func TestDuelResultText(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "Vasya", Duration: 252},
		{Rank: 2, Name: "Petya"},
	}
	out := duelResultText(rows, 4, 2, true, 0)
	for _, want := range []string{"Vasya", "побеждает", "Petya", "4:12", "H2H", "4–2"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelResultText: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestDuelResultTextShowsLoserTime(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "Vasya", Duration: 264}, // 4:24
		{Rank: 2, Name: "Petya", Duration: 279}, // 4:39
	}
	out := duelResultText(rows, 0, 0, false, 0)
	for _, want := range []string{"4:24", "vs 4:39", "+0:15"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelResultText loser time: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestDuelResultTextHidesLoserTimeWhenUnknown(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "Vasya", Duration: 264},
		{Rank: 2, Name: "Petya"}, // Duration 0: manual entry, no solve time
	}
	out := duelResultText(rows, 0, 0, false, 0)
	if strings.Contains(out, "vs") {
		t.Errorf("duelResultText: loser has no duration, must not show 'vs'\ngot: %s", out)
	}
}

func TestDuelResultTextUpset(t *testing.T) {
	rows := []storage.ResultRow{
		{Rank: 1, Name: "Vasya", Duration: 252},
		{Rank: 2, Name: "Petya"},
	}
	out := duelResultText(rows, 1, 0, true, 35)
	for _, want := range []string{"😮 Апсет: фаворит пал", "<b>Vasya</b> берёт +35"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelResultText upset: want %q in output\ngot: %s", want, out)
		}
	}
	if strings.Contains(out, "Рейтинг") || strings.Contains(out, "обыграл фаворита") {
		t.Errorf("duelResultText: no own rating line / masculine wording\ngot: %s", out)
	}
	if plain := duelResultText(rows, 1, 0, true, 0); strings.Contains(plain, "Апсет") {
		t.Errorf("duelResultText upset=0: should NOT contain 'Апсет'\ngot: %s", plain)
	}
}

func TestDuelResultTextEmpty(t *testing.T) {
	out := duelResultText(nil, 0, 0, false, 0)
	if !strings.Contains(out, "Никто не финишировал") {
		t.Errorf("duelResultText empty: want 'Никто не финишировал' in output\ngot: %s", out)
	}
}

func TestDuelsText(t *testing.T) {
	rows := []storage.DuelStanding{
		{Name: "Vasya", Wins: 8, Losses: 2},
		{Name: "Masha", Wins: 5, Losses: 5},
	}
	out := duelsText(rows, nil, nil, nil, nil, nil, "Asia/Bishkek")
	for _, want := range []string{"🥇", "Vasya", "8-2", "(80%)", "Masha", "(50%)"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestDuelsTextEmpty(t *testing.T) {
	out := duelsText(nil, nil, nil, nil, nil, nil, "Asia/Bishkek")
	if !strings.Contains(out, "Ещё не было дуэлей") {
		t.Errorf("duelsText empty: want 'Ещё не было дуэлей' in output\ngot: %s", out)
	}
}

func TestDuelsTextWithRecent(t *testing.T) {
	rows := []storage.DuelStanding{
		{Name: "Vasya", Wins: 3, Losses: 1},
	}
	recent := []storage.DuelMatch{
		// 12:00 UTC -> Asia/Bishkek (UTC+6) = 18:00, same date.
		{CompletedAt: "2026-06-22 12:00:00", Winner: "Vasya", Loser: "Petya"},
	}
	out := duelsText(rows, nil, nil, nil, recent, nil, "Asia/Bishkek")
	for _, want := range []string{"Последние дуэли", "2026-06-22 18:00", "Vasya", "обыграл", "Petya"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText with recent: want %q in output\ngot: %s", want, out)
		}
	}
}

// TestDuelsTextNewBlocks: head-to-head, duel solve-time, and win-streak marker.
func TestDuelsTextNewBlocks(t *testing.T) {
	rows := []storage.DuelStanding{
		{PlayerID: 1, Name: "Vasya", Wins: 5, Losses: 1},
		{PlayerID: 2, Name: "Masha", Wins: 1, Losses: 5},
	}
	h2h := []storage.H2HPair{{AID: 1, BID: 2, AName: "Vasya", BName: "Masha", AWins: 4, BWins: 1}}
	speed := []storage.SpeedRow{{Name: "Vasya", AvgSecs: 200, BestSecs: 150, Games: 6}}
	streaks := map[int64]domain.Streak{1: {Current: 3, Best: 3}, 2: {Current: 0, Best: 1}}
	out := duelsText(rows, h2h, speed, streaks, nil, nil, "Asia/Bishkek")
	for _, want := range []string{"🔥3", "Личные встречи", "4–1", "Время в дуэлях", "3:20", "2:30"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText new blocks: want %q in output\ngot: %s", want, out)
		}
	}
}

// TestDuelsTextChallenges: the «Вызовы» block shows issued/received and the
// decline count only when non-zero, with no badge on the top decliner.
func TestDuelsTextChallenges(t *testing.T) {
	rows := []storage.DuelStanding{{PlayerID: 1, Name: "Vasya", Wins: 1, Losses: 0}}
	ch := []storage.ChallengeStat{
		{PlayerID: 1, Name: "Vasya", Issued: 7, Received: 3},
		{PlayerID: 2, Name: "Masha", Issued: 3, Received: 7, Declined: 4},
	}
	out := duelsText(rows, nil, nil, nil, nil, ch, "Asia/Bishkek")
	for _, want := range []string{"Вызовы", "бросил <b>7</b>", "получил <b>3</b>",
		"отказов <b>4</b>"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText challenges: want %q in output\ngot: %s", want, out)
		}
	}
	vasyaLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "бросил <b>7</b>") {
			vasyaLine = line
		}
	}
	if strings.Contains(vasyaLine, "отказов") {
		t.Errorf("Vasya (0 declines): line must have no отказов, got %q", vasyaLine)
	}
	if strings.Contains(out, "🙈") {
		t.Errorf("no icon on the top decliner:\n%s", out)
	}
}

// TestDuelsTextChallengesOnly: no finished duels yet but challenges exist —
// the panel must show the block instead of the empty-state message.
func TestDuelsTextChallengesOnly(t *testing.T) {
	ch := []storage.ChallengeStat{{PlayerID: 2, Name: "Masha", Issued: 2, Received: 0}}
	out := duelsText(nil, nil, nil, nil, nil, ch, "Asia/Bishkek")
	if strings.Contains(out, "Ещё не было дуэлей") {
		t.Errorf("challenges exist — empty-state message must not show:\n%s", out)
	}
	if !strings.Contains(out, "Вызовы") {
		t.Errorf("want Вызовы block:\n%s", out)
	}
}

func TestHistoryTextShowsLocalDateAndTime(t *testing.T) {
	games := []storage.HistoryGame{
		// 14:37 UTC -> Asia/Bishkek (UTC+6) = 20:37, same date.
		{ID: 1, CompletedAt: "2026-06-28 14:37:00", Difficulty: "medium",
			Order: []string{"Nur", "Joe Rude", "mister"}},
	}
	out := historyText(games, "Asia/Bishkek")
	for _, want := range []string{"2026-06-28 20:37", "Medium", "Nur", "Joe Rude", "mister"} {
		if !strings.Contains(out, want) {
			t.Errorf("historyText: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestHistoryTextSeasonSeparators(t *testing.T) {
	games := []storage.HistoryGame{ // newest first, spanning two seasons
		{ID: 3, SeasonNumber: 5, CompletedAt: "2026-07-08 10:39:00", Order: []string{"Joe Rude"}},
		{ID: 2, SeasonNumber: 5, CompletedAt: "2026-07-08 06:47:00", Order: []string{"mister"}},
		{ID: 1, SeasonNumber: 4, CompletedAt: "2026-07-07 14:05:00", Order: []string{"Nur"}},
	}
	out := historyText(games, "UTC")
	for _, want := range []string{"— Сезон 5 —", "— Сезон 4 —"} {
		if !strings.Contains(out, want) {
			t.Errorf("historyText: want separator %q\ngot: %s", want, out)
		}
	}
	if n := strings.Count(out, "— Сезон 5 —"); n != 1 {
		t.Errorf("season 5 separator should appear once, got %d\n%s", n, out)
	}
	if strings.Index(out, "— Сезон 5 —") > strings.Index(out, "— Сезон 4 —") {
		t.Errorf("season 5 block should come first (newest first)\n%s", out)
	}
}

func TestHistoryTextBadTimeFallsBack(t *testing.T) {
	games := []storage.HistoryGame{
		{ID: 1, CompletedAt: "garbage", Order: []string{"Nur"}},
	}
	out := historyText(games, "Asia/Bishkek")
	if !strings.Contains(out, "garbage") {
		t.Errorf("historyText bad time: want raw 'garbage' fallback\ngot: %s", out)
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
	diff, id, owner := parseDuelPick("medium:42:777")
	if diff != "medium" || id != 42 || owner != 777 {
		t.Errorf("parseDuelPick valid: want (medium, 42, 777), got (%s, %d, %d)", diff, id, owner)
	}
	diff, id, owner = parseDuelPick("medium:42")
	if diff != "medium" || id != 42 || owner != 0 {
		t.Errorf("parseDuelPick legacy: want (medium, 42, 0), got (%s, %d, %d)", diff, id, owner)
	}
	diff, id, owner = parseDuelPick("bad")
	if diff != "" || id != 0 || owner != 0 {
		t.Errorf("parseDuelPick invalid: want (\"\", 0, 0), got (%s, %d, %d)", diff, id, owner)
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
	out := resultText(rows, "medium", "hardcore", nil, 0, 0, 0)

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
	out := duelResultText(rows, 0, 0, false, 0)
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
	out := duelResultText(rows, 0, 0, false, 0)
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

func TestStatsKeyboardMarksActive(t *testing.T) {
	m := statsKeyboard("me", nil, 0)
	var flat []tele.InlineButton
	for _, row := range m.InlineKeyboard {
		flat = append(flat, row...)
	}
	if len(flat) != 7 {
		t.Fatalf("want 7 tab buttons, got %d", len(flat))
	}
	ids := map[string]string{} // data -> text
	for _, btn := range flat {
		if btn.Unique != cbStatsTab {
			t.Errorf("button %q has unique %q, want %q", btn.Text, btn.Unique, cbStatsTab)
		}
		ids[btn.Data] = btn.Text
	}
	for _, want := range []string{"table", "me", "speed", "duels", "history", "rating", "activity"} {
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

func TestDuelChallengeText(t *testing.T) {
	target := storage.Player{Name: "Petya", TgID: sql.NullInt64{Int64: 7, Valid: true}}
	out := duelChallengeText("Vasya", target, "medium", "TRPK", false, 0, 0, 0, 0)
	for _, want := range []string{"Vasya", "tg://user?id=7", "usdoku.com/TRPK", "Medium"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelChallengeText: want %q in output\ngot: %s", want, out)
		}
	}
	if strings.Contains(out, "⚠️") {
		t.Errorf("duelChallengeText nickWarn=false: should NOT contain warning\ngot: %s", out)
	}
	if strings.Contains(out, "Победа даст") {
		t.Errorf("duelChallengeText elo=0: should NOT contain stakes\ngot: %s", out)
	}

	outWarn := duelChallengeText("Vasya", target, "medium", "TRPK", true, 0, 0, 0, 0)
	if !strings.Contains(outWarn, "setnick") {
		t.Errorf("duelChallengeText nickWarn=true: want 'setnick' in output\ngot: %s", outWarn)
	}
}

func TestDuelChallengeTextStakes(t *testing.T) {
	target := storage.Player{Name: "Petya"}
	out := duelChallengeText("Vasya", target, "medium", "", false, 1032, 968, 26, 38)
	for _, want := range []string{"⚖️ Рейтинг: Vasya 1032, Petya 968", "Победа даст Vasya +26, Petya +38"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelChallengeText stakes: want %q in output\ngot: %s", want, out)
		}
	}
}

func TestDuelReplyTexts(t *testing.T) {
	ch := storage.Player{Name: "Vasya", TgID: sql.NullInt64{Int64: 5, Valid: true}}
	acc := duelAcceptReplyText(ch)
	for _, want := range []string{"Вызов принят", "tg://user?id=5", "Поехали"} {
		if !strings.Contains(acc, want) {
			t.Errorf("accept reply: want %q in %q", want, acc)
		}
	}
	dec := duelDeclineReplyText(ch)
	for _, want := range []string{"Вызов отклонён", "tg://user?id=5"} {
		if !strings.Contains(dec, want) {
			t.Errorf("decline reply: want %q in %q", want, dec)
		}
	}
}

func TestDuelPostTexts(t *testing.T) {
	target := storage.Player{Name: "Petya", TgID: sql.NullInt64{Int64: 7, Valid: true}}
	if out := duelAcceptText(target, "ABCD"); !strings.Contains(out, "Вызов принят") ||
		!strings.Contains(out, "usdoku.com/ABCD") || strings.Contains(out, "принял") {
		t.Errorf("duelAcceptText: %s", out)
	}
	if out := duelDeclineText(target); !strings.Contains(out, "Вызов отклонён") || strings.Contains(out, "отказался") {
		t.Errorf("duelDeclineText: %s", out)
	}
}

func TestDuelExpiredText(t *testing.T) {
	got := duelExpiredText("Vasya", "Pe<t>ya")
	for _, want := range []string{"⌛", "Vasya", "Pe&lt;t&gt;ya", "→", "сгорел", "лига свободна"} {
		if !strings.Contains(got, want) {
			t.Errorf("duelExpiredText: want %q in %q", want, got)
		}
	}
}

func TestDuelChallengeTextShowsTTL(t *testing.T) {
	out := duelChallengeText("Vasya", storage.Player{Name: "Petya"}, "medium", "", false, 0, 0, 0, 0)
	if !strings.Contains(out, "Сгорит через 20 мин, если не принять") {
		t.Errorf("challenge must show the expiry line\ngot: %s", out)
	}
}

func TestMilestoneLines(t *testing.T) {
	if !gamesMilestones[250] || gamesMilestones[100] {
		t.Errorf("gamesMilestones: 250 must be in, 100 must be out (badge covers it)")
	}
	if !winsMilestones[100] || winsMilestones[50] {
		t.Errorf("winsMilestones: 100 must be in, 50 must be out (badge covers it)")
	}
	for _, s := range []string{
		milestoneGamesLine("Joe<b>", 250),
		milestoneWinsLine("Joe<b>", 100),
	} {
		if !strings.Contains(s, "Joe&lt;b&gt;") || !strings.Contains(s, "№") {
			t.Errorf("milestone line malformed: %q", s)
		}
	}
	if s := milestoneLeagueLine(500); !strings.Contains(s, "500") || !strings.Contains(s, "лиги") {
		t.Errorf("league milestone malformed: %q", s)
	}
}

func TestStatsKeyboardSpeedToggle(t *testing.T) {
	find := func(m *tele.ReplyMarkup, data string) bool {
		for _, row := range m.InlineKeyboard {
			for _, btn := range row {
				if btn.Data == data {
					return true
				}
			}
		}
		return false
	}
	season := statsKeyboard("speed", nil, 0)
	if !find(season, "speed_all") {
		t.Errorf("speed tab: want all-seasons toggle button")
	}
	all := statsKeyboard("speed_all", nil, 0)
	if !find(all, "speed") {
		t.Errorf("speed_all: want back-to-season toggle button")
	}
	// speed_all must highlight the speed tab.
	marked := false
	for _, row := range all.InlineKeyboard {
		for _, btn := range row {
			if btn.Data == "speed" && strings.Contains(btn.Text, "•") {
				marked = true
			}
		}
	}
	if !marked {
		t.Errorf("speed_all: speed tab must carry the active marker")
	}
	// The table tab: 7 tab buttons in 2 rows (4 + 3) plus the chart row.
	if n := len(statsKeyboard("table", nil, 4).InlineKeyboard); n != 3 {
		t.Errorf("table tab without archive: want 3 rows, got %d", n)
	}
	// Table tab with archived seasons carries the season row above the chart row.
	withSeasons := statsKeyboard("table", []int{1, 2, 3}, 4)
	if n := len(withSeasons.InlineKeyboard); n != 4 {
		t.Fatalf("table tab with archive: want 4 rows, got %d", n)
	}
	seasonRow := withSeasons.InlineKeyboard[2]
	if len(seasonRow) != 4 {
		t.Fatalf("season row: want 4 buttons, got %d", len(seasonRow))
	}
	if seasonRow[3].Text != "▶ С4" || seasonRow[3].Unique != cbSeasonView || seasonRow[3].Data != "4" {
		t.Errorf("active season button wrong: %+v", seasonRow[3])
	}
	if seasonRow[0].Text != "С1" || seasonRow[0].Data != "1" {
		t.Errorf("first season button wrong: %+v", seasonRow[0])
	}
}

func TestHelpTextLeadsWithPlayAndStats(t *testing.T) {
	for _, want := range []string{"/play", "/stats"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("helpText missing %q", want)
		}
	}
}

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

func TestRecordAndClaimKeyboardSkipsOverlongNicks(t *testing.T) {
	long := strings.Repeat("я", 40) // ~80 bytes → payload exceeds 64
	m := recordAndClaimKeyboard(42, []string{"joe", long})
	var data []string
	for _, row := range m.InlineKeyboard {
		for _, btn := range row {
			if btn.Unique != cbClaimNick {
				continue // the «Записать результат» button
			}
			if len(btn.Data) > 64 {
				t.Errorf("callback_data exceeds 64 bytes: %d", len(btn.Data))
			}
			data = append(data, btn.Data)
		}
	}
	if len(data) != 1 || data[0] != "42:joe" {
		t.Fatalf("want only [42:joe], got %v", data)
	}
}

func TestRecordsTextRendersHoldersAndEmpty(t *testing.T) {
	rows := []storage.RecordRow{
		{Difficulty: "easy", Secs: 72, Name: "Ann"},
		{Difficulty: "medium", Secs: 118, Name: "Joe<b>"},
	}
	out := recordsText(rows, nil)
	for _, want := range []string{"1:12", "Ann", "1:58", "Easy", "Medium", "Joe&lt;b&gt;"} {
		if !strings.Contains(out, want) {
			t.Errorf("recordsText: want %q in\n%s", want, out)
		}
	}
	if empty := recordsText(nil, nil); !strings.Contains(empty, "Пока нет") {
		t.Errorf("empty recordsText should hint, got: %s", empty)
	}
	// Titles section renders when only titles exist (no time records).
	titles := recordsText(nil, []storage.TitleRow{{Name: "Bob<b>", Count: 3}})
	for _, want := range []string{"Титулы", "Bob&lt;b&gt;", "×3"} {
		if !strings.Contains(titles, want) {
			t.Errorf("recordsText titles: want %q in\n%s", want, titles)
		}
	}
}

func TestBadgeCollectionText(t *testing.T) {
	// Two championships, 34 wins (💪 earned, 💯 locked), no timed solves.
	in := domain.BadgeInput{Wins: 34, Games: 120, BestSecs: 0, WinStreak: 3, DayStreak: 2, SeasonsWon: 2}
	out := badgeCollectionText(domain.BadgeProgress(in))
	for _, want := range []string{
		"🏆", "Бейджи", "(4/7)", // header with earned count
		"чемпион сезона ×2",        // 🏅 folds in the title count
		"💯 50 побед — 34/50",       // locked count badge shows progress
		"🔒 ⚡ решение быстрее 2:00", // locked speed badge, no best time
		"📅 7 дней подряд — 2/7",    // locked streak badge
	} {
		if !strings.Contains(out, want) {
			t.Errorf("badgeCollectionText: want %q in\n%s", want, out)
		}
	}
	// A locked speed badge with a recorded (but slow) best shows the time.
	slow := badgeCollectionText(domain.BadgeProgress(domain.BadgeInput{BestSecs: 134}))
	if !strings.Contains(slow, "🔒 ⚡ решение быстрее 2:00 — лучшее 2:14") {
		t.Errorf("speed badge best-time hint missing:\n%s", slow)
	}
	// A single championship shows the label without a ×N suffix.
	one := badgeCollectionText(domain.BadgeProgress(domain.BadgeInput{SeasonsWon: 1}))
	if strings.Contains(one, "чемпион сезона ×") {
		t.Errorf("single title should not show ×N:\n%s", one)
	}
}

func TestSpeedTextAllSeasons(t *testing.T) {
	ranked := []storage.SpeedRow{{Name: "Nur", AvgSecs: 300, BestSecs: 228, Games: 40}}
	out := speedText(nil, "medium", ranked, nil, 3)
	if !strings.Contains(out, "все сезоны") {
		t.Errorf("speedText all: want 'все сезоны' in header\ngot: %s", out)
	}
	if strings.Contains(out, "сезон ") {
		t.Errorf("speedText all: must not mention a season number\ngot: %s", out)
	}
}

func TestRecordAndClaimKeyboardOneClaimPerNick(t *testing.T) {
	m := recordAndClaimKeyboard(42, []string{"joe", "max"})
	var data []string
	for _, row := range m.InlineKeyboard {
		for _, btn := range row {
			if btn.Unique != cbClaimNick {
				continue // the «Записать результат» button
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

func TestRatingDeltaLines(t *testing.T) {
	names := map[int64]string{10: "Alice", 20: "Bob"}
	gr := domain.GameRating{
		GameID:      1,
		Delta:       map[int64]int{10: 14, 20: -9},
		NewRating:   map[int64]int{10: 1042, 20: 988},
		CrownBefore: 20,
		CrownAfter:  10,
	}
	got := ratingDeltaLines(gr, names)
	if !strings.Contains(got, "Alice +14 → 1042") {
		t.Errorf("missing winner line: %q", got)
	}
	if !strings.Contains(got, "Bob -9 → 988") {
		t.Errorf("missing loser line: %q", got)
	}
	if !strings.Contains(got, "👑") || !strings.Contains(got, "Alice") {
		t.Errorf("missing crown change: %q", got)
	}

	zero := domain.GameRating{
		GameID:    2,
		Delta:     map[int64]int{10: 0},
		NewRating: map[int64]int{10: 1000},
	}
	if got := ratingDeltaLines(zero, map[int64]string{10: "Alice"}); strings.Contains(got, "+0") {
		t.Errorf("zero delta must not render as +0: %q", got)
	}
}

func TestCrownChangeLineNoChange(t *testing.T) {
	names := map[int64]string{10: "Alice"}
	gr := domain.GameRating{CrownBefore: 10, CrownAfter: 10}
	if got := crownChangeLine(gr, names); got != "" {
		t.Errorf("expected empty when crown unchanged, got %q", got)
	}
}

func TestCrownChangeLineFirstCrown(t *testing.T) {
	names := map[int64]string{10: "Alice"}
	gr := domain.GameRating{CrownBefore: 0, CrownAfter: 10}
	got := crownChangeLine(gr, names)
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "👑") {
		t.Errorf("first crown line wrong: %q", got)
	}
}

func TestRatingLadder(t *testing.T) {
	names := map[int64]string{10: "Alice", 20: "Bob"}
	r := domain.Ratings{
		Crown: 10,
		Ladder: []domain.PlayerRating{
			{PlayerID: 10, Rating: 1042, Peak: 1042, Games: 12, Provisional: false},
			{PlayerID: 20, Rating: 988, Peak: 1010, Games: 4, Provisional: true},
		},
	}
	got := ratingLadder(r, names)
	if !strings.Contains(got, "Alice") || !strings.Contains(got, "1042") {
		t.Errorf("missing leader: %q", got)
	}
	if !strings.Contains(got, "👑") {
		t.Errorf("missing crown on leader: %q", got)
	}
	if !strings.Contains(got, "калибр") {
		t.Errorf("missing provisional marker on Bob: %q", got)
	}
}

func TestRatingLadderEmpty(t *testing.T) {
	got := ratingLadder(domain.Ratings{}, map[int64]string{})
	if !strings.Contains(got, "нет") {
		t.Errorf("empty ladder should say there are no games: %q", got)
	}
}

func TestDigestText(t *testing.T) {
	se := &storage.Season{Number: 3, Target: 100}
	top := []storage.Standing{
		{Name: "Ann", Points: 24, Wins: 8}, {Name: "Joe", Points: 15, Wins: 5}, {Name: "Max", Points: 9, Wins: 3},
	}
	fastest := &storage.RecordRow{Difficulty: "medium", Secs: 118, Name: "Joe"}
	out := digestText(se, top, fastest, "Ann", 4, 11)
	for _, want := range []string{"Сезон 3", "Ann", "Joe", "Max", "1:58", "🔥", "Ann", "11"} {
		if !strings.Contains(out, want) {
			t.Errorf("digestText: want %q in\n%s", want, out)
		}
	}
}

func TestDuelsTextShowsScore(t *testing.T) {
	rows := []storage.DuelStanding{
		{Name: "Vasya", Wins: 8, Losses: 2},
		{Name: "Masha", Wins: 5, Losses: 5},
	}
	out := duelsText(rows, nil, nil, nil, nil, nil, "Asia/Bishkek")
	for _, want := range []string{"Дуэли</b> · счёт", "8-2", "(80%)"} {
		if !strings.Contains(out, want) {
			t.Errorf("duelsText score: want %q in output\ngot: %s", want, out)
		}
	}
	if strings.Contains(out, "рейтинг") {
		t.Errorf("duelsText must not advertise a separate duel rating\ngot: %s", out)
	}
}

func TestMeTextShowsProgressBar(t *testing.T) {
	st := &storage.PlayerStat{Games: 10, Wins: 3, Points: 15, Rank: 2}
	out := meText("Alice", st, &storage.SpeedStat{}, 0, 0, nil, domain.Streak{}, season1())
	if !strings.Contains(out, "▰") || !strings.Contains(out, "▱") {
		t.Errorf("meText: want progress bar in output\ngot: %s", out)
	}
}

func TestStandingsMoves(t *testing.T) {
	before := []storage.Standing{
		{PlayerID: 1, Name: "Ann"}, {PlayerID: 2, Name: "Joe"}, {PlayerID: 3, Name: "Max"},
	}
	after := []storage.Standing{
		{PlayerID: 2, Name: "Joe"}, {PlayerID: 1, Name: "Ann"}, {PlayerID: 3, Name: "Max"},
	}
	moves := standingsMoves(before, after)
	if len(moves) != 2 {
		t.Fatalf("want 2 moves, got %d: %+v", len(moves), moves)
	}
	got := map[string]int{}
	for _, m := range moves {
		got[m.Name] = m.Delta
	}
	if got["Joe"] != 1 || got["Ann"] != -1 {
		t.Errorf("want Joe +1, Ann -1, got %v", got)
	}
	// No changes → no moves; a new player is not a move.
	if m := standingsMoves(before, before); len(m) != 0 {
		t.Errorf("identical standings: want no moves, got %+v", m)
	}
	withNew := append([]storage.Standing{{PlayerID: 9, Name: "New"}}, before...)
	for _, m := range standingsMoves(before, withNew) {
		if m.Name == "New" {
			t.Errorf("new player must not be a move: %+v", m)
		}
	}
}

func TestMovesLine(t *testing.T) {
	out := movesLine([]standingsMove{{Name: "Ann", Delta: -1}, {Name: "Joe", Delta: 1}})
	for _, want := range []string{"↗️", "Joe", "+1", "↘️", "Ann", "-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("movesLine: want %q in %q", want, out)
		}
	}
	// Climbers render before fallers.
	if strings.Index(out, "Joe") > strings.Index(out, "Ann") {
		t.Errorf("movesLine: climber should come first: %q", out)
	}
	if movesLine(nil) != "" {
		t.Errorf("movesLine(nil) should be empty")
	}
}

func TestNewBadges(t *testing.T) {
	got := newBadges([]string{"🔥"}, []string{"🔥", "💪"})
	if len(got) != 1 || got[0] != "💪" {
		t.Errorf("want [💪], got %v", got)
	}
	if got := newBadges(nil, nil); len(got) != 0 {
		t.Errorf("want none, got %v", got)
	}
}

func TestCelebrationLines(t *testing.T) {
	pb := personalBestLine("Joe<b>", "medium", 181, 192)
	for _, want := range []string{"🚀", "Joe&lt;b&gt;", "Medium", "3:01", "3:12"} {
		if !strings.Contains(pb, want) {
			t.Errorf("personalBestLine: want %q in %q", want, pb)
		}
	}
	ws := winStreakLine("Ann", 4)
	for _, want := range []string{"🔥", "Ann", "4"} {
		if !strings.Contains(ws, want) {
			t.Errorf("winStreakLine: want %q in %q", want, ws)
		}
	}
	bl := badgeLine("Ann", []string{"💪"})
	for _, want := range []string{"🎖", "Ann", "💪", "10 побед"} {
		if !strings.Contains(bl, want) {
			t.Errorf("badgeLine: want %q in %q", want, bl)
		}
	}
}

func TestSeasonEndTextWithAwards(t *testing.T) {
	awards := []string{awardWinsLine("Ann", 9), awardFastestLine("Joe", 118, "medium")}
	out := seasonEndText(3, "Ann", 101, awards, 100, 4)
	for _, want := range []string{"Сезон 3 завершён", "Ann", "101", "Номинации",
		"Больше всех побед", "1:58", "сезон 4", "100"} {
		if !strings.Contains(out, want) {
			t.Errorf("seasonEndText awards: want %q in output\ngot: %s", want, out)
		}
	}
	// No awards → no nominations block.
	if plain := seasonEndText(3, "Ann", 101, nil, 100, 4); strings.Contains(plain, "Номинации") {
		t.Errorf("seasonEndText no awards: should NOT contain 'Номинации'\ngot: %s", plain)
	}
}

func TestRemoveOneToday(t *testing.T) {
	dates := []string{"2026-07-05", "2026-07-04"}
	got := removeOneToday(dates, "2026-07-05")
	if len(got) != 1 || got[0] != "2026-07-04" {
		t.Errorf("single today: want [2026-07-04], got %v", got)
	}
	// today twice → unchanged (the set still contains today before the game).
	twice := []string{"2026-07-05", "2026-07-05", "2026-07-04"}
	if got := removeOneToday(twice, "2026-07-05"); len(got) != 3 {
		t.Errorf("double today: want unchanged, got %v", got)
	}
}

func TestSeasonSummaryTextArchived(t *testing.T) {
	se := &storage.Season{Number: 2, Target: 100, Status: "archived"}
	rows := []storage.Standing{
		{PlayerID: 1, Name: "Nur", Points: 100, Wins: 27, Games: 58},
		{PlayerID: 2, Name: "mister", Points: 80, Wins: 22, Games: 57},
		{PlayerID: 3, Name: "Later Joiner", Points: 0, Wins: 0, Games: 0},
	}
	awards := []string{awardWinsLine("Nur", 27)}
	out := seasonSummaryText(se, rows, 65, "2025-11-17", "2025-12-02", "Nur", awards)
	for _, want := range []string{"Сезон 2", "завершён", "2025-11-17", "2025-12-02",
		"игр: <b>65</b>", "👑", "Nur", "🥇", "🥈", "Номинации", "Больше всех побед"} {
		if !strings.Contains(out, want) {
			t.Errorf("seasonSummaryText: want %q in output\ngot: %s", want, out)
		}
	}
	// Zero-game players stay hidden; an active season says "идёт".
	if strings.Contains(out, "Later Joiner") {
		t.Errorf("seasonSummaryText: zero-game player must be hidden\ngot: %s", out)
	}
	se.Status = "active"
	if live := seasonSummaryText(se, rows, 65, "", "", "", nil); !strings.Contains(live, "идёт") {
		t.Errorf("seasonSummaryText active: want 'идёт'\ngot: %s", live)
	}
}

func TestArchiveHintAndNoSuchSeason(t *testing.T) {
	if h := archiveHint([]int{1, 2, 3}); !strings.Contains(h, "/season 1…3") {
		t.Errorf("archiveHint: want range, got %q", h)
	}
	if h := archiveHint(nil); h != "" {
		t.Errorf("archiveHint empty: want \"\", got %q", h)
	}
	if s := noSuchSeasonText(7, []int{1, 3}); !strings.Contains(s, "1…3") {
		t.Errorf("noSuchSeasonText: want range, got %q", s)
	}
}

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

func TestPendingConflictText(t *testing.T) {
	g := &storage.Game{
		ID:         42,
		Difficulty: sql.NullString{String: "medium", Valid: true},
		Mode:       sql.NullString{String: "hardcore", Valid: true},
		UsdokuCode: sql.NullString{String: "ABCD", Valid: true},
		CreatedBy:  sql.NullInt64{Int64: 5, Valid: true},
		CreatedAt:  "2026-07-08 10:00:00",
	}
	text := pendingConflictText(g, "Nur", "UTC")
	for _, want := range []string{
		"незакрытая игра", "#42", "Medium", "Hardcore",
		"2026-07-08 10:00", "Nur", "usdoku.com/ABCD",
		"запиши её результат или отмени",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}

	// Bare game (manual /result leftover): no difficulty, code, or creator.
	bare := pendingConflictText(&storage.Game{ID: 7, CreatedAt: "2026-07-08 10:00:00"}, "", "UTC")
	if strings.Contains(bare, "usdoku.com") || strings.Contains(bare, "·  ·") {
		t.Errorf("bare game should omit empty metadata:\n%s", bare)
	}
	if !strings.Contains(bare, "#7") {
		t.Errorf("bare game should still show id:\n%s", bare)
	}
}

func TestFmtDBTime(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	got := fmtDBTime(time.Date(2026, 8, 1, 0, 0, 0, 0, loc))
	if want := "2026-07-31 18:00:00"; got != want {
		t.Errorf("fmtDBTime = %q, want %q", got, want)
	}
}

func TestSeasonDeadlineEndText(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	next := time.Date(2026, 9, 1, 0, 0, 0, 0, loc).UTC()
	got := seasonDeadlineEndText(5, "Nur", 42, []string{"🏅 award"}, 6, 100, next, loc)

	for _, want := range []string{
		"Сезон 5 завершён",
		"по календарю",
		"<b>Nur</b>",
		"42",
		"🏅 award",
		"сезон 6",
		"100",
		"31 августа",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("seasonDeadlineEndText missing %q in:\n%s", want, got)
		}
	}
}

func TestSeasonDeadlineEndTextNoAwards(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	next := time.Date(2026, 9, 1, 0, 0, 0, 0, loc).UTC()
	got := seasonDeadlineEndText(1, "Joe <b>", 3, nil, 2, 100, next, loc)
	if strings.Contains(got, "Номинации") {
		t.Errorf("no awards, but the awards block rendered:\n%s", got)
	}
	if !strings.Contains(got, "Joe &lt;b&gt;") {
		t.Errorf("winner name not escaped:\n%s", got)
	}
}

func TestSeasonDeadlineLine(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		Number:   5,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true}, // 1 Aug 00:00 +06
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, loc)
	got := seasonDeadlineLine(se, now, loc)
	if !strings.Contains(got, "31 июля") {
		t.Errorf("line = %q, want the last playable day (31 июля)", got)
	}
	if !strings.Contains(got, "11 дн") {
		t.Errorf("line = %q, want 11 days left", got)
	}
}

func TestSeasonDeadlineLineLastDay(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		Number:   5,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true},
	}
	now := time.Date(2026, 7, 31, 20, 0, 0, 0, loc)
	got := seasonDeadlineLine(se, now, loc)
	if !strings.Contains(got, "сегодня") {
		t.Errorf("line = %q, want it to say the season ends today", got)
	}
}

// TestSeasonDeadlineLineJustAfterDeadline covers the truncate-vs-floor bug:
// now is a few seconds past the stored deadline, so the true day count is a
// small negative fraction (e.g. -30s / 24h ≈ -0.00035). Truncating toward
// zero collapses that to 0 ("сегодня!"), which is wrong — the last playable
// day is already over. Flooring correctly lands in the days<0 branch.
func TestSeasonDeadlineLineJustAfterDeadline(t *testing.T) {
	loc := time.FixedZone("UTC+6", 6*3600)
	se := &storage.Season{
		Number:   5,
		Deadline: sql.NullString{String: "2026-07-31 18:00:00", Valid: true}, // 1 Aug 00:00 +06
	}
	// 30 seconds after the stored deadline instant.
	now := time.Date(2026, 7, 31, 18, 0, 30, 0, time.UTC)
	got := seasonDeadlineLine(se, now, loc)
	if strings.Contains(got, "сегодня") {
		t.Errorf("line = %q, must NOT say сегодня — the last playable day has passed", got)
	}
	if !strings.Contains(got, "31 июля") {
		t.Errorf("line = %q, want the past date (31 июля)", got)
	}
}

func TestSeasonDeadlineLineUnset(t *testing.T) {
	se := &storage.Season{Number: 5}
	if got := seasonDeadlineLine(se, time.Now(), time.UTC); got != "" {
		t.Errorf("no deadline: line = %q, want empty", got)
	}
}

func TestActivityText(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	rows := []storage.InitiativeRow{
		{Name: "Joe Rude", GamesAll: 47, DuelsAll: 39, Games30: 12, Duels30: 8,
			LastPlayed: "2026-07-20 08:00:00"},
		{Name: "mister", GamesAll: 14, DuelsAll: 7, Games30: 0, Duels30: 0,
			LastPlayed: "2026-07-14 08:00:00"},
		{Name: "Ghost", LastPlayed: ""},
	}
	got := activityText(rows, now, time.UTC)

	for _, want := range []string{
		"Актив",
		"Joe Rude",
		"47", "39", "12",
		"сегодня",
		"mister",
		"6 дн",
		"Ghost",
		"ещё не играл",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("activityText missing %q in:\n%s", want, got)
		}
	}
}

func TestActivityTextEmpty(t *testing.T) {
	got := activityText(nil, time.Now(), time.UTC)
	if !strings.Contains(got, "/join") {
		t.Errorf("empty roster should hint at /join, got:\n%s", got)
	}
}

func TestEvenGames(t *testing.T) {
	rows := []storage.Standing{
		{PlayerID: 1, Name: "Ann", Games: 41},
		{PlayerID: 2, Name: "Joe", Games: 36},
		{PlayerID: 3, Name: "Max", Games: 0},
	}
	got := evenGames(rows, 42)
	if got[0].Games != 42 || got[1].Games != 42 {
		t.Errorf("players with games must show the season total, got %+v", got)
	}
	if got[2].Games != 0 {
		t.Errorf("a player with no games stays at 0, got %d", got[2].Games)
	}
	if rows[1].Games != 36 {
		t.Errorf("input must stay untouched, got %d", rows[1].Games)
	}
	if low := evenGames(rows, 10); low[0].Games != 41 {
		t.Errorf("a total below the player's own count must not lower it, got %d", low[0].Games)
	}
}

func TestStandingsTextRaceTable(t *testing.T) {
	se := &storage.Season{Number: 6, Target: 100}
	rows := []storage.Standing{
		{PlayerID: 1, Name: "mister", Points: 20, Wins: 6},
		{PlayerID: 2, Name: "Nur", Points: 18, Wins: 5},
		{PlayerID: 3, Name: "Max", Points: 15, Wins: 4},
	}
	// At the window start: Nur 17, mister 15, Max 15. mister climbed, Nur dropped.
	race := raceWindow{Days: 7, Games: 4, Gains: map[int64]int{1: 5, 2: 1}}
	out := standingsText(se, rows, race, map[int64][]int{1: {1, 2, 3, 1, 1, 0}})
	for _, want := range []string{
		"гонка до 100 очков",
		"<pre>1 mister 20 ▰▰▱▱▱▱▱▱▱▱ +5 ↗1\n",
		"2 Nur    18 ▰▰▱▱▱▱▱▱▱▱ +1 ↘1\n",
		"3 Max    15 ▰▰▱▱▱▱▱▱▱▱ +0</pre>",
		"за 7 дней · 4 игры",
		"<b>mister</b> · 6 поб · 🥈🥉🥇🥇➖",
		"<b>Max</b> · 4 поб",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("standingsText missing %q\ngot: %s", want, out)
		}
	}
}

func TestStandingsTextQuietWeek(t *testing.T) {
	se := &storage.Season{Number: 6, Target: 100}
	rows := []storage.Standing{{PlayerID: 1, Name: "A&B", Points: 100}, {PlayerID: 2, Name: "Александр Великий", Points: 99}}
	out := standingsText(se, rows, raceWindow{Days: 7}, nil)
	if strings.Contains(out, "+0") || strings.Contains(out, "за 7 дней") {
		t.Errorf("no games in the window: no window columns or legend\ngot: %s", out)
	}
	if strings.Contains(out, "Форма") {
		t.Errorf("no ranks: no form block\ngot: %s", out)
	}
	if !strings.Contains(out, "A&amp;B") || !strings.Contains(out, "Александр…") {
		t.Errorf("names must be escaped and clipped\ngot: %s", out)
	}
	// Only the target fills the bar: 99 stays one cell short.
	if !strings.Contains(out, "100 ▰▰▰▰▰▰▰▰▰▰") || !strings.Contains(out, " 99 ▰▰▰▰▰▰▰▰▰▱") {
		t.Errorf("bars: full only at the target\ngot: %s", out)
	}
}

func TestBoardCaption(t *testing.T) {
	if got := boardCaption("QWER", "easy", "original"); got != "🧩 QWER · Easy · Original" {
		t.Errorf("caption = %q", got)
	}
	if got := boardCaption("QWER", "", ""); got != "🧩 QWER" {
		t.Errorf("caption without tag = %q", got)
	}
}

func TestBoardLine(t *testing.T) {
	cases := []struct {
		g    domain.Grading
		want string
	}{
		{domain.Grading{Used: map[string]int{"hidden-single": 30, "obvious-single": 20}, Solved: true}, "🧠 Доска: хватало одиночек"},
		{domain.Grading{Used: map[string]int{"hidden-single": 3, "x-wing": 1, "pointing-pair": 2}, Solved: true},
			"🧠 Доска: понадобились Указывающая пара, X-Wing"},
		{domain.Grading{Used: map[string]int{"hidden-single": 3}}, "🧠 Доска: нужны техники сложнее справочника /learn"},
		{domain.Grading{Used: map[string]int{"swordfish": 1}}, "🧠 Доска: Swordfish и что-то сложнее справочника /learn"},
	}
	for _, c := range cases {
		if got := boardLine(c.g); got != c.want {
			t.Errorf("boardLine(%v) = %q, want %q", c.g.Used, got, c.want)
		}
	}
}

func TestTechButtonOnlyBeyondSingles(t *testing.T) {
	if _, ok := techButton(domain.Grading{Used: map[string]int{"hidden-single": 9}, Solved: true}); ok {
		t.Error("singles only: no button")
	}
	btn, ok := techButton(domain.Grading{Used: map[string]int{"hidden-single": 9, "y-wing": 1, "pointing-pair": 1}, Solved: true})
	if !ok || btn.Unique != cbBoardTech || btn.Data != "y-wing" || btn.Text != "📚 Y-Wing" {
		t.Errorf("button = %+v, %v; want the hardest (Y-Wing)", btn, ok)
	}
}

func TestHistoryTextShowsRoomCode(t *testing.T) {
	games := []storage.HistoryGame{{ID: 1, SeasonNumber: 2, CompletedAt: "2026-10-01 10:00:00",
		Difficulty: "easy", Order: []string{"Аня"}, UsdokuCode: "QWER"}}
	if out := historyText(games, "UTC"); !strings.Contains(out, "Easy · QWER:") {
		t.Errorf("room code missing: %q", out)
	}
}
