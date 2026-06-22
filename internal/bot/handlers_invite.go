package bot

import (
	tele "gopkg.in/telebot.v3"
)

// onInvite opens a room and pings everyone to join, with an RSVP roster button.
func (b *Bot) onInvite(c tele.Context) error {
	difficulty, mode := parseNewGameArgs(c.Args())
	room, err := b.createGameRoom(c, difficulty, mode)
	if err != nil {
		return b.fail(c, "onInvite", err)
	}
	if room == nil {
		return nil // a guard already replied
	}
	players, err := b.st.ListPlayers(c.Chat().ID)
	if err != nil {
		return b.fail(c, "onInvite.players", err)
	}
	return c.Send(inviteText(difficulty, room.code, players, nil), inviteKeyboard(room.gameID, 0))
}

// onJoinIn toggles the presser's RSVP and refreshes the roster in place.
func (b *Bot) onJoinIn(c tele.Context) error {
	gameID := parseID(c.Data())
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onJoinIn.game", err)
	}
	if game == nil || game.Status != "pending" {
		return c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
	}
	me := realSender(c)
	if me == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не вижу кто ты"})
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, me.ID)
	if err != nil {
		return b.fail(c, "onJoinIn.player", err)
	}
	if player == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Сначала /join"})
	}
	nowIn, err := b.st.ToggleRsvp(gameID, player.ID)
	if err != nil {
		return b.fail(c, "onJoinIn.toggle", err)
	}
	roster, err := b.st.RsvpPlayers(gameID)
	if err != nil {
		return b.fail(c, "onJoinIn.roster", err)
	}
	players, err := b.st.ListPlayers(game.ChatID)
	if err != nil {
		return b.fail(c, "onJoinIn.players", err)
	}
	resp := "Ты в деле! 🔥"
	if !nowIn {
		resp = "Вышел из игры"
	}
	_ = c.Respond(&tele.CallbackResponse{Text: resp})
	return c.Edit(
		inviteText(game.Difficulty.String, game.UsdokuCode.String, players, roster),
		inviteKeyboard(gameID, len(roster)),
	)
}
