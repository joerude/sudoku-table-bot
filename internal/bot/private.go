package bot

import (
	"slices"
	"strings"

	tele "gopkg.in/telebot.v3"
)

const (
	privateDeniedText = "Я работаю в группе: добавь меня в чат с друзьями"
	privateStartText  = "👋 Я веду учёт игр в судоку (usdoku.com) в групповом чате. " +
		"Добавь меня в чат с друзьями: там /setnick и /play.\n" +
		"Здесь можно полистать справочник техник: /learn. Все команды: /help"
)

// privateCommandSet lists the only commands that make sense without a group: the
// intro, the technique dictionary and the owner's /backup. /restore arrives as a
// document caption and checks its own rights.
var privateCommandSet = []string{"start", "help", "learn", "backup"}

// privateTextAllowed reports whether a text message sent in a private chat may
// reach its handler. Only registered commands outside privateCommandSet are
// refused; everything else (plain text, a mistyped command) passes to onText,
// which stays silent or hints at the typo.
func privateTextAllowed(text string) bool {
	m := leadingCmdRx.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return true
	}
	cmd := strings.ToLower(m[1])
	if !slices.Contains(knownCommands, cmd) {
		return true
	}
	return slices.Contains(privateCommandSet, cmd)
}

// privateCallbackAllowed: the dictionary buttons are the only ones that work
// without a group.
func privateCallbackAllowed(unique string) bool {
	switch unique {
	case cbLearnRoot, cbLearnTier, cbLearnTech, cbLearnRand:
		return true
	}
	return false
}

func isPrivate(c tele.Context) bool {
	chat := c.Chat()
	return chat != nil && chat.Type == tele.ChatPrivate
}

// privateGate keeps a private chat from becoming an empty one-person league:
// league commands and buttons there get a pointer to the group instead.
func (b *Bot) privateGate(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if !isPrivate(c) {
			return next(c)
		}
		if cb := c.Callback(); cb != nil {
			if privateCallbackAllowed(cb.Unique) {
				return next(c)
			}
			return c.Respond(&tele.CallbackResponse{Text: privateDeniedText})
		}
		if m := c.Message(); m != nil && !privateTextAllowed(m.Text) {
			return b.ephemeral(c, privateDeniedText)
		}
		return next(c)
	}
}
