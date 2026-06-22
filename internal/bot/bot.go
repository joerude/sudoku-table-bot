// Package bot wires Telegram handlers to the storage layer.
package bot

import (
	"log"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// Bot is the running Telegram bot.
type Bot struct {
	tb     *tele.Bot
	st     *storage.Store
	ud     *usdoku.Client
	dbPath string
}

// New constructs a Bot using long-polling.
func New(token string, pollTimeout time.Duration, st *storage.Store, dbPath string) (*Bot, error) {
	tb, err := tele.NewBot(tele.Settings{
		Token:     token,
		Poller:    &tele.LongPoller{Timeout: pollTimeout},
		ParseMode: tele.ModeHTML,
	})
	if err != nil {
		return nil, err
	}
	b := &Bot{tb: tb, st: st, ud: usdoku.New(), dbPath: dbPath}
	tb.Use(logUpdate)
	b.routes()
	return b, nil
}

// Stop stops the poller so main can close the database cleanly.
func (b *Bot) Stop() { b.tb.Stop() }

// logUpdate logs every incoming command and button press in a readable form,
// e.g.  "▶ joerude · /newgame medium"  or  "⚡ joerude · pick(12:5)".
func logUpdate(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		switch {
		case c.Callback() != nil:
			cb := c.Callback()
			data := cb.Data
			if data != "" {
				data = "(" + data + ")"
			}
			log.Printf("⚡ %s · %s%s", senderName(c), cb.Unique, data)
		case c.Message() != nil:
			if text := c.Message().Text; text != "" {
				log.Printf("▶ %s · %s", senderName(c), text)
			}
		}
		return next(c)
	}
}

// senderName returns a short, human label for who triggered an update.
func senderName(c tele.Context) string {
	u := c.Sender()
	if u == nil {
		return "?"
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return "id" + strconv.FormatInt(u.ID, 10)
}

// Start launches the reminder loop, resumes game watchers, and begins polling.
func (b *Bot) Start() {
	go b.runReminders()
	b.resumeWatches()
	if err := b.tb.SetCommands(botCommands()); err != nil {
		log.Printf("set commands: %v", err)
	}
	log.Println("bot started")
	b.tb.Start()
}

// botCommands is the menu shown by Telegram's "/" button.
func botCommands() []tele.Command {
	return []tele.Command{
		{Text: "join", Description: "зарегистрироваться"},
		{Text: "setnick", Description: "ник на usdoku (для авто-учёта)"},
		{Text: "players", Description: "список игроков"},
		{Text: "removeplayer", Description: "убрать игрока"},
		{Text: "newgame", Description: "новая игра + ссылка"},
		{Text: "export", Description: "выгрузить CSV (игры + очки)"},
		{Text: "result", Description: "записать результат вручную"},
		{Text: "duel", Description: "вызвать игрока на дуэль"},
		{Text: "status", Description: "таблица сезона"},
		{Text: "season", Description: "инфо о сезоне"},
		{Text: "me", Description: "моя статистика"},
		{Text: "history", Description: "последние игры"},
		{Text: "speed", Description: "рейтинг по скорости (medium)"},
		{Text: "settings", Description: "настройки (админ)"},
		{Text: "backup", Description: "бэкап базы файлом"},
		{Text: "help", Description: "справка"},
	}
}

func (b *Bot) routes() {
	b.tb.Handle("/start", b.onHelp)
	b.tb.Handle("/help", b.onHelp)
	b.tb.Handle("/join", b.onJoin)
	b.tb.Handle("/setnick", b.onSetNick)
	b.tb.Handle("/players", b.onPlayers)
	b.tb.Handle("/removeplayer", b.onRemovePlayer)
	b.tb.Handle("/export", b.onExport)

	b.tb.Handle("/newgame", b.onNewGame)
	b.tb.Handle("/result", b.onResult)
	b.tb.Handle("/duel", b.onDuel)
	b.tb.Handle(&tele.Btn{Unique: cbDuelPick}, b.onDuelPick)
	b.tb.Handle(&tele.Btn{Unique: cbAccept}, b.onAccept)
	b.tb.Handle(&tele.Btn{Unique: cbDecline}, b.onDecline)

	b.tb.Handle("/status", b.onStatus)
	b.tb.Handle("/table", b.onStatus)
	b.tb.Handle("/season", b.onSeason)
	b.tb.Handle("/me", b.onMe)
	b.tb.Handle("/history", b.onHistory)
	b.tb.Handle("/speed", b.onSpeed)
	b.tb.Handle("/settings", b.onSettings)
	b.tb.Handle("/backup", b.onBackup)

	b.tb.Handle(&tele.Btn{Unique: cbPick}, b.onPick)
	b.tb.Handle(&tele.Btn{Unique: cbUndo}, b.onUndoPick)
	b.tb.Handle(&tele.Btn{Unique: cbCancel}, b.onCancelRecord)
	b.tb.Handle(&tele.Btn{Unique: cbDone}, b.onDone)
	b.tb.Handle(&tele.Btn{Unique: cbReset}, b.onReset)
	b.tb.Handle(&tele.Btn{Unique: cbEdit}, b.onEdit)
	b.tb.Handle(&tele.Btn{Unique: cbDel}, b.onDeleteAsk)
	b.tb.Handle(&tele.Btn{Unique: cbDelY}, b.onDeleteConfirm)
	b.tb.Handle(&tele.Btn{Unique: cbDelN}, b.onDeleteCancel)
	b.tb.Handle(&tele.Btn{Unique: cbUndel}, b.onRestore)
	b.tb.Handle(&tele.Btn{Unique: cbRec}, b.onRecord)

	b.tb.Handle(&tele.Btn{Unique: cbQGame}, b.onQuickGame)
	b.tb.Handle(&tele.Btn{Unique: cbQStatus}, b.onQuickStatus)
	b.tb.Handle(&tele.Btn{Unique: cbQMe}, b.onQuickMe)

	b.tb.Handle(tele.OnAddedToGroup, b.onAddedToGroup)
	b.tb.Handle(tele.OnText, b.onText)
	b.tb.Handle(tele.OnDocument, b.onDocument)
}

// ensure guarantees a chat row + active season exist, returning the season.
func (b *Bot) ensure(c tele.Context) (*storage.Season, error) {
	chatID := c.Chat().ID
	adminID := int64(0)
	if u := realSender(c); u != nil {
		adminID = u.ID
	}
	if err := b.st.EnsureChat(chatID, adminID); err != nil {
		return nil, err
	}
	return b.st.ActiveSeason(chatID)
}

// realSender returns the human who sent the update, or nil for bot/anonymous
// senders. Anonymous group admins arrive as @GroupAnonymousBot (name "Group"),
// which must not be registered as a player.
func realSender(c tele.Context) *tele.User {
	u := c.Sender()
	if u == nil || u.IsBot {
		return nil
	}
	return u
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
