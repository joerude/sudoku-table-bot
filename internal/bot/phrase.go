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

// evPhraseGif and evPhraseAudio mark a game whose winner already got the
// /phrase gif or the /phrase audio.
const (
	evPhraseGif   = "phrase_gif"
	evPhraseAudio = "phrase_audio"
)

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

// postWinnerMedia answers a recorded result with the winner's /phrase gif,
// then the /phrase audio, silently. An edit re-records through the same path,
// so the events keep it to one of each per game and winner. Best-effort, a
// failure only logs.
func (b *Bot) postWinnerMedia(game *storage.Game, result *tele.Message) {
	if b.tb == nil || result == nil || result.ID == 0 {
		return
	}
	rows, err := b.st.GameResults(game.ID)
	if err != nil {
		log.Printf("postWinnerMedia.results %d: %v", game.ID, err)
		return
	}
	w, ok := b.phraseWinner(game.ChatID, rows)
	if !ok {
		return
	}
	b.postWinnerOne(game, result, w.PlayerID, evPhraseGif, func() (tele.Sendable, error) {
		id, err := b.st.PhraseGif(w.PlayerID)
		if id == "" || err != nil {
			return nil, err
		}
		return &tele.Animation{File: tele.File{FileID: id}}, nil
	})
	b.postWinnerOne(game, result, w.PlayerID, evPhraseAudio, func() (tele.Sendable, error) {
		ref, err := b.st.PhraseAudio(w.PlayerID)
		if err != nil {
			return nil, err
		}
		return phraseAudioSendable(ref), nil
	})
}

// postWinnerOne sends one piece of the winner's media unless this game
// already got it; load returns nil when the player has none.
func (b *Bot) postWinnerOne(game *storage.Game, result *tele.Message, playerID int64, kind string, load func() (tele.Sendable, error)) {
	if b.phraseMediaShown(game, playerID, kind) {
		return
	}
	what, err := load()
	if err != nil {
		log.Printf("postWinnerMedia.read %s %d: %v", kind, game.ID, err)
		return
	}
	if what == nil {
		return
	}
	if _, err := b.tb.Send(tele.ChatID(game.ChatID), what, quietReply(result)); err != nil {
		log.Printf("postWinnerMedia.send %s %d: %v", kind, game.ID, err)
		return
	}
	b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: playerID, Kind: kind})
}

// phraseMediaShown is true when this winner already got this kind of media
// for this game. A read error counts as shown: a glitch makes the bot
// quieter, not louder.
func (b *Bot) phraseMediaShown(game *storage.Game, playerID int64, kind string) bool {
	events, err := b.st.GameEvents(game.ChatID, game.ID, kind)
	if err != nil {
		log.Printf("phraseMediaShown %s %d: %v", kind, game.ID, err)
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
	if clear, ok := phraseSubArg(arg, phraseGifWord); ok {
		return b.setPhraseGif(c, player.ID, clear)
	}
	if clear, ok := phraseSubArg(arg, phraseAudioWord); ok {
		return b.setPhraseAudio(c, player.ID, clear)
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

// phraseGifWord and phraseAudioWord are the /phrase sub-commands for the
// GIF and the sound.
const (
	phraseGifWord   = "gif"
	phraseAudioWord = "audio"
)

// phraseSubArg tells a /phrase <word> payload ("<word>" or "<word> -") from
// a text phrase; clear is true for "<word> -". Anything longer is a text
// phrase.
func phraseSubArg(arg, word string) (clear, ok bool) {
	f := strings.Fields(arg)
	if len(f) == 0 || !strings.EqualFold(f[0], word) {
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

// isPhraseCaption is true for media sent with the caption "/phrase <word>"
// (or "/phrase@bot <word>").
func isPhraseCaption(caption, word string) bool {
	f := strings.Fields(caption)
	if len(f) != 2 {
		return false
	}
	cmd, _, _ := strings.Cut(f[0], "@")
	return cmd == "/phrase" && strings.EqualFold(f[1], word)
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
	if m == nil || m.Animation == nil || !isPhraseCaption(m.Caption, phraseGifWord) {
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

// Kinds of /phrase audio: an mp3 or other music file, or a voice note. The
// stored value is "<kind>:<file_id>", since Telegram sends each with its own
// method.
const (
	phraseAudioMusic = "audio"
	phraseAudioVoice = "voice"
)

// phraseAudioFile is the "<kind>:<file_id>" of the sound a /phrase audio
// message carries or replies to ("" when neither has one).
func phraseAudioFile(m *tele.Message) string {
	if m == nil {
		return ""
	}
	if ref := audioRef(m); ref != "" {
		return ref
	}
	if m.ReplyTo != nil {
		return audioRef(m.ReplyTo)
	}
	return ""
}

func audioRef(m *tele.Message) string {
	switch {
	case m.Audio != nil:
		return phraseAudioMusic + ":" + m.Audio.FileID
	case m.Voice != nil:
		return phraseAudioVoice + ":" + m.Voice.FileID
	}
	return ""
}

// phraseAudioSendable turns a stored /phrase audio back into what to send
// (nil for "" or a value it does not know).
func phraseAudioSendable(ref string) tele.Sendable {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return nil
	}
	switch kind {
	case phraseAudioMusic:
		return &tele.Audio{File: tele.File{FileID: id}}
	case phraseAudioVoice:
		return &tele.Voice{File: tele.File{FileID: id}}
	}
	return nil
}

// setPhraseAudio handles /phrase audio, like setPhraseGif for the sound.
func (b *Bot) setPhraseAudio(c tele.Context, playerID int64, clear bool) error {
	chatID := c.Chat().ID
	var text string
	switch ref := phraseAudioFile(c.Message()); {
	case clear:
		if err := b.st.SetPhraseAudio(playerID, ""); err != nil {
			return b.fail(c, "phraseAudio.clear", err)
		}
		text = "💬 Звук убран"
	case ref == "":
		current, err := b.st.PhraseAudio(playerID)
		if err != nil {
			return b.fail(c, "phraseAudio.read", err)
		}
		text = phraseAudioUsageText(current != "")
	default:
		if err := b.st.SetPhraseAudio(playerID, ref); err != nil {
			return b.fail(c, "phraseAudio.set", err)
		}
		b.logEvent(storage.Event{ChatID: chatID, PlayerID: playerID, Kind: "phrase_audio_set"})
		text = "💬 Звук сохранён. Выйдет после твоей победы."
		if !b.featureOn(chatID, featPhrase) {
			text += "\n<i>Сейчас выключено в чате: /settings phrase on</i>"
		}
	}
	if b.privately(c, text, nil) {
		return nil
	}
	return b.quiet(c, text)
}

// onPhraseAudio catches an mp3 or a voice note captioned /phrase audio; any
// other sound is ignored.
func (b *Bot) onPhraseAudio(c tele.Context) error {
	m := c.Message()
	if m == nil || audioRef(m) == "" || !isPhraseCaption(m.Caption, phraseAudioWord) {
		return nil
	}
	if isPrivate(c) {
		return b.ephemeral(c, privateDeniedText)
	}
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onPhraseAudio.ensure", err)
	}
	sender := realSender(c)
	if sender == nil {
		return b.ephemeral(c, anonMsg)
	}
	player, err := b.st.PlayerByTg(c.Chat().ID, sender.ID)
	if err != nil {
		return b.fail(c, "onPhraseAudio.player", err)
	}
	if player == nil {
		return b.ephemeral(c, "Сначала зарегистрируйся: /setnick &lt;ник на usdoku&gt;")
	}
	return b.setPhraseAudio(c, player.ID, false)
}

// phraseAudioUsageText explains how to set the sound.
func phraseAudioUsageText(set bool) string {
	text := "💬 Звук выходит после твоей победы, сразу под результатом (после гифки, если она есть).\n"
	if set {
		text += "Сейчас: стоит\n"
	}
	return text + "\nОтветь <code>/phrase audio</code> на mp3 или голосовое, или пришли его с такой подписью.\n/phrase audio " + phraseClear + " · убрать"
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
	b.WriteString("\n/phrase audio · звук после победы (ответом на mp3 или голосовое)")
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
