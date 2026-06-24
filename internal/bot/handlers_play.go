package bot

import tele "gopkg.in/telebot.v3"

// onPlay posts the play hub: normal game / duel / invite.
func (b *Bot) onPlay(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onPlay.ensure", err)
	}
	return c.Send("🎮 <b>Что играем?</b>", playMenuKeyboard())
}

// onPlayGame swaps the hub for the difficulty chooser.
func (b *Bot) onPlayGame(c tele.Context) error {
	_ = c.Respond()
	return c.Edit("🧩 <b>Сложность?</b>", playDiffKeyboard())
}

// onPlayDiff creates a normal game at the chosen difficulty (default mode).
func (b *Bot) onPlayDiff(c tele.Context) error {
	diff := c.Data()
	if !validDifficulty[diff] {
		diff = "medium"
	}
	_ = c.Respond()
	return b.startNewGame(c, diff, "hardcore")
}

// onPlayDuel routes to the existing duel opponent picker.
func (b *Bot) onPlayDuel(c tele.Context) error {
	_ = c.Respond()
	return b.onDuel(c)
}

// onPlayInvite routes to the existing invite flow.
func (b *Bot) onPlayInvite(c tele.Context) error {
	_ = c.Respond()
	return b.onInvite(c)
}
