package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featWeekBoards adds the week's boards to the weekly digest: how many needed
// more than singles, which techniques, and the hardest board with its winner.
// The digest then carries a button that explains the hardest technique (the
// same ephemeral /learn card as under a result).
const featWeekBoards = "weekboards"

// weekBoardsMin is how many graded boards the block needs: one or two boards
// are already described under their own results.
const weekBoardsMin = 3

// gradedBoard is one finished game's board, graded with the /learn techniques.
type gradedBoard struct {
	Code    string
	Winner  string
	Grading domain.Grading
}

// weekBoardsBlock renders the digest's boards section and returns the hardest
// board (nil when singles were enough everywhere). Each board counts once, by
// the hardest technique it needed; a board the solver could not finish ranks
// above every technique of the dictionary.
func weekBoardsBlock(boards []gradedBoard) (string, *gradedBoard) {
	if len(boards) < weekBoardsMin {
		return "", nil
	}
	techs := domain.Techniques()
	level := make(map[string]int, len(techs))
	for i, t := range techs {
		level[t.Key] = i
	}
	beyond := len(techs) // the level of a board the dictionary does not solve

	counts := make([]int, len(techs)+1)
	easy, top := 0, -1
	var hardest *gradedBoard
	for i := range boards {
		g := boards[i].Grading
		key := g.Hardest()
		lv, known := level[key]
		switch {
		case !g.Solved:
			lv = beyond
		case !known || singles[key]:
			easy++
			continue
		}
		counts[lv]++
		if lv >= top { // on a tie the later board: the freshest in memory
			top, hardest = lv, &boards[i]
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n\n🧠 <b>Доски недели</b>: %d", len(boards))
	switch {
	case easy == len(boards):
		b.WriteString(", всем хватило одиночек")
		return b.String(), nil
	case easy > 0:
		fmt.Fprintf(&b, ", на %d хватило одиночек", easy)
	}
	var parts []string
	for lv, n := range counts {
		if n == 0 {
			continue
		}
		name := "вне справочника /learn"
		if lv < len(techs) {
			name = techs[lv].Name
		}
		parts = append(parts, fmt.Sprintf("%s ×%d", name, n))
	}
	var about []string
	if hardest.Code != "" {
		about = append(about, esc(hardest.Code))
	}
	if hardest.Winner != "" {
		about = append(about, "победа: "+esc(hardest.Winner))
	}
	if len(about) > 0 {
		parts[len(parts)-1] += " (" + strings.Join(about, ", ") + ")"
	}
	b.WriteString("\nСложнее: " + strings.Join(parts, " · "))
	return b.String(), hardest
}

// weekBoards is the digest's boards section for the games since `since`, with
// the keyboard that explains the hardest technique (nil when there is none).
// Best-effort: "" when switched off, on an error or with too few boards.
func (b *Bot) weekBoards(chatID int64, season *storage.Season, since time.Time) (string, *tele.ReplyMarkup) {
	if !b.featureOn(chatID, featWeekBoards) {
		return "", nil
	}
	rows, err := b.st.BoardsSince(chatID, season.ID, fmtDBTime(since))
	if err != nil {
		log.Printf("weekBoards %d: %v", chatID, err)
		return "", nil
	}
	var boards []gradedBoard
	for _, r := range rows {
		if grid, ok := domain.ParsePuzzle(r.Puzzle); ok {
			boards = append(boards, gradedBoard{Code: r.Code, Winner: r.Winner, Grading: domain.GradePuzzle(grid)})
		}
	}
	text, hardest := weekBoardsBlock(boards)
	if hardest == nil {
		return text, nil
	}
	btn, ok := techButton(hardest.Grading)
	if !ok {
		return text, nil
	}
	kb := &tele.ReplyMarkup{}
	kb.Inline(kb.Row(btn))
	return text, kb
}
