package bot

import (
	"log"
	"sync"
	"time"

	tele "gopkg.in/telebot.v3"
)

// ephemeralTTL is how long a self-deleting service message lives.
const ephemeralTTL = 20 * time.Second

// ephemeralRoute is how a service reply reaches the user.
type ephemeralRoute int

const (
	routeLegacy   ephemeralRoute = iota // a chat message that deletes itself after ephemeralTTL
	routeCallback                       // an ephemeral answer to the button press (callback_query_id)
	routeReply                          // an ephemeral reply to an ephemeral command
	routeAdmin                          // an admin bot may send an ephemeral message any time
)

// pickEphemeralRoute chooses the route: a button press and an ephemeral command
// can always be answered ephemerally, anything else only by a chat admin bot.
// botAdmin is called only when it matters (it may ask Telegram).
func pickEphemeralRoute(callback bool, incomingEphemeralID int, botAdmin func() bool) ephemeralRoute {
	switch {
	case callback:
		return routeCallback
	case incomingEphemeralID != 0:
		return routeReply
	case botAdmin():
		return routeAdmin
	}
	return routeLegacy
}

// ephemeral sends a transient service message (error / guard / validation) that
// only its addressee sees: an ephemeral message (see pickEphemeralRoute) when
// Telegram allows one, otherwise a chat message that deletes itself after
// ephemeralTTL along with the user command that triggered it. Deleting the
// user's message needs the bot to be a group admin and is silently skipped
// otherwise. Use for noise only, never for results, stats, confirmations, or
// messages with action buttons.
func (b *Bot) ephemeral(c tele.Context, text string, opts ...interface{}) error {
	if b.sendPrivately(c, text, markupOf(opts)) {
		return nil
	}
	msg, err := b.tb.Send(c.Recipient(), text, opts...)
	if err != nil {
		return err
	}
	var userCmd *tele.Message
	if m := c.Message(); c.Callback() == nil && m != nil && m.ID != 0 { // c.Message() is the user's command, not a bot msg
		userCmd = m
	}
	b.scheduleDelete(ephemeralTTL, msg, userCmd)
	return nil
}

// sendPrivately delivers text as an ephemeral message to whoever triggered the
// update. False means it was not sent (no route, or the API refused, e.g. an
// old client or a stale callback) and the caller falls back to a chat message.
// A button press is answered with an empty toast so the text shows only once.
func (b *Bot) sendPrivately(c tele.Context, text string, markup *tele.ReplyMarkup) bool {
	chat, u := c.Chat(), realSender(c)
	if chat == nil || u == nil {
		return false
	}
	cbID, replyID := "", 0
	if cb := c.Callback(); cb != nil {
		cbID = cb.ID
	} else if m := c.Message(); m != nil {
		replyID = m.EphemeralMessageID
	}
	route := pickEphemeralRoute(cbID != "", replyID, func() bool { return b.botIsAdmin(chat) })
	if route == routeLegacy {
		return false
	}
	if _, err := b.sendEphemeral(chat.ID, u.ID, cbID, replyID, text, markup); err != nil {
		log.Printf("ephemeral send (route %d): %v", route, err)
		return false
	}
	switch route {
	case routeCallback:
		_ = c.Respond()
	case routeAdmin:
		b.scheduleDelete(ephemeralTTL, c.Message()) // the user's command
	}
	return true
}

// incomingEphemeral is the ephemeral id of the command being handled, 0 for a
// regular message or a button press.
func incomingEphemeral(c tele.Context) int {
	if c.Callback() != nil {
		return 0
	}
	if m := c.Message(); m != nil {
		return m.EphemeralMessageID
	}
	return 0
}

// privately answers an ephemeral command with an ephemeral reply. False means
// the command was a regular one or the reply failed: the caller then posts its
// usual public answer.
func (b *Bot) privately(c tele.Context, text string, markup *tele.ReplyMarkup) bool {
	if incomingEphemeral(c) == 0 {
		return false
	}
	return b.sendPrivately(c, text, markup)
}

// editCallbackMessage is c.Edit for a picker that may live in an ephemeral
// message (an ephemeral /learn): those only change through
// editEphemeralMessageText, addressed to the user who pressed the button.
func (b *Bot) editCallbackMessage(c tele.Context, text string, markup *tele.ReplyMarkup) error {
	cb, m := c.Callback(), c.Message()
	if cb != nil && m != nil && m.EphemeralMessageID != 0 && m.Chat != nil && cb.Sender != nil {
		return b.editEphemeralText(m.Chat.ID, cb.Sender.ID, m.EphemeralMessageID, text, markup)
	}
	return c.Edit(text, markup)
}

// markupOf picks the inline keyboard out of send options, if any.
func markupOf(opts []interface{}) *tele.ReplyMarkup {
	for _, o := range opts {
		if m, ok := o.(*tele.ReplyMarkup); ok {
			return m
		}
	}
	return nil
}

// botAdminTTL is how long the bot's own admin status in a chat is trusted.
const botAdminTTL = 10 * time.Minute

// botAdminCache remembers per chat whether the bot is an admin there, so a
// service reply does not cost a getChatMember call every time.
type botAdminCache struct {
	mu sync.Mutex
	m  map[int64]botAdminEntry
}

type botAdminEntry struct {
	admin bool
	at    time.Time
}

// get returns the cached answer for chatID, calling fetch when it is missing
// or older than botAdminTTL. A failed fetch counts as "not admin" and is not
// cached, so the next call asks again.
func (bc *botAdminCache) get(chatID int64, now time.Time, fetch func() (bool, error)) bool {
	bc.mu.Lock()
	e, ok := bc.m[chatID]
	bc.mu.Unlock()
	if ok && now.Sub(e.at) < botAdminTTL {
		return e.admin
	}
	admin, err := fetch()
	if err != nil {
		log.Printf("bot admin check %d: %v", chatID, err)
		return false
	}
	bc.mu.Lock()
	if bc.m == nil {
		bc.m = map[int64]botAdminEntry{}
	}
	bc.m[chatID] = botAdminEntry{admin: admin, at: now}
	bc.mu.Unlock()
	return admin
}

// botIsAdmin reports whether the bot is an admin of chat (cached). Private
// chats have no admins.
func (b *Bot) botIsAdmin(chat *tele.Chat) bool {
	if chat == nil || chat.Type == tele.ChatPrivate || b.tb == nil || b.tb.Me == nil {
		return false
	}
	return b.botAdmins.get(chat.ID, time.Now(), func() (bool, error) {
		m, err := b.tb.ChatMemberOf(chat, b.tb.Me)
		if err != nil {
			return false, err
		}
		return m.Role == tele.Creator || m.Role == tele.Administrator, nil
	})
}
