package bot

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

const (
	// cardKindInvite marks the /invite card: it keeps the RSVP button.
	cardKindInvite = "invite"
	// cardClosedText replaces the card once the result is recorded; the result
	// itself goes out as a new message, because edits do not notify anyone.
	cardClosedText = "🏁 Записано, результат ниже ↓"
	// cardCancelledText replaces the card of a game that was deleted elsewhere.
	cardCancelledText = "🗑 Игра отменена"
	// creatingText is the instant feedback while the usdoku room is being made.
	creatingText = "⏳ Создаю комнату…"
)

// livePost is the game's card: the one chat message edited through the whole
// game. base is the card's own text; the live section (who joined, who finished)
// is appended below it. The message id and base text are mirrored in the games
// table (post_msg_id, post_text, post_kind), so a restart rebuilds the anchor
// instead of losing it.
type livePost struct {
	mu        sync.Mutex // serialises edits of this card
	msgID     int64
	chatID    int64
	base      string // "" = unknown or retired: no live edits
	kind      string
	section   string // live lines under base
	finishers bool   // somebody finished: offer «Записать сейчас»
	lastSent  string // signature of the last edit, to skip no-op edits
	persisted bool
}

var liveMu sync.Mutex
var livePosts = map[int64]*livePost{} // gameID -> anchor (cache over the DB)

// registerLive remembers which message to live-edit for a game. Kept for flows
// that edit a pressed button's message into the card (the duel accept post);
// the anchor is mirrored into the DB on first use.
func registerLive(gameID int64, msg tele.Editable, base string) {
	if m, ok := msg.(*tele.Message); msg == nil || (ok && m == nil) {
		return
	}
	idStr, chatID := msg.MessageSig()
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return
	}
	liveMu.Lock()
	livePosts[gameID] = &livePost{msgID: id, chatID: chatID, base: base}
	liveMu.Unlock()
}

// dropLive forgets a game's cached anchor (watcher finished or gave up).
func dropLive(gameID int64) {
	liveMu.Lock()
	delete(livePosts, gameID)
	liveMu.Unlock()
}

// saveCard stores the game's card (message id, base text, kind) in the DB and
// the anchor cache.
func (b *Bot) saveCard(gameID, chatID, msgID int64, base, kind string) {
	if err := b.st.SetPostCard(gameID, msgID, base, kind); err != nil {
		log.Printf("saveCard %d: %v", gameID, err)
	}
	liveMu.Lock()
	livePosts[gameID] = &livePost{msgID: msgID, chatID: chatID, base: base, kind: kind, persisted: true}
	liveMu.Unlock()
}

// anchor returns the game's card anchor: the cached one, else rebuilt from the
// DB (after a restart). nil when the game has no card.
func (b *Bot) anchor(gameID int64) *livePost {
	liveMu.Lock()
	lp := livePosts[gameID]
	liveMu.Unlock()
	if lp != nil {
		lp.mu.Lock()
		if !lp.persisted {
			if err := b.st.SetPostCard(gameID, lp.msgID, lp.base, lp.kind); err != nil {
				log.Printf("anchor.persist %d: %v", gameID, err)
			}
			lp.persisted = true
		}
		lp.mu.Unlock()
		return lp
	}
	g, err := b.st.GameByID(gameID)
	if err != nil || g == nil || !g.PostMsgID.Valid {
		return nil
	}
	lp = &livePost{
		msgID: g.PostMsgID.Int64, chatID: g.ChatID,
		base: g.PostText.String, kind: g.PostKind.String, persisted: true,
	}
	liveMu.Lock()
	if cur := livePosts[gameID]; cur != nil {
		lp = cur
	} else {
		livePosts[gameID] = lp
	}
	liveMu.Unlock()
	return lp
}

// editPost edits a stored chat message. A no-op edit ("message is not
// modified") is not an error.
func (b *Bot) editPost(chatID, msgID int64, text string, kb *tele.ReplyMarkup) error {
	m := tele.StoredMessage{MessageID: strconv.FormatInt(msgID, 10), ChatID: chatID}
	var err error
	if kb != nil {
		_, err = b.tb.Edit(m, text, kb)
	} else {
		_, err = b.tb.Edit(m, text) // no markup: the keyboard goes away
	}
	if err != nil && (errors.Is(err, tele.ErrMessageNotModified) ||
		errors.Is(err, tele.ErrSameMessageContent) || strings.Contains(err.Error(), "not modified")) {
		return nil
	}
	return err
}

// cardKeyboard is the card's keyboard for its current live state.
func (lp *livePost) keyboard(gameID int64, rsvp int) *tele.ReplyMarkup {
	return liveKeyboard(gameID, lp.kind, lp.finishers, rsvp)
}

// renderCard edits the card to base + live section. The caller holds lp.mu.
func (b *Bot) renderCard(gameID int64, lp *livePost) {
	if lp.base == "" {
		return
	}
	text := lp.base
	if lp.section != "" {
		text += "\n\n" + lp.section
	}
	rsvp := 0
	if lp.kind == cardKindInvite {
		if roster, err := b.st.RsvpPlayers(gameID); err == nil {
			rsvp = len(roster)
		}
	}
	sig := fmt.Sprintf("%s|%t|%d", text, lp.finishers, rsvp)
	if sig == lp.lastSent {
		return
	}
	if err := b.editPost(lp.chatID, lp.msgID, text, lp.keyboard(gameID, rsvp)); err != nil {
		log.Printf("renderCard %d: %v", gameID, err)
		return
	}
	lp.lastSent = sig
}

// setCardBase swaps the card's base text (e.g. a refreshed RSVP roster), keeping
// the live section, and re-renders. False when the game has no live card.
func (b *Bot) setCardBase(gameID int64, text string) bool {
	lp := b.anchor(gameID)
	if lp == nil {
		return false
	}
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.base = text
	if err := b.st.SetPostText(gameID, text); err != nil {
		log.Printf("setCardBase %d: %v", gameID, err)
	}
	b.renderCard(gameID, lp)
	return true
}

// liveUpdate refreshes the card's live section from a usdoku poll. It backs off
// when a manual recording is in progress: the picker has transformed the same
// message, and an edit here would clobber it.
func (b *Bot) liveUpdate(gameID int64, info *usdoku.GameInfo) {
	lp := b.anchor(gameID)
	if lp == nil {
		return
	}
	if picked, err := b.st.PickedPlayerIDs(gameID); err != nil || len(picked) > 0 {
		return
	}
	game, err := b.st.GameByID(gameID)
	if err != nil || game == nil {
		return
	}
	snap := buildLiveSnapshot(info, func(nick string) string {
		if pl, _ := b.st.PlayerByNick(game.ChatID, nick); pl != nil {
			return pl.Name
		}
		return nick
	})
	lp.mu.Lock()
	defer lp.mu.Unlock()
	if lp.base == "" { // retired, suspended by the picker, or restored without text
		return
	}
	lp.section = liveSectionText(snap)
	lp.finishers = len(snap.Finishers) > 0
	b.renderCard(gameID, lp)
}

// suspendLive stops live edits of the card while the manual picker is shown in
// its message: an edit from the watcher would wipe the picker the user is using.
func (b *Bot) suspendLive(c tele.Context, gameID int64) {
	lp := b.anchor(gameID)
	if lp == nil || c.Message() == nil || int64(c.Message().ID) != lp.msgID {
		return
	}
	lp.mu.Lock()
	lp.base = ""
	lp.mu.Unlock()
}

// retireCardText clears the stored base text so a restart does not bring the
// live edits back for a card that has been turned into a note or closed.
func (b *Bot) retireCardText(gameID int64) {
	if err := b.st.SetPostText(gameID, ""); err != nil {
		log.Printf("retireCardText %d: %v", gameID, err)
	}
}

// closeCard retires the card after the result was recorded: short text, no
// keyboard. The result is posted separately, so the group gets a notification.
func (b *Bot) closeCard(gameID int64) { b.retireCard(gameID, cardClosedText) }

// retireCard turns the card into a one-line text without a keyboard.
func (b *Bot) retireCard(gameID int64, text string) {
	lp := b.anchor(gameID)
	if lp == nil {
		return
	}
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.base = ""
	b.retireCardText(gameID)
	if err := b.editPost(lp.chatID, lp.msgID, text, nil); err != nil {
		log.Printf("retireCard %d: %v", gameID, err)
	}
}

// tryCardNote turns the game's card into text + keyboard (a fallback or a
// nudge). False when there is no card or the edit failed.
func (b *Bot) tryCardNote(gameID int64, text string, kb *tele.ReplyMarkup) bool {
	lp := b.anchor(gameID)
	if lp == nil {
		return false
	}
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.base = "" // the card is not live any more
	b.retireCardText(gameID)
	if err := b.editPost(lp.chatID, lp.msgID, text, kb); err != nil {
		log.Printf("cardNote %d: %v", gameID, err)
		return false
	}
	return true
}

// cardNote is tryCardNote that falls back to a new message.
func (b *Bot) cardNote(gameID, chatID int64, text string, kb *tele.ReplyMarkup) {
	if b.tryCardNote(gameID, text, kb) {
		return
	}
	if _, err := b.tb.Send(tele.ChatID(chatID), text, kb); err != nil {
		log.Printf("cardNote.send %d: %v", gameID, err)
	}
}

// retireOtherCard retires the game's card with text when the user acted in a
// different message (the picker, a conflict post), so no dead keyboard stays.
func (b *Bot) retireOtherCard(c tele.Context, gameID int64, text string) {
	lp := b.anchor(gameID)
	if lp == nil || (c.Message() != nil && int64(c.Message().ID) == lp.msgID) {
		return
	}
	b.retireCard(gameID, text)
}

// registerGameCardRoutes wires the buttons that live on the game card.
func (b *Bot) registerGameCardRoutes() {
	b.tb.Handle(&tele.Btn{Unique: cbRecNow}, b.onRecordNow)
	b.tb.Handle(&tele.Btn{Unique: cbAgain}, b.onAgain)
}

// watching counts the live usdoku watchers per game, so the stale-game nudge can
// tell "nobody is looking" from "the bot is still waiting on usdoku".
var (
	watchMu  sync.Mutex
	watching = map[int64]int{}
)

func markWatching(gameID int64) {
	watchMu.Lock()
	watching[gameID]++
	watchMu.Unlock()
}

func unmarkWatching(gameID int64) {
	watchMu.Lock()
	if watching[gameID] <= 1 {
		delete(watching, gameID)
	} else {
		watching[gameID]--
	}
	watchMu.Unlock()
}

// isWatching reports whether a watcher goroutine is currently polling the game.
func isWatching(gameID int64) bool {
	watchMu.Lock()
	defer watchMu.Unlock()
	return watching[gameID] > 0
}

// busyRooms remembers, per watched duel, whether somebody besides the challenger
// sits in its usdoku room. The expiry ticker reads it instead of polling usdoku.
var (
	busyMu    sync.Mutex
	busyRooms = map[int64]bool{}
)

func setRoomBusy(gameID int64, busy bool) {
	busyMu.Lock()
	defer busyMu.Unlock()
	if busy {
		busyRooms[gameID] = true
	} else {
		delete(busyRooms, gameID)
	}
}

// roomBusy reports whether the last poll saw a non-challenger in the game's room.
func roomBusy(gameID int64) bool {
	busyMu.Lock()
	defer busyMu.Unlock()
	return busyRooms[gameID]
}
