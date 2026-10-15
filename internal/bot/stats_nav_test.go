package bot

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"
)

func flatButtons(m *tele.ReplyMarkup) []tele.InlineButton {
	var out []tele.InlineButton
	for _, row := range m.InlineKeyboard {
		out = append(out, row...)
	}
	return out
}

func TestStatsTabsLayoutAndEmoji(t *testing.T) {
	m := statsKeyboard("table", nil, 1)
	if len(m.InlineKeyboard) != 3 || len(m.InlineKeyboard[0]) != 4 || len(m.InlineKeyboard[1]) != 3 {
		t.Fatalf("tab layout: want rows of 4 and 3 plus the chart row, got %d rows", len(m.InlineKeyboard))
	}
	if chart := m.InlineKeyboard[2]; len(chart) != 1 || chart[0].Unique != cbRaceChart {
		t.Errorf("table tab: last row must be the race chart button, got %+v", chart)
	}
	text := map[string]string{}
	for _, b := range flatButtons(m) {
		text[b.Data] = b.Text
	}
	if _, ok := text["records"]; ok {
		t.Errorf("records tab must be merged into Speed")
	}
	if !strings.Contains(text["activity"], "📣") || strings.Contains(text["activity"], "🎯") {
		t.Errorf("activity tab emoji: %q", text["activity"])
	}
	if !strings.Contains(text["rating"], "Рейтинг") {
		t.Errorf("rating tab missing: %v", text)
	}
}

func TestRecentSeasonStripAndArchive(t *testing.T) {
	// Six seasons: the last four plus «Архив».
	m := statsKeyboard("table", []int{1, 2, 3, 4, 5}, 6)
	if len(m.InlineKeyboard) != 5 {
		t.Fatalf("want 2 tab rows + strip + archive row + chart row, got %d rows", len(m.InlineKeyboard))
	}
	strip := m.InlineKeyboard[2]
	var got []string
	for _, b := range strip {
		got = append(got, b.Text)
	}
	if want := "С3 С4 С5 ▶ С6"; strings.Join(got, " ") != want {
		t.Errorf("season strip = %q, want %q", strings.Join(got, " "), want)
	}
	arch := m.InlineKeyboard[3]
	if len(arch) != 1 || arch[0].Data != seasonArchive || arch[0].Unique != cbSeasonView || !strings.Contains(arch[0].Text, "Архив") {
		t.Errorf("archive button: %+v", arch)
	}

	// Four seasons or fewer: nothing hidden, no archive button.
	few := statsKeyboard("table", []int{1, 2, 3}, 4)
	for _, b := range flatButtons(few) {
		if b.Data == seasonArchive {
			t.Errorf("archive button shown with nothing hidden")
		}
	}
}

func TestSeasonViewKeyboardNeighboursAndBack(t *testing.T) {
	archived, active := []int{1, 2, 3}, 4
	back := func(m *tele.ReplyMarkup) tele.InlineButton {
		rows := m.InlineKeyboard
		return rows[len(rows)-1][0]
	}

	mid := seasonViewKeyboard(archived, active, 2)
	if len(mid.InlineKeyboard) != 2 {
		t.Fatalf("middle season: want arrows + back, got %d rows", len(mid.InlineKeyboard))
	}
	arrows := mid.InlineKeyboard[0]
	if len(arrows) != 2 || arrows[0].Data != "1" || arrows[1].Data != "3" ||
		!strings.Contains(arrows[0].Text, "◀") || !strings.Contains(arrows[1].Text, "▶") {
		t.Errorf("arrows: %+v", arrows)
	}
	if b := back(mid); b.Unique != cbStatsTab || b.Data != "table" || !strings.Contains(b.Text, "К статистике") {
		t.Errorf("back button: %+v", b)
	}

	first := seasonViewKeyboard(archived, active, 1)
	if a := first.InlineKeyboard[0]; len(a) != 1 || a[0].Data != "2" {
		t.Errorf("first season: only a forward arrow expected: %+v", a)
	}
	last := seasonViewKeyboard(archived, active, 4)
	if a := last.InlineKeyboard[0]; len(a) != 1 || a[0].Data != "3" {
		t.Errorf("last season: only a back arrow expected: %+v", a)
	}
	solo := seasonViewKeyboard(nil, 1, 1)
	if len(solo.InlineKeyboard) != 1 || back(solo).Data != "table" {
		t.Errorf("single season: back row only, got %+v", solo.InlineKeyboard)
	}
}

func TestArchiveKeyboardListsAllSeasonsWithBack(t *testing.T) {
	m := archiveKeyboard([]int{1, 2, 3, 4, 5}, 6)
	var nums int
	for _, b := range flatButtons(m) {
		if b.Unique == cbSeasonView {
			nums++
		}
	}
	if nums != 6 {
		t.Errorf("archive lists %d season buttons, want 6", nums)
	}
	rows := m.InlineKeyboard
	if last := rows[len(rows)-1][0]; last.Unique != cbStatsTab || last.Data != "table" {
		t.Errorf("archive back button: %+v", last)
	}
}

func TestIsNotModified(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{tele.ErrSameMessageContent, true},
		{tele.ErrMessageNotModified, true},
		{fmt.Errorf("edit: %w", tele.ErrSameMessageContent), true},
		{errors.New("telegram: Bad Request: message is not modified: specified new message content"), true},
		{errors.New("telegram: message to edit not found"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := isNotModified(tc.err); got != tc.want {
			t.Errorf("isNotModified(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
