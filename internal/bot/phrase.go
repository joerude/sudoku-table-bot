package bot

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featPhrase shows the winner's /phrase in bold under the result.
const featPhrase = "phrase"

// phraseMaxRunes keeps the phrase on one line of a phone screen.
const phraseMaxRunes = 60

// phraseClear is the /phrase argument that removes the phrase.
const phraseClear = "-"

var (
	errPhraseEmpty   = errors.New("phrase: empty")
	errPhraseTooLong = errors.New("phrase: too long")
)

// normalizePhrase squeezes a /phrase argument into one line: whitespace and
// line breaks collapse to single spaces, and quotes around the whole phrase
// go (the post adds its own «»).
func normalizePhrase(raw string) (string, error) {
	s := strings.Join(strings.Fields(raw), " ")
	for _, q := range [][2]string{{"«", "»"}, {`"`, `"`}, {"“", "”"}, {"'", "'"}} {
		if len(s) >= len(q[0])+len(q[1]) && strings.HasPrefix(s, q[0]) && strings.HasSuffix(s, q[1]) {
			s = strings.TrimSpace(s[len(q[0]) : len(s)-len(q[1])])
			break
		}
	}
	switch {
	case s == "":
		return "", errPhraseEmpty
	case utf8.RuneCountInString(s) > phraseMaxRunes:
		return "", errPhraseTooLong
	}
	return s, nil
}

// phraseLine is the bold phrase under a win.
func phraseLine(phrase string) string {
	return "💬 <b>«" + esc(phrase) + "»</b>"
}

// winnerPhrase is the phrase to print under a result: the winner's, when the
// mechanic is on and someone actually won (rows are rank-ordered, DNF last).
func (b *Bot) winnerPhrase(chatID int64, rows []storage.ResultRow) string {
	if len(rows) == 0 || rows[0].Rank != 1 || !b.featureOn(chatID, featPhrase) {
		return ""
	}
	phrase, err := b.st.Phrase(rows[0].PlayerID)
	if err != nil {
		log.Printf("winnerPhrase: %v", err) // the post goes out without it
		return ""
	}
	return phrase
}

// onPhrase sets, shows or clears the sender's phrase: /phrase <text>,
// /phrase, /phrase -.
func (b *Bot) onPhrase(c tele.Context) error {
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onPhrase.ensure", err)
	}
	sender := realSender(c)
	if sender == nil {
		return b.ephemeral(c, anonMsg)
	}
	chatID := c.Chat().ID
	player, err := b.st.PlayerByTg(chatID, sender.ID)
	if err != nil {
		return b.fail(c, "onPhrase.player", err)
	}
	if player == nil {
		return b.ephemeral(c, "Сначала зарегистрируйся: /setnick &lt;ник на usdoku&gt;")
	}
	arg := strings.TrimSpace(c.Message().Payload)
	var text string
	switch arg {
	case "":
		current, err := b.st.Phrase(player.ID)
		if err != nil {
			return b.fail(c, "onPhrase.read", err)
		}
		text = phraseUsageText(current)
	case phraseClear:
		if err := b.st.SetPhrase(player.ID, ""); err != nil {
			return b.fail(c, "onPhrase.clear", err)
		}
		text = "💬 Фраза убрана"
	default:
		phrase, err := normalizePhrase(arg)
		switch {
		case errors.Is(err, errPhraseTooLong):
			return b.ephemeral(c, fmt.Sprintf(
				"Фраза длиннее %d символов. Покороче, чтобы влезла в строку на телефоне.", phraseMaxRunes))
		case err != nil:
			return b.ephemeral(c, phraseUsageText(""))
		}
		if err := b.st.SetPhrase(player.ID, phrase); err != nil {
			return b.fail(c, "onPhrase.set", err)
		}
		b.logEvent(storage.Event{ChatID: chatID, PlayerID: player.ID, Kind: "phrase_set"})
		text = phraseSetText(phrase, b.featureOn(chatID, featPhrase))
	}
	if b.privately(c, text, nil) {
		return nil
	}
	return b.quiet(c, text)
}

// phraseUsageText explains /phrase and shows the current phrase, if any.
func phraseUsageText(current string) string {
	var b strings.Builder
	b.WriteString("💬 Фраза выходит жирным под результатом, когда ты побеждаешь.\n")
	if current != "" {
		b.WriteString("Сейчас: <b>«" + esc(current) + "»</b>\n")
	}
	fmt.Fprintf(&b, "\n/phrase &lt;текст&gt; · поставить, до %d символов\n/phrase %s · убрать", phraseMaxRunes, phraseClear)
	return b.String()
}

// phraseSetText confirms a new phrase; when the mechanic is off it says so,
// since the line promises a post that would not come.
func phraseSetText(phrase string, on bool) string {
	text := "💬 Фраза: <b>«" + esc(phrase) + "»</b>\nВыйдет под результатом твоей победы."
	if !on {
		text += "\n<i>Сейчас выключено в чате: /settings phrase on</i>"
	}
	return text
}
