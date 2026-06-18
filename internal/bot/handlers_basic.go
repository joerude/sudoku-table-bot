package bot

import (
	"fmt"
	"strings"

	tele "gopkg.in/telebot.v3"
)

func (b *Bot) onHelp(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onHelp.ensure", err)
	}
	return c.Send(helpText)
}

func (b *Bot) onJoin(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onJoin.ensure", err)
	}
	sender := c.Sender()
	if sender == nil {
		return c.Send("Не удалось определить пользователя.")
	}
	name := strings.TrimSpace(c.Message().Payload)
	if name == "" {
		name = sender.FirstName
	}
	if name == "" {
		name = sender.Username
	}
	if name == "" {
		name = "Игрок"
	}

	_, created, err := b.st.RegisterPlayer(c.Chat().ID, sender.ID, name)
	if err != nil {
		return b.fail(c, "onJoin.register", err)
	}
	if created {
		return c.Send(fmt.Sprintf("✅ <b>%s</b> в игре! Удачи 🍀", esc(name)))
	}
	return c.Send(fmt.Sprintf("👌 Имя обновлено: <b>%s</b>", esc(name)))
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
		return c.Send("Пока никто не зарегистрирован. Жми /join")
	}
	var sb strings.Builder
	sb.WriteString("👥 <b>Игроки</b>\n")
	for _, p := range players {
		sb.WriteString("• " + esc(p.Name) + "\n")
	}
	return c.Send(sb.String())
}
