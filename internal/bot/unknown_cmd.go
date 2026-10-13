package bot

import (
	"fmt"
	"regexp"
	"strings"
)

// leadingCmdRx matches a command at the very start of a message, the same shape
// telebot routes on: "/cmd", "/cmd@bot", optionally followed by a payload.
var leadingCmdRx = regexp.MustCompile(`^/(\w+)(?:@(\w+))?(?:\s|$)`)

// mentionCmdRx matches "@bot /cmd": a command typed after the bot mention, which
// Telegram sends as plain text.
var mentionCmdRx = regexp.MustCompile(`^@(\w+)\s+/\w+`)

// textHint says what a plain text message earns, if anything.
type textHint int

const (
	hintNone      textHint = iota // not a command attempt: stay silent
	hintUnknown                   // starts with an unregistered /command
	hintMentioned                 // "@bot /start": the command was typed after the mention
)

// classifyText decides whether a text message deserves a reply. Only a leading
// "/cmd" or "@thisbot /cmd" does; links, "1/2" and a slash in the middle of a
// sentence are ignored. cmd is the lowercased command name for hintUnknown.
func classifyText(text, botUsername string) (hint textHint, cmd string) {
	text = strings.TrimSpace(text)
	if m := leadingCmdRx.FindStringSubmatch(text); m != nil {
		if m[2] != "" && !strings.EqualFold(m[2], botUsername) {
			return hintNone, "" // addressed to another bot
		}
		return hintUnknown, strings.ToLower(m[1])
	}
	if m := mentionCmdRx.FindStringSubmatch(text); m != nil && strings.EqualFold(m[1], botUsername) {
		return hintMentioned, ""
	}
	return hintNone, ""
}

// suggestCommand returns the known command closest to typed, or "" when nothing
// is close enough to be a plausible typo.
func suggestCommand(typed string, known []string) string {
	typed = strings.ToLower(typed)
	n := len([]rune(typed))
	maxDist := 1
	switch {
	case n > 8:
		maxDist = 3
	case n > 4:
		maxDist = 2
	}
	best, bestDist, bestPrefix := "", maxDist+1, -1
	for _, k := range known {
		d := editDistance(typed, k)
		if d > maxDist || d >= n || d > bestDist {
			continue
		}
		// On a tie the command sharing a longer start wins ("seting" -> settings).
		if p := commonPrefix(typed, k); d < bestDist || p > bestPrefix {
			best, bestDist, bestPrefix = k, d, p
		}
	}
	return best
}

func commonPrefix(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i := 0
	for i < len(ra) && i < len(rb) && ra[i] == rb[i] {
		i++
	}
	return i
}

// editDistance is the plain Levenshtein distance between two strings.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

const mentionCmdHint = "Команды пиши с «/» в начале и без упоминания, например /start или /help."

// restoreHintText answers a plain-text /restore: it works only as a caption.
const restoreHintText = "/restore работает только с файлом: пришли файл бэкапа с подписью /restore."

// unknownCommandText is the reply to a mistyped command.
func unknownCommandText(typed, suggestion string) string {
	if strings.EqualFold(typed, "restore") { // not a route: onDocument reads the caption
		return restoreHintText
	}
	if r := []rune(typed); len(r) > 32 {
		typed = string(r[:32])
	}
	if suggestion == "" {
		return fmt.Sprintf("Не знаю /%s. Все команды: /help", esc(typed))
	}
	return fmt.Sprintf("Не знаю /%s. Похоже на /%s. Все команды: /help", esc(typed), suggestion)
}
