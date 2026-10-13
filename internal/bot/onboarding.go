package bot

import (
	"fmt"
	"strings"

	tele "gopkg.in/telebot.v3"
)

// joinName picks the display name for a player: an explicit name wins, then the
// Telegram first name, @username, and finally a placeholder.
func joinName(u *tele.User, explicit string) string {
	if name := strings.TrimSpace(explicit); name != "" {
		return name
	}
	if u != nil {
		if u.FirstName != "" {
			return u.FirstName
		}
		if u.Username != "" {
			return u.Username
		}
	}
	return "Игрок"
}

const (
	setNickUsage   = "Укажи свой ник из usdoku: /setnick МойНик"
	setNickSpaces  = "В нике не должно быть пробелов. Впиши ник так, как он показан в комнате usdoku: /setnick МойНик"
	setNickMatches = "Ник должен совпадать с именем в комнате usdoku, иначе я не узнаю тебя в игре."
)

// nickTakenText is the refusal for a nick another player already uses.
func nickTakenText(nick, holder string) string {
	return fmt.Sprintf("Ник <b>%s</b> уже у игрока <b>%s</b>. Если это ты, попроси его выбрать другой ник.",
		esc(nick), esc(holder))
}

// setNickDoneText confirms a saved nick; created adds a note that the player was
// registered by the same command.
func setNickDoneText(nick string, created bool) string {
	var sb strings.Builder
	if created {
		sb.WriteString("👋 Записал тебя в игроки.\n")
	}
	fmt.Fprintf(&sb, "✅ usdoku-ник: <b>%s</b> сохранён.\n<i>%s</i>", esc(nick), setNickMatches)
	return sb.String()
}
