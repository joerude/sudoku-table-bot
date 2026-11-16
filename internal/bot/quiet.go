package bot

import tele "gopkg.in/telebot.v3"

// withOptions returns opts plus add as a fresh slice, so a caller's variadic
// slice is never written through.
func withOptions(opts []interface{}, add ...interface{}) []interface{} {
	out := make([]interface{}, 0, len(opts)+len(add))
	out = append(out, opts...)
	return append(out, add...)
}

// quiet posts a service message (a confirmation, a settings view, a roster)
// without a notification: nobody's phone buzzes for it. For other zones the
// hookup is one line: replace c.Send(text, kb) with b.quiet(c, text, kb).
func (b *Bot) quiet(c tele.Context, what interface{}, opts ...interface{}) error {
	return c.Send(what, withOptions(opts, tele.Silent)...)
}

// quietReply are the send options of a silent answer to msg, for posts the bot
// makes on its own (no tele.Context). A *SendOptions replaces telebot's
// defaults wholesale, the bot's parse mode included, so HTML is set here.
// A nil or deleted msg sends the post without the reply.
func quietReply(msg *tele.Message) *tele.SendOptions {
	return &tele.SendOptions{ReplyTo: msg, ParseMode: tele.ModeHTML,
		DisableNotification: true, AllowWithoutReply: true}
}
