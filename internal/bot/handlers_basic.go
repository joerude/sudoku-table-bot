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
	return c.Send(helpText, quickMenuKeyboard())
}

// onAddedToGroup greets the chat with onboarding when the bot is added.
func (b *Bot) onAddedToGroup(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onAddedToGroup.ensure", err)
	}
	return c.Send(welcomeText, quickMenuKeyboard())
}

// onText nudges users who type a command incorrectly (e.g. "@bot /start").
func (b *Bot) onText(c tele.Context) error {
	if c.Message() != nil && strings.Contains(c.Message().Text, "/") {
		return c.Reply("Команды пиши с «/» в начале и без упоминания — например /start или /help.")
	}
	return nil
}

// onQuickStatus / onQuickMe back the quick-menu buttons.
func (b *Bot) onQuickStatus(c tele.Context) error {
	_ = c.Respond()
	return b.onStatus(c)
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
	player, err := b.st.PlayerByTg(c.Chat().ID, sender.ID)
	if err != nil {
		return b.fail(c, "onSetNick.player", err)
	}
	if player == nil {
		return b.ephemeral(c, "Сначала зарегистрируйся: /join")
	}
	nick := strings.TrimSpace(c.Message().Payload)
	if nick == "" {
		return b.ephemeral(c, "Укажи свой ник из usdoku: /setnick МойНик")
	}
	if err := b.st.SetNick(player.ID, nick); err != nil {
		return b.fail(c, "onSetNick.set", err)
	}
	return c.Send(fmt.Sprintf(
		"✅ usdoku-ник: <b>%s</b> сохранён — проверю в ближайшей игре.", esc(nick)))
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
		return b.ephemeral(c, "Кого убрать? Напиши: /removeplayer Имя\n(смотри /players)")
	}
	n, err := b.st.RemovePlayer(c.Chat().ID, name)
	if err != nil {
		return b.fail(c, "onRemovePlayer.remove", err)
	}
	if n == 0 {
		return b.ephemeral(c, "Не нашёл игрока: <b>"+esc(name)+"</b>. Проверь /players.")
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
		return b.ephemeral(c, "Пока никто не зарегистрирован. Жми /join")
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
