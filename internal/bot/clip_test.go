package bot

import (
	"strconv"
	"strings"
	"testing"
)

func TestTgLenCountsUTF16(t *testing.T) {
	if got := tgLen("abc"); got != 3 {
		t.Errorf("ascii: %d", got)
	}
	if got := tgLen("судоку"); got != 6 {
		t.Errorf("cyrillic: %d", got)
	}
	if got := tgLen("🥇"); got != 2 { // astral emoji = surrogate pair
		t.Errorf("emoji: %d, want 2", got)
	}
}

func TestClipItemsFits(t *testing.T) {
	got := clipItems("head\n", []string{"a\n", "b\n"}, 100)
	if got != "head\na\nb\n" {
		t.Errorf("untouched text changed: %q", got)
	}
	if got := clipItems("head\n", nil, 100); got != "head\n" {
		t.Errorf("no items: %q", got)
	}
}

func TestClipItemsDropsFromTheEnd(t *testing.T) {
	items := make([]string, 10)
	for i := range items {
		items[i] = strings.Repeat("x", 9) + "\n" // 10 units each
	}
	got := clipItems("H\n", items, 50)
	if tgLen(got) > 50 {
		t.Errorf("clipped text is %d units, limit 50:\n%s", tgLen(got), got)
	}
	if !strings.HasPrefix(got, "H\n") {
		t.Errorf("head lost: %q", got)
	}
	kept := strings.Count(got, "xxxxxxxxx\n")
	if kept == 0 || kept >= 10 {
		t.Fatalf("kept %d items, want some but not all", kept)
	}
	wantTail := "…и ещё " + strconv.Itoa(10-kept)
	if !strings.HasSuffix(got, wantTail) {
		t.Errorf("tail = %q, want suffix %q", got, wantTail)
	}
}

func TestClipItemsHeadAlone(t *testing.T) {
	got := clipItems("H\n", []string{strings.Repeat("y", 100) + "\n"}, 30)
	if got != "H\n…и ещё 1" {
		t.Errorf("got %q", got)
	}
}

func TestClipTextBelowLimit(t *testing.T) {
	if got := clipText("short\ntext", 100); got != "short\ntext" {
		t.Errorf("got %q", got)
	}
}

func TestClipTextCutsAtLineBreak(t *testing.T) {
	s := strings.Repeat("0123456789\n", 20) // 220 units
	got := clipText(s, 100)
	if tgLen(got) > 100 {
		t.Errorf("clipped text is %d units, limit 100", tgLen(got))
	}
	if !strings.HasSuffix(got, "\n…") {
		t.Errorf("want closing ellipsis: %q", got[len(got)-10:])
	}
	for _, ln := range strings.Split(strings.TrimSuffix(got, "\n…"), "\n") {
		if ln != "0123456789" {
			t.Errorf("a line was cut mid-way: %q", ln)
		}
	}
}
