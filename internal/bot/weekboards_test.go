package bot

import (
	"strings"
	"testing"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

func TestWeekBoardsBlock(t *testing.T) {
	solved := func(used map[string]int) domain.Grading { return domain.Grading{Used: used, Solved: true} }
	boards := []gradedBoard{
		{Code: "AAAA", Winner: "Bob", Grading: solved(map[string]int{"obvious-single": 40, "hidden-single": 9})},
		{Code: "BBBB", Winner: "Bob", Grading: solved(map[string]int{"hidden-single": 30, "obvious-pair": 2})},
		{Code: "QWER", Winner: "Alice <3", Grading: solved(map[string]int{"obvious-pair": 1, "x-wing": 1})},
		{Code: "CCCC", Winner: "Kate", Grading: solved(map[string]int{"obvious-single": 50})},
	}
	text, hardest := weekBoardsBlock(boards)
	want := "\n\n🧠 <b>Доски недели</b>: 4, на 2 хватило одиночек\n" +
		"Сложнее: Голая пара ×1 · X-Wing ×1 (QWER, победа: Alice &lt;3)"
	if text != want {
		t.Errorf("block =\n%q\nwant\n%q", text, want)
	}
	if hardest == nil || hardest.Code != "QWER" {
		t.Errorf("hardest = %+v, want the X-Wing board", hardest)
	}

	// A board the solver cannot finish outranks the whole dictionary.
	boards = append(boards, gradedBoard{Code: "ZZZZ", Grading: domain.Grading{Used: map[string]int{"x-wing": 1}}})
	text, hardest = weekBoardsBlock(boards)
	if hardest.Code != "ZZZZ" || text[len(text)-len("вне справочника /learn ×1 (ZZZZ)"):] != "вне справочника /learn ×1 (ZZZZ)" {
		t.Errorf("unsolved board must close the list:\n%s", text)
	}

	if text, hardest := weekBoardsBlock(boards[:2]); text != "" || hardest != nil {
		t.Errorf("two boards are too few for a block: %q", text)
	}
	easy := []gradedBoard{boards[0], boards[3], boards[0]}
	if text, hardest := weekBoardsBlock(easy); text != "\n\n🧠 <b>Доски недели</b>: 3, всем хватило одиночек" || hardest != nil {
		t.Errorf("all singles: %q, %v", text, hardest)
	}
}

func TestDigestCarriesWeekBoards(t *testing.T) {
	const chat = int64(-7951)
	b, st := titlesChat(t, chat) // five season games played just now, ids 1..5
	const puzzle = "100000569492056108056109240009640801064010000218035604040500016905061402621000005"
	for id := int64(1); id <= 3; id++ {
		if err := st.SetPuzzle(id, puzzle); err != nil {
			t.Fatal(err)
		}
	}
	text, kb, hasGames, err := b.buildWeeklyDigest(chat, false)
	if err != nil || !hasGames {
		t.Fatalf("digest: %v %v", hasGames, err)
	}
	// Only the three games with a stored grid count as boards; the grid is an
	// X-Wing board and Alice won every game.
	if !strings.Contains(text, "🧠 <b>Доски недели</b>: 3\nСложнее: X-Wing ×3 (") ||
		!strings.Contains(text, "победа: Alice)") {
		t.Errorf("digest misses the week's boards:\n%s", text)
	}
	if kb == nil || len(kb.InlineKeyboard) != 1 || !strings.Contains(kb.InlineKeyboard[0][0].Text, "X-Wing") {
		t.Errorf("the digest must carry the hardest technique's button: %+v", kb)
	}
	if err := st.SetFeature(chat, featWeekBoards, false); err != nil {
		t.Fatal(err)
	}
	if text, _, _, _ := b.buildWeeklyDigest(chat, false); strings.Contains(text, "Доски недели") {
		t.Errorf("switched off, the digest must not list boards:\n%s", text)
	}
}
