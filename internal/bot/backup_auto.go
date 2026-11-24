package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	tele "gopkg.in/telebot.v3"
)

// stateLastBackup is the bot_state key holding the ISO week of the last
// automatic backup.
const stateLastBackup = "last_backup"

// defaultBackupTZ is the schedule's time zone while no group chat is known
// (the chats table default).
const defaultBackupTZ = "Asia/Bishkek"

// backupWeek is the marker of a week, "2026-W48" (ISO week of the local date).
func backupWeek(now time.Time) string {
	y, w := now.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// weeklyBackupDue reports whether the weekly backup goes out now: not yet for
// this week, and from Monday at the digest's hour on. A bot that was down on
// Monday catches up later in the week, a backup is not worth skipping. now is
// the local time, last the marker of the week already handled ("" = never).
func weeklyBackupDue(now time.Time, last string) bool {
	if now.Weekday() == time.Monday && now.Format("15:04") < weeklyDigestTime {
		return false
	}
	return last != backupWeek(now)
}

// backupTZ is the time zone of the weekly schedule: the first group chat's,
// since the owner has no zone of their own.
func (b *Bot) backupTZ() string {
	if chats, err := b.st.AllChats(); err == nil && len(chats) > 0 && chats[0].TZ != "" {
		return chats[0].TZ
	}
	return defaultBackupTZ
}

// remindBackup sends the database to the owner once a week.
func (b *Bot) remindBackup() { b.remindBackupAt(time.Now()) }

// remindBackupAt is remindBackup with an explicit clock. The week is marked
// before sending, so a failed send is not retried every minute; a marker that
// cannot be written skips the send for the same reason.
func (b *Bot) remindBackupAt(clock time.Time) {
	if b.ownerID == 0 {
		return
	}
	now := clock.In(loadLoc(b.backupTZ()))
	last, err := b.st.State(stateLastBackup)
	if err != nil {
		log.Printf("remindBackup state: %v", err)
		return
	}
	if !weeklyBackupDue(now, last) {
		return
	}
	if err := b.st.SetState(stateLastBackup, backupWeek(now)); err != nil {
		log.Printf("remindBackup mark: %v", err)
		return
	}
	if err := b.sendBackupToOwner(now); err != nil {
		log.Printf("remindBackup send: %v", err)
	}
}

// sendBackupToOwner DMs the same file /backup sends, silently. The temp copy is
// removed whatever happens. A nil bot client (unit tests) sends nothing.
func (b *Bot) sendBackupToOwner(now time.Time) error {
	if b.tb == nil {
		return nil
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("sudoku-autobackup-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmp)
	if err := b.st.BackupTo(tmp); err != nil {
		return err
	}
	doc := &tele.Document{
		File:     tele.FromDisk(tmp),
		FileName: fmt.Sprintf("sudoku-backup-%s.db", now.Format("2006-01-02")),
		Caption:  "💾 Автобэкап · " + fmtRuDate(now, now.Location()),
	}
	_, err := b.tb.Send(tele.ChatID(b.ownerID), doc, tele.Silent)
	return err
}
