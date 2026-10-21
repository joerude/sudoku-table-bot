package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// dbAccessAllowed decides who may download or replace the whole database: the
// configured bot owner anywhere, otherwise only a group admin inside a group. A
// non-owner in a private chat never gets access.
func dbAccessAllowed(ownerID, senderID int64, private, groupAdmin bool) bool {
	if ownerID != 0 && senderID == ownerID {
		return true
	}
	return !private && groupAdmin
}

// requireDBAccess gates /backup and /restore, replying with an ephemeral refusal.
func (b *Bot) requireDBAccess(c tele.Context) bool {
	return b.requireOwnerOrAdmin(c, "⛔ Бэкап и восстановление доступны только владельцу бота или админу группы.")
}

// requireOwnerOrAdmin lets the bot owner through anywhere and a group admin in
// the group; everyone else gets denied as an ephemeral reply.
func (b *Bot) requireOwnerOrAdmin(c tele.Context, denied string) bool {
	u := realSender(c)
	chat := c.Chat()
	if u != nil && chat != nil {
		private := chat.Type == tele.ChatPrivate
		// Ask Telegram about group admin rights only when the owner shortcut misses.
		groupAdmin := false
		if !private && !(b.ownerID != 0 && u.ID == b.ownerID) {
			groupAdmin = b.isGroupAdmin(c)
		}
		if dbAccessAllowed(b.ownerID, u.ID, private, groupAdmin) {
			return true
		}
	}
	_ = b.ephemeral(c, denied)
	return false
}

// onBackup sends a full copy of the database as a document, for migration or
// safekeeping.
func (b *Bot) onBackup(c tele.Context) error {
	if !b.requireDBAccess(c) {
		return nil
	}
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onBackup.ensure", err)
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("sudoku-backup-%d.db", time.Now().UnixNano()))
	if err := b.st.BackupTo(tmp); err != nil {
		return b.fail(c, "onBackup.backup", err)
	}
	defer os.Remove(tmp)

	doc := &tele.Document{
		File:     tele.FromDisk(tmp),
		FileName: fmt.Sprintf("sudoku-backup-%s.db", time.Now().Format("2006-01-02")),
		Caption: "💾 Полный бэкап базы (игроки, ники, сезоны, игры, настройки).\n" +
			"Чтобы восстановить — пришли этот файл боту с подписью <code>/restore</code>.",
	}
	return c.Send(doc)
}

// onDocument handles an uploaded database for /restore: it stages the file and
// exits so the swap happens cleanly at startup (Docker restarts the container).
func (b *Bot) onDocument(c tele.Context) error {
	msg := c.Message()
	if msg == nil || msg.Document == nil {
		return nil
	}
	if strings.TrimSpace(msg.Caption) != "/restore" {
		return nil // an unrelated file — ignore it
	}
	if _, err := b.ensure(c); err != nil {
		return b.fail(c, "onDocument.ensure", err)
	}
	if !b.requireDBAccess(c) {
		return nil
	}

	incoming := b.dbPath + ".incoming"
	if err := b.tb.Download(&msg.Document.File, incoming); err != nil {
		return b.fail(c, "onDocument.download", err)
	}
	if err := storage.ValidateDB(incoming); err != nil {
		_ = os.Remove(incoming)
		return c.Send("⚠️ Это не похоже на базу бота — восстановление отменено.")
	}

	_ = c.Send("✅ База принята. Перезапускаюсь с новой базой… (Docker поднимет за пару секунд)")
	log.Println("restore staged, exiting to swap database on restart")
	go func() {
		time.Sleep(700 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}
