package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// The «Игроки» tab of /stats: a profile per player. Its tab id stays "me"
// (the sender's own profile, so old buttons still work); "p:<id>" opens
// another player's profile from the name buttons under it.
const profileTabPrefix = "p:"

// profileNameRunes keeps three name buttons on one phone row.
const profileNameRunes = 12

func profileTabData(playerID int64) string {
	return profileTabPrefix + strconv.FormatInt(playerID, 10)
}

// parseProfileTab reads a "p:<id>" tab id.
func parseProfileTab(tab string) (int64, bool) {
	rest, ok := strings.CutPrefix(tab, profileTabPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// profileView renders the profile tab: tab "me" shows whoever tapped, "p:<id>"
// that player. It returns the id of the player shown (0 when none).
func (b *Bot) profileView(c tele.Context, season *storage.Season, tab string) (string, int64, error) {
	chatID := c.Chat().ID
	if id, ok := parseProfileTab(tab); ok {
		p, err := b.st.PlayerByID(id)
		if err != nil {
			return "", 0, err
		}
		if p != nil && p.ChatID == chatID {
			text, err := b.profileText(chatID, season, p)
			return text, p.ID, err
		}
	}
	sender := realSender(c)
	if sender == nil {
		return "Не удалось определить пользователя.", 0, nil
	}
	p, err := b.st.PlayerByTg(chatID, sender.ID)
	if err != nil {
		return "", 0, err
	}
	if p == nil {
		return "Ты ещё не в игре. Жми /join", 0, nil
	}
	text, err := b.profileText(chatID, season, p)
	return text, p.ID, err
}

// profileText is one player's profile: the season block of /me, the phrase
// under the name, rating, seasons won, the record against each opponent and
// the badge collection.
func (b *Bot) profileText(chatID int64, season *storage.Season, p *storage.Player) (string, error) {
	stat, err := b.st.StatFor(chatID, season.ID, p.ID)
	if err != nil {
		return "", err
	}
	sp, err := b.st.SpeedFor(chatID, season.ID, p.ID, "medium")
	if err != nil {
		return "", err
	}
	duelW, duelL, err := b.st.DuelRecord(chatID, p.ID)
	if err != nil {
		return "", err
	}
	duelSp, duelStreak, err := b.duelExtras(chatID, p.ID)
	if err != nil {
		return "", err
	}
	phrase, err := b.st.Phrase(p.ID)
	if err != nil {
		return "", err
	}
	won, err := b.st.SeasonsWon(chatID, p.ID)
	if err != nil {
		return "", err
	}
	versus, err := b.st.Versus(chatID, p.ID)
	if err != nil {
		return "", err
	}
	games, err := b.st.GamesForRating(chatID)
	if err != nil {
		return "", err
	}
	pr, place := ladderPlace(domain.ComputeRatingsActive(games, b.activePlayerSet(chatID)), p.ID)
	return withPhrase(meText(p.Name, stat, sp, duelW, duelL, duelSp, duelStreak, season), phrase) +
		profileExtraText(pr, place, won, versus) +
		b.meExtra(chatID, p.ID, b.chatTZ(chatID)), nil
}

// ladderPlace finds a player on the rating ladder: their rating and 1-based
// place, or nil when they are not on it (no games, or not active).
func ladderPlace(r domain.Ratings, playerID int64) (*domain.PlayerRating, int) {
	for i, pr := range r.Ladder {
		if pr.PlayerID == playerID {
			return &pr, i + 1
		}
	}
	return nil, 0
}

// withPhrase puts the player's phrase under the profile's first (name) line.
func withPhrase(text, phrase string) string {
	if phrase == "" {
		return text
	}
	line := "💬 «" + esc(phrase) + "»"
	head, rest, ok := strings.Cut(text, "\n")
	if !ok {
		return text + "\n" + line
	}
	return head + "\n" + line + "\n" + rest
}

// profileExtraText is the all-time part of a profile: rating and place,
// seasons won, and the record against each opponent (the profile's owner on
// the left of every score).
func profileExtraText(pr *domain.PlayerRating, place, seasonsWon int, versus []storage.VersusRow) string {
	var b strings.Builder
	if pr != nil {
		fmt.Fprintf(&b, "\n📈 Рейтинг: <b>%d</b> · место %d", pr.Rating, place)
		if pr.Provisional {
			b.WriteString(" <i>(калибровка)</i>")
		}
	}
	if seasonsWon > 0 {
		fmt.Fprintf(&b, "\n👑 Сезонов выиграно: <b>%d</b>", seasonsWon)
	}
	if len(versus) > 0 {
		b.WriteString("\n\n<b>Личный счёт</b>")
		for _, v := range versus {
			b.WriteString("\n" + esc(v.OpponentName))
			if v.GameWins+v.GameLosses > 0 {
				fmt.Fprintf(&b, " · игры %d:%d", v.GameWins, v.GameLosses)
			}
			if v.DuelWins+v.DuelLosses > 0 {
				fmt.Fprintf(&b, " · дуэли %d:%d", v.DuelWins, v.DuelLosses)
			}
		}
	}
	return b.String()
}

// profileRows are the name buttons under the profile tab, three to a row;
// the player on screen is a disabled button.
func profileRows(players []storage.Player, shownID int64) []tele.Row {
	m := &tele.ReplyMarkup{}
	var btns []tele.Btn
	for _, p := range players {
		name := clipRunes(p.Name, profileNameRunes)
		btn := m.Data(name, cbStatsTab, profileTabData(p.ID))
		if p.ID == shownID {
			btn = disabled(m.Data("• "+name, cbStatsTab, profileTabData(p.ID)))
		}
		btns = append(btns, btn)
	}
	var rows []tele.Row
	for i := 0; i < len(btns); i += statsTabsPerRow {
		rows = append(rows, m.Row(btns[i:min(i+statsTabsPerRow, len(btns))]...))
	}
	return rows
}

// withProfileRows appends the name buttons to the stats hub keyboard.
func (b *Bot) withProfileRows(kb *tele.ReplyMarkup, chatID, shownID int64) *tele.ReplyMarkup {
	players, err := b.st.ListPlayers(chatID)
	if err != nil {
		log.Printf("withProfileRows: %v", err) // the hub still works, just without the names
		return kb
	}
	if len(players) < 2 {
		return kb
	}
	names := &tele.ReplyMarkup{}
	names.Inline(profileRows(players, shownID)...)
	kb.InlineKeyboard = append(kb.InlineKeyboard, names.InlineKeyboard...)
	return kb
}
