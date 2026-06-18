// Package bot wires Telegram handlers to the storage layer.
package bot

import (
	"log"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// Bot is the running Telegram bot.
type Bot struct {
	tb *tele.Bot
	st *storage.Store
}

// New constructs a Bot using long-polling.
func New(token string, pollTimeout time.Duration, st *storage.Store) (*Bot, error) {
	tb, err := tele.NewBot(tele.Settings{
		Token:     token,
		Poller:    &tele.LongPoller{Timeout: pollTimeout},
		ParseMode: tele.ModeHTML,
	})
	if err != nil {
		return nil, err
	}
	b := &Bot{tb: tb, st: st}
	b.routes()
	return b, nil
}

// Start launches the reminder loop and begins polling (blocking).
func (b *Bot) Start() {
	go b.runReminders()
	log.Println("bot started")
	b.tb.Start()
}

func (b *Bot) routes() {
	b.tb.Handle("/start", b.onHelp)
	b.tb.Handle("/help", b.onHelp)
	b.tb.Handle("/join", b.onJoin)
	b.tb.Handle("/players", b.onPlayers)

	b.tb.Handle("/newgame", b.onNewGame)
	b.tb.Handle("/result", b.onResult)

	b.tb.Handle("/status", b.onStatus)
	b.tb.Handle("/table", b.onStatus)
	b.tb.Handle("/season", b.onSeason)
	b.tb.Handle("/me", b.onMe)
	b.tb.Handle("/history", b.onHistory)
	b.tb.Handle("/settings", b.onSettings)

	b.tb.Handle(&tele.Btn{Unique: cbPick}, b.onPick)
	b.tb.Handle(&tele.Btn{Unique: cbDone}, b.onDone)
	b.tb.Handle(&tele.Btn{Unique: cbReset}, b.onReset)
	b.tb.Handle(&tele.Btn{Unique: cbEdit}, b.onEdit)
	b.tb.Handle(&tele.Btn{Unique: cbDel}, b.onDelete)
	b.tb.Handle(&tele.Btn{Unique: cbRec}, b.onRecord)
}

// ensure guarantees a chat row + active season exist, returning the season.
func (b *Bot) ensure(c tele.Context) (*storage.Season, error) {
	chatID := c.Chat().ID
	adminID := int64(0)
	if c.Sender() != nil {
		adminID = c.Sender().ID
	}
	if err := b.st.EnsureChat(chatID, adminID); err != nil {
		return nil, err
	}
	return b.st.ActiveSeason(chatID)
}

// fail logs an error and shows a generic message to the user.
func (b *Bot) fail(c tele.Context, where string, err error) error {
	log.Printf("%s: %v", where, err)
	if c.Callback() != nil {
		_ = c.Respond(&tele.CallbackResponse{Text: "Ошибка, попробуйте ещё раз"})
	}
	return c.Send("⚠️ Что-то пошло не так. Попробуйте ещё раз.")
}

// parseID reads an int64 from callback payload (whole string or before ':').
func parseID(s string) int64 {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// parsePair reads "<a>:<b>" from a callback payload.
func parsePair(s string) (int64, int64) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	a, _ := strconv.ParseInt(parts[0], 10, 64)
	bb, _ := strconv.ParseInt(parts[1], 10, 64)
	return a, bb
}
