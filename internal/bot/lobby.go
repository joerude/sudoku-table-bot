package bot

import (
	"log"
	"slices"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featLobby adds who the room still waits for under «👀 В комнате» while the
// round has not started, and says when everyone is in.
const featLobby = "lobby"

// lobbyWaitLine is the line under «👀 В комнате»: who of the expected players
// is not in the room yet, or that everyone is. "" when nobody joined yet (the
// card shows nothing then) or there is no one to expect.
func lobbyWaitLine(joined, expected []string) string {
	if len(joined) == 0 || len(expected) < 2 {
		return ""
	}
	var missing []string
	for _, name := range expected {
		if !slices.Contains(joined, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return "✅ Все в комнате, можно стартовать"
	}
	return "⏳ Ждём: " + escJoin(missing)
}

// lobbyExpected names who the room waits for: the two duelists in a duel, the
// «Я в деле» roster of an invite once anyone pressed it, otherwise every
// active player of the chat.
func (b *Bot) lobbyExpected(game *storage.Game) []string {
	var players []storage.Player
	var err error
	targetID, isDuel, derr := b.st.DuelTargetID(game.ID)
	switch {
	case derr != nil:
		err = derr
	case isDuel:
		players, err = b.duelists(game, targetID)
	default:
		if game.PostKind.String == cardKindInvite {
			players, err = b.st.RsvpPlayers(game.ID)
		}
		if err == nil && len(players) == 0 {
			players, err = b.st.ListPlayers(game.ChatID)
		}
	}
	if err != nil {
		log.Printf("lobbyExpected %d: %v", game.ID, err) // no line beats a wrong one
		return nil
	}
	names := make([]string, len(players))
	for i, p := range players {
		names[i] = p.Name
	}
	return names
}

// duelists are the challenger (the game's creator) and the target.
func (b *Bot) duelists(game *storage.Game, targetID int64) ([]storage.Player, error) {
	var out []storage.Player
	if game.CreatedBy.Valid {
		p, err := b.st.PlayerByTg(game.ChatID, game.CreatedBy.Int64)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	t, err := b.st.PlayerByID(targetID)
	if err != nil {
		return nil, err
	}
	if t != nil {
		out = append(out, *t)
	}
	return out, nil
}
