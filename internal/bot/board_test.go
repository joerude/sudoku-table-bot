package bot

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// easyGrid needs only singles (the usdoku easy board from the solver tests).
var easyGrid, _ = domain.ParsePuzzle(strings.ReplaceAll(
	"...69...3..518...47.....8.93.8.......9..4..8.......2.15.3.....88...691..4...38...", ".", "0"))

func TestBoardPhotoGradesOnlyFinishedGames(t *testing.T) {
	g := &storage.Game{Status: "pending", UsdokuCode: sql.NullString{String: "QWER", Valid: true},
		Difficulty: sql.NullString{String: "easy", Valid: true}}
	photo, kb, err := boardPhoto(g, easyGrid)
	if err != nil {
		t.Fatal(err)
	}
	if photo.Caption != "🧩 QWER · Easy" || kb != nil {
		t.Errorf("live game: caption %q, kb %v; want no board line (it is a hint)", photo.Caption, kb)
	}

	g.Status = "completed"
	photo, _, err = boardPhoto(g, easyGrid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(photo.Caption, "🧠 Доска: хватало одиночек") {
		t.Errorf("finished game: caption %q, want the board line", photo.Caption)
	}
}
