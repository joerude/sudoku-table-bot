package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func TestSeriesFormatPayloads(t *testing.T) {
	if got := parseBestOf([]string{"hard", "BO5"}); got != 5 {
		t.Errorf("parseBestOf(bo5) = %d", got)
	}
	if got := parseBestOf([]string{"hard", "bo4", "duel"}); got != 0 {
		t.Errorf("parseBestOf(bo4) = %d, want 0", got)
	}
	// The format step's buttons are picks with the format filled in.
	kb := duelFormatKeyboard("hard", 4, 777)
	bo3 := kb.InlineKeyboard[1][0]
	if bo3.Unique != cbDuelPick || bo3.Data != "hard:4:777:3" {
		t.Fatalf("bo3 button = %s|%s, want duelpick|hard:4:777:3", bo3.Unique, bo3.Data)
	}
	if d, id, owner := parseDuelPick("hard:4:777:3"); d != "hard" || id != 4 || owner != 777 {
		t.Errorf("parseDuelPick with format = (%s, %d, %d)", d, id, owner)
	}
	if duelPickBestOf("hard:4:777:3") != 3 || duelPickBestOf("hard:4:777") != 0 {
		t.Error("duelPickBestOf: want 3 with a format, 0 without")
	}
	// A series rematch keeps the format; a single one stays 3 parts.
	btn := rematchButtonBo("hard", 3, 8, 5)
	if d, a, b := parseRematch(btn.Data); d != "hard" || a != 3 || b != 8 || rematchBestOf(btn.Data) != 5 {
		t.Errorf("series rematch %q parsed as (%s, %d, %d, bo%d)", btn.Data, d, a, b, rematchBestOf(btn.Data))
	}
	if rematchBestOf(rematchButton("hard", 3, 8).Data) != 1 {
		t.Error("single rematch must stay bo1")
	}
}

func TestBuildSeriesViewAsOfGame(t *testing.T) {
	se := &storage.Series{AID: 1, BID: 2, BestOf: 3}
	games := []storage.SeriesGame{
		{GameID: 10, Status: "completed", WinnerID: 1},
		{GameID: 11, Status: "completed", WinnerID: 0}, // nobody finished
		{GameID: 12, Status: "completed", WinnerID: 2},
		{GameID: 13, Status: "completed", WinnerID: 1},
		{GameID: 14, Status: "pending"},
	}
	names := map[int64]string{1: "Арсен", 2: "Тимур"}

	v, ok := buildSeriesView(se, games, 12, names)
	if !ok || v.GameNo != 3 || v.AWins != 1 || v.BWins != 1 || v.Done {
		t.Fatalf("as of game 12: %+v", v)
	}
	v, ok = buildSeriesView(se, games, 13, names)
	if !ok || v.GameNo != 4 || v.AWins != 2 || !v.Done || len(v.Winners) != 4 || v.Winners[1] != "" {
		t.Fatalf("as of game 13: %+v", v)
	}
	if _, ok := buildSeriesView(se, games, 14, names); ok {
		t.Error("a pending game has no series view")
	}
}

func TestSeriesResultText(t *testing.T) {
	rows := []storage.ResultRow{
		{PlayerID: 1, Name: "Арсен", Rank: 1, Duration: 432},
		{PlayerID: 2, Name: "Тимур", Rank: 2, Duration: 481},
	}
	mid := seriesResultText(rows, seriesView{BestOf: 5, GameNo: 2, AName: "Арсен", BName: "Тимур", AWins: 2}, 0, "")
	for _, want := range []string{"Серия до 3 побед</b> · партия 2", "🏆 Победа: <b>Арсен</b>",
		"Серия: <b>Арсен</b> 2:0 Тимур"} {
		if !strings.Contains(mid, want) {
			t.Errorf("mid-series: want %q\ngot: %s", want, mid)
		}
	}
	if strings.Contains(mid, "Партии:") {
		t.Errorf("mid-series must not list the games yet\ngot: %s", mid)
	}

	final := seriesResultText(rows, seriesView{BestOf: 3, GameNo: 3, AName: "Арсен", BName: "Тимур",
		AWins: 1, BWins: 2, Winners: []string{"Тимур", "Арсен", "Тимур"}, Done: true, H2H: true, HA: 1, HB: 3}, 0, "")
	for _, want := range []string{"🏆 Серия: <b>Тимур</b> 2:1 Арсен", "Партии: Тимур, Арсен, Тимур",
		"Личный счёт в сериях: <b>Тимур</b> 3:1 <b>Арсен</b>"} {
		if !strings.Contains(final, want) {
			t.Errorf("final: want %q\ngot: %s", want, final)
		}
	}

	void := seriesResultText([]storage.ResultRow{{Name: "Арсен"}, {Name: "Тимур"}},
		seriesView{BestOf: 3, GameNo: 2, AName: "Арсен", BName: "Тимур", AWins: 1}, 0, "")
	if !strings.Contains(void, "Никто не дошел до финиша. Партия не в счёт.") {
		t.Errorf("void game line missing\ngot: %s", void)
	}
}
