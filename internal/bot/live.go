package bot

import (
	"log"
	"sync"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/usdoku"
)

// livePost is the chat message a watcher live-edits as players finish. base is
// the message's original text; live finish lines are appended below it. Anchors
// live only in memory: after a restart live edits stop, but the final result
// still posts as its own message (autoRecord), so nothing is lost.
type livePost struct {
	msg      tele.Editable
	base     string
	keyboard *tele.ReplyMarkup
}

var liveMu sync.Mutex
var livePosts = map[int64]*livePost{} // gameID -> anchor

// registerLive remembers which message to live-edit for a game.
func registerLive(gameID int64, msg tele.Editable, base string, kb *tele.ReplyMarkup) {
	if msg == nil {
		return
	}
	liveMu.Lock()
	livePosts[gameID] = &livePost{msg: msg, base: base, keyboard: kb}
	liveMu.Unlock()
}

// dropLive forgets a game's anchor (watcher finished or gave up).
func dropLive(gameID int64) {
	liveMu.Lock()
	delete(livePosts, gameID)
	liveMu.Unlock()
}

// liveUpdate appends the current finish order to the game's anchored post. It
// backs off when a manual recording is in progress: the picker has transformed
// the same message, and an edit here would clobber it.
func (b *Bot) liveUpdate(gameID int64, info *usdoku.GameInfo) {
	liveMu.Lock()
	lp := livePosts[gameID]
	liveMu.Unlock()
	if lp == nil {
		return
	}
	if picked, err := b.st.PickedPlayerIDs(gameID); err != nil || len(picked) > 0 {
		return
	}
	lines := b.liveFinishText(gameID, info)
	if lines == "" {
		return
	}
	if _, err := b.tb.Edit(lp.msg, lp.base+"\n\n"+lines, lp.keyboard); err != nil {
		log.Printf("liveUpdate %d: %v", gameID, err)
	}
}

// liveFinishText renders the in-progress finish order: one medal line per
// finisher with the solve time. usdoku nicks are resolved to player names when
// known. Returns "" when nobody has finished yet.
func (b *Bot) liveFinishText(gameID int64, info *usdoku.GameInfo) string {
	game, err := b.st.GameByID(gameID)
	if err != nil || game == nil {
		return ""
	}
	var out string
	for i, p := range info.FinishOrder() {
		name := p.Name
		if pl, _ := b.st.PlayerByNick(game.ChatID, p.Name); pl != nil {
			name = pl.Name
		}
		out += medal(i+1) + " " + esc(name)
		if secs := p.SolveSeconds(info.Info.StartedAt); secs > 0 {
			out += " · ⏱ " + fmtDuration(int(secs))
		}
		out += "\n"
	}
	if out == "" {
		return ""
	}
	return "🏁 <b>Финишировали:</b>\n" + out + "<i>…жду остальных</i>"
}
