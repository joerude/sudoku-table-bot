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

// evPhraseGif marks a game whose winner already got the /phrase gif.
const evPhraseGif = "phrase_gif"

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

// phraseWinner is the player whose /phrase goes under a result: the winner,
// when the mechanic is on and someone actually won (rows are rank-ordered,
// DNF last).
func (b *Bot) phraseWinner(chatID int64, rows []storage.ResultRow) (storage.ResultRow, bool) {
	if len(rows) == 0 || rows[0].Rank != 1 || !b.featureOn(chatID, featPhrase) {
		return storage.ResultRow{}, false
	}
	return rows[0], true
}

// winnerPhrase is the phrase to print under a result ("" when none).
func (b *Bot) winnerPhrase(chatID int64, rows []storage.ResultRow) string {
	w, ok := b.phraseWinner(chatID, rows)
	if !ok {
		return ""
	}
	phrase, err := b.st.Phrase(w.PlayerID)
	if err != nil {
		log.Printf("winnerPhrase: %v", err) // the post goes out without it
		return ""
	}
	return phrase
}

// postWinnerGif answers a recorded result with the winner's /phrase gif,
// silently. An edit re-records through the same path, so the evPhraseGif event
// keeps it to one GIF per game and winner. Best-effort, a failure only logs.
func (b *Bot) postWinnerGif(game *storage.Game, result *tele.Message) {
	if b.tb == nil || result == nil || result.ID == 0 {
		return
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("postWinnerGif.results %d: %v", game.ID, err)
		return
	}
	w, ok := b.phraseWinner(game.ChatID, rows)
	if !ok {
		return
	}
	if b.phraseGifShown(game, w.PlayerID) {
		return
	}
	fileID, err := b.st.PhraseGif(w.PlayerID)
	if err != nil || fileID == "" {
		if err != nil {
			log.Printf("postWinnerGif.read %d: %v", game.ID, err)
		}
		return
	}
	gif := &tele.Animation{File: tele.File{FileID: fileID}}
	if _, err := b.tb.Send(tele.ChatID(game.ChatID), gif, quietReply(result)); err != nil {
		log.Printf("postWinnerGif.send %d: %v", game.ID, err)
		return
	}
	b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: w.PlayerID, Kind: evPhraseGif})
}

// phraseGifShown is true when this winner already got the GIF for this game.
// A read error counts as shown: a glitch makes the bot quieter, not louder.
func (b *Bot) phraseGifShown(game *storage.Game, playerID int64) bool {
	events, err := b.st.GameEvents(game.ChatID, game.ID, evPhraseGif)
	if err != nil {
		log.Printf("phraseGifShown %d: %v", game.ID, err)
		return true
	}
	for _, e := range events {
		if e.PlayerID == playerID {
			return true
		}
	}
	return false
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
	if clear, ok := phraseGifArg(arg); ok {
		return b.setPhraseGif(c, player.ID, clear)
	}
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

// phraseGifWord is the /phrase sub-command for the GIF.
const phraseGifWord = "gif"

// phraseGifArg tells a /phrase gif payload ("gif" or "gif -") from a text
// phrase; clear is true for "gif -". Anything longer is a text phrase.
func phraseGifArg(arg string) (clear, ok bool) {
	f := strings.Fields(arg)
	if len(f) == 0 || !strings.EqualFold(f[0], phraseGifWord) {
		return false, false
	}
	switch {
	case len(f) == 1:
		return false, true
	case len(f) == 2 && f[1] == phraseClear:
		return true, true
	}
	return false, false
}

// isPhraseGifCaption is true for a GIF sent with the caption "/phrase gif"
// (or "/phrase@bot gif").
func isPhraseGifCaption(caption string) bool {
	f := strings.Fields(caption)
	if len(f) != 2 {
		return false
	}
	cmd, _, _ := strings.Cut(f[0], "@")
	return cmd == "/phrase" && strings.EqualFold(f[1], phraseGifWord)
}

// setPhraseGif handles /phrase gif: the GIF comes from the message it replies
// to or from the message itself (a GIF captioned /phrase gif). Without one it
// explains how to send it.
func (b *Bot) setPhraseGif(c tele.Context, playerID int64, clear bool) error {
	chatID := c.Chat().ID
	var text string
	switch fileID := phraseGifFile(c.Message()); {
	case clear:
		if err := b.st.SetPhraseGif(playerID, ""); err != nil {
			return b.fail(c, "phraseGif.clear", err)
		}
		text = "💬 Гифка убрана"
	case fileID == "":
		current, err := b.st.PhraseGif(playerID)
		if err != nil {
			return b.fail(c, "phraseGif.read", err)
		}
		text = phraseGifUsageText(current != "")
	default:
		if err := b.st.SetPhraseGif(playerID, fileID); err != nil {
			return b.fail(c, "phraseGif.set", err)
		}
		b.logEvent(storage.Event{ChatID: chatID, PlayerID: playerID, Kind: "phrase_gif_set"})
		text = "💬 Гифка сохранена. Выйдет после твоей победы."
		if !b.featureOn(chatID, featPhrase) {
			text += "\n<i>Сейчас выключено в чате: /settings phrase on</i>"
		}
	}
	if b.privately(c, text, nil) {
		return nil
	}
	return b.quiet(c, text)
}

// phraseGifFile is the file_id of the GIF a /phrase gif message carries or
// replies to ("" when neither is a GIF).
func phraseGifFile(m *tele.Message) string {
	switch {
	case m == nil:
		return ""
	case m.Animation != nil:
		return m.Animation.FileID
	case m.ReplyTo != nil && m.ReplyTo.Animation != nil:
		return m.ReplyTo.Animation.FileID
	}
	return ""
}

// onAnimation catches a GIF captioned /phrase gif; any other GIF is ignored.
func (b *Bot) onAnimation(c tele.Context) error {
	m := c.Message()
	if m == nil || m.Animation == nil || !isPhraseGifCaption(m.Caption) {
		return nil
	}
	if isPrivate(c) {
		return b.ephemeral(c, privateDeniedText)
	}
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onAnimation.ensure", err)
	}
	sender := realSender(c)
	if sender == nil {
		return b.ephemeral(c, anonMsg)
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, sender.ID)
	if err != nil {
		return b.fail(c, "onAnimation.player", err)
	}
	if player == nil {
		return b.ephemeral(c, "Сначала зарегистрируйся: /setnick &lt;ник на usdoku&gt;")
	}
	return b.setPhraseGif(c, player.ID, false)
}

// phraseGifUsageText explains how to set the GIF.
func phraseGifUsageText(set bool) string {
	text := "💬 Гифка выходит после твоей победы, сразу под результатом.\n"
	if set {
		text += "Сейчас: стоит\n"
	}
	return text + "\nОтветь <code>/phrase gif</code> на сообщение с гифкой или пришли гифку с такой подписью.\n/phrase gif " + phraseClear + " · убрать"
}

// phraseUsageText explains /phrase and shows the current phrase, if any.
func phraseUsageText(current string) string {
	var b strings.Builder
	b.WriteString("💬 Фраза выходит жирным под результатом, когда ты побеждаешь.\n")
	if current != "" {
		b.WriteString("Сейчас: <b>«" + esc(current) + "»</b>\n")
	}
	fmt.Fprintf(&b, "\n/phrase &lt;текст&gt; · поставить, до %d символов\n/phrase %s · убрать", phraseMaxRunes, phraseClear)
	b.WriteString("\n/phrase gif · гифка после победы (ответом на гифку)")
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
