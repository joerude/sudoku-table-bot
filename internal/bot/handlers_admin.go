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

// onBackup sends a full copy of the database as a document, for migration or
// safekeeping.
func (b *Bot) onBackup(c tele.Context) error {
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
	if !b.requireAdmin(c, c.Chat().ID) {
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
