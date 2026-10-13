package bot

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// tgMaxText is Telegram's cap on one message's text. Lengths are counted in
// UTF-16 units over the raw HTML, tags included, so the count errs on the safe
// side (Telegram does not count tags).
const tgMaxText = 4096

// tgLen is the length of s in UTF-16 code units, the unit Telegram counts.
func tgLen(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

// clipItems joins head and items (each a complete chunk, trailing newline
// included) and, when the result would exceed limit, drops items from the end
// and appends "…и ещё N" (N = number of dropped items). The head is always kept.
func clipItems(head string, items []string, limit int) string {
	var b strings.Builder
	b.WriteString(head)
	total := tgLen(head)
	for _, it := range items {
		total += tgLen(it)
	}
	if total <= limit {
		for _, it := range items {
			b.WriteString(it)
		}
		return b.String()
	}

	b.Reset()
	b.WriteString(head)
	used := tgLen(head)
	kept := 0
	for i, it := range items {
		tail := fmt.Sprintf("…и ещё %d", len(items)-i-1)
		// Keep the item only if it and a tail for what follows still fit.
		if used+tgLen(it)+tgLen(tail) > limit {
			break
		}
		b.WriteString(it)
		used += tgLen(it)
		kept = i + 1
	}
	fmt.Fprintf(&b, "…и ещё %d", len(items)-kept)
	return b.String()
}

// clipText is the last-resort guard for a whole screen: text over limit is cut
// at the last line break that leaves room for a closing "…". Screens that can
// be truncated sensibly by item should use clipItems instead.
func clipText(s string, limit int) string {
	if tgLen(s) <= limit {
		return s
	}
	const tail = "\n…"
	lines := strings.SplitAfter(s, "\n")
	var b strings.Builder
	used := tgLen(tail)
	for _, ln := range lines {
		if used+tgLen(ln) > limit {
			break
		}
		b.WriteString(ln)
		used += tgLen(ln)
	}
	return strings.TrimRight(b.String(), "\n") + tail
}
