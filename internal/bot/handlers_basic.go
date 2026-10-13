package bot

import (
	"errors"
	"fmt"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

func (b *Bot) onHelp(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onHelp.ensure", err)
	}
	if isPrivate(c) {
		return c.Send(helpText) // quick buttons are group-only
	}
	return c.Send(helpText, quickMenuKeyboard())
}

// onStart greets a chat that nobody has joined yet with the full onboarding. A
// repeated /start gets a one-line pointer that deletes itself.
func (b *Bot) onStart(c tele.Context) error {
	if isPrivate(c) {
		return c.Send(privateStartText)
	}
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onStart.ensure", err)
	}
	players, err := b.st.ListPlayers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onStart.players", err)
	}
	if len(players) == 0 {
		return c.Send(welcomeText, quickMenuKeyboard())
	}
	return b.ephemeral(c, startText(len(players)))
}

// onAddedToGroup greets the chat with onboarding when the bot is added.
func (b *Bot) onAddedToGroup(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onAddedToGroup.ensure", err)
	}
	return c.Send(welcomeText, quickMenuKeyboard())
}

// onText nudges users who type a command incorrectly: an unknown "/cmd" gets a
// "did you mean" hint, "@bot /start" gets the mention hint. Any other text,
// including links and "1/2", is ignored (a group-admin bot sees every message).
func (b *Bot) onText(c tele.Context) error {
	if c.Message() == nil {
		return nil
	}
	botName := ""
	if b.tb.Me != nil {
		botName = b.tb.Me.Username
	}
	switch hint, cmd := classifyText(c.Message().Text, botName); hint {
	case hintUnknown:
		return b.ephemeral(c, unknownCommandText(cmd, suggestCommand(cmd, knownCommands)))
	case hintMentioned:
		return b.ephemeral(c, mentionCmdHint)
	}
	return nil
}

// onQuickStatus / onQuickMe back the quick-menu buttons.
func (b *Bot) onQuickStatus(c tele.Context) error {
	_ = c.Respond()
	return b.onStatus(c)
}

// onQuickPlay opens the same hub as /play.
func (b *Bot) onQuickPlay(c tele.Context) error {
	_ = c.Respond()
	return b.onPlay(c)
}

func (b *Bot) onQuickHelp(c tele.Context) error {
	_ = c.Respond()
	return c.Send(helpText)
}

func (b *Bot) onQuickMe(c tele.Context) error {
	_ = c.Respond()
	return b.onMe(c)
}

func (b *Bot) onJoin(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onJoin.ensure", err)
	}
	sender := realSender(c)
	if sender == nil {
		return b.ephemeral(c, anonMsg)
	}
	name := joinName(sender, c.Message().Payload)

	p, created, err := b.st.RegisterPlayer(c.Chat().ID, sender.ID, name)
	if err != nil {
		return b.fail(c, "onJoin.register", err)
	}
	_ = b.st.SetUsername(p.ID, sender.Username) // best-effort; refreshes/clears
	if created {
		return c.Send(fmt.Sprintf("✅ <b>%s</b> в игре! Удачи 🍀", esc(name)))
	}
	return c.Send(fmt.Sprintf("👌 Имя обновлено: <b>%s</b>", esc(name)))
}

func (b *Bot) onSetNick(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onSetNick.ensure", err)
	}
	sender := realSender(c)
	if sender == nil {
		return b.ephemeral(c, anonMsg)
	}
	nick, err := storage.NormalizeNick(c.Message().Payload)
	switch {
	case errors.Is(err, storage.ErrNickEmpty):
		return b.ephemeral(c, setNickUsage)
	case errors.Is(err, storage.ErrNickSpaces):
		return b.ephemeral(c, setNickSpaces)
	}
	chatID := c.Chat().ID
	player, err := b.st.PlayerByTg(chatID, sender.ID)
	if err != nil {
		return b.fail(c, "onSetNick.player", err)
	}
	created := false
	if player == nil { // one step onboarding: /setnick registers the player too
		player, created, err = b.st.RegisterPlayer(chatID, sender.ID, joinName(sender, ""))
		if err != nil {
			return b.fail(c, "onSetNick.register", err)
		}
		_ = b.st.SetUsername(player.ID, sender.Username) // best-effort
	}
	if err := b.st.SetNick(player.ID, nick); err != nil {
		var taken *storage.NickTakenError
		if errors.As(err, &taken) {
			return b.ephemeral(c, nickTakenText(taken.Nick, taken.Holder))
		}
		return b.fail(c, "onSetNick.set", err)
	}
	return c.Send(setNickDoneText(nick, created))
}

func (b *Bot) onRemovePlayer(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onRemovePlayer.ensure", err)
	}
	if !b.requireAdmin(c) {
		return nil
	}
	name := strings.TrimSpace(c.Message().Payload)
	if name == "" {
		return b.ephemeral(c, removeWhoText)
	}
	n, err := b.st.RemovePlayer(c.Chat().ID, name)
	if err != nil {
		return b.fail(c, "onRemovePlayer.remove", err)
	}
	if n == 0 {
		return b.ephemeral(c, playerNotFoundText(name))
	}
	return c.Send("🗑 Игрок убран: <b>" + esc(name) + "</b> (его прошлые игры сохранены).")
}

func (b *Bot) onPlayers(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onPlayers.ensure", err)
	}
	players, err := b.st.ListPlayers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onPlayers.list", err)
	}
	if len(players) == 0 {
		return b.ephemeral(c, noPlayersText)
	}
	var sb strings.Builder
	sb.WriteString("👥 <b>Игроки</b>\n")
	for _, p := range players {
		line := "• <b>" + esc(p.Name) + "</b>"
		if p.UsdokuNick.Valid && p.UsdokuNick.String != "" {
			line += " — usdoku: <code>" + esc(p.UsdokuNick.String) + "</code>"
		} else {
			line += " — <i>ник не задан</i>"
		}
		sb.WriteString(line + "\n")
	}
	return c.Send(sb.String())
}
