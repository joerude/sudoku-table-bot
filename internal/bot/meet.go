package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// featMeet keeps an unjoined room alive: after meetOfferAfter alone in the
// lobby the card offers «Буду через 15 мин / через час», and at the agreed
// time the bot pings the creator and whoever promised to come.
const featMeet = "meet"

const (
	evMeetSet  = "meet_set"  // detail = minutes, player_id = who promised
	evMeetPing = "meet_ping" // the reminder went out
)

// meetOfferAfter is how long a room waits for players before the card offers
// a later time.
const meetOfferAfter = 10 * time.Minute

const meetOfferText = "⏳ Соперника пока нет. Не сейчас? Отметь, когда сможешь 👇"

// meetEligible reports whether a game's card may carry the offer: a regular
// game card (the invite has its RSVP roster, a duel its accept and expiry)
// with the mechanic on.
func (b *Bot) meetEligible(g *storage.Game) bool {
	if g.PostKind.String != "" || !b.featureOn(g.ChatID, featMeet) {
		return false
	}
	_, isDuel, err := b.st.DuelTargetID(g.ID)
	return err == nil && !isDuel
}

// meetCardView decides the meet part of a card from the room's state: the
// agreed time while the room is still a lobby, or the offer once it has
// waited meetOfferAfter with fewer than minPlayers inside. Nothing once the
// round has started. createdAt and meetAt are DB times ("" = no meet).
func meetCardView(now time.Time, createdAt, meetAt, status string, joined, minPlayers int,
	byName string, loc *time.Location) (offer bool, line string) {
	if status != "pending" {
		return false, ""
	}
	if meetAt != "" {
		at, err := parseDBTime(meetAt)
		if err != nil {
			return false, ""
		}
		return false, meetAgreedLine(byName, at, loc)
	}
	created, err := parseDBTime(createdAt)
	if err != nil || joined >= minPlayers || now.Sub(created) < meetOfferAfter {
		return false, ""
	}
	return true, meetOfferText
}

// meetAgreedLine is the card line once somebody promised a time.
func meetAgreedLine(name string, at time.Time, loc *time.Location) string {
	if name == "" {
		name = "Соперник" // the promiser's row could not be read
	}
	return fmt.Sprintf("🤝 <b>%s</b> будет в %s", esc(name), at.In(loc).Format("15:04"))
}

// meetTime is now plus minutes, rounded up to the next five minutes (UTC).
func meetTime(now time.Time, minutes int) time.Time {
	at := now.UTC().Add(time.Duration(minutes) * time.Minute)
	r := at.Truncate(5 * time.Minute)
	if r.Before(at) {
		r = r.Add(5 * time.Minute)
	}
	return r
}

// meetButtons is the offer's row on the card.
func meetButtons(gameID int64) []tele.InlineButton {
	m := &tele.ReplyMarkup{}
	return []tele.InlineButton{
		*m.Data("🕒 Буду через 15 мин", cbMeet, fmt.Sprintf("%d:15", gameID)).Inline(),
		*m.Data("🕐 Через час", cbMeet, fmt.Sprintf("%d:60", gameID)).Inline(),
	}
}

// parseMeet reads a meet button payload "<gameID>:<minutes>".
func parseMeet(data string) (gameID int64, minutes int, ok bool) {
	id, mins, found := strings.Cut(data, ":")
	if !found {
		return 0, 0, false
	}
	gameID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	minutes, err = strconv.Atoi(mins)
	if err != nil || (minutes != 15 && minutes != 60) {
		return 0, 0, false
	}
	return gameID, minutes, true
}

// meetPingText is the reminder at the agreed time; who are rendered mentions.
func meetPingText(who []string, at time.Time, loc *time.Location, code string) string {
	text := fmt.Sprintf("⏰ %s: договаривались на %s.", strings.Join(who, ", "), at.In(loc).Format("15:04"))
	if code != "" {
		text += " Комната ждёт: https://www.usdoku.com/" + esc(code)
	}
	return text
}

// meetView computes a watched game's meet state for its card.
func (b *Bot) meetView(game *storage.Game, status string, joined int) (offer bool, line string) {
	if !b.meetEligible(game) {
		return false, ""
	}
	by := ""
	if game.MeetBy.Valid {
		if p, err := b.st.PlayerByID(game.MeetBy.Int64); err == nil && p != nil {
			by = p.Name
		}
	}
	return meetCardView(time.Now(), game.CreatedAt, game.MeetAt.String, status, joined,
		b.minPlayers(game.ChatID), by, loadLoc(b.chatTZ(game.ChatID)))
}

// onMeet records «Буду через…» from the card: the first promise stands, the
// creator is the one already waiting, and the card shows the agreed time.
func (b *Bot) onMeet(c tele.Context) error {
	gameID, minutes, ok := parseMeet(c.Data())
	if !ok {
		return c.Respond(&tele.CallbackResponse{Text: guardFailToast})
	}
	game, err := b.st.GameByID(gameID)
	if err != nil {
		return b.fail(c, "onMeet.game", err)
	}
	if game == nil || game.Status != "pending" || game.Deleted {
		return c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
	}
	sender := realSender(c)
	if sender == nil {
		return c.Respond(&tele.CallbackResponse{Text: "Не получилось определить тебя"})
	}
	player, err := b.st.PlayerByTg(game.ChatID, sender.ID)
	if err != nil {
		return b.fail(c, "onMeet.player", err)
	}
	if player == nil {
		return c.Respond(&tele.CallbackResponse{Text: guardJoinFirst})
	}
	if game.CreatedBy.Valid && game.CreatedBy.Int64 == sender.ID {
		return c.Respond(&tele.CallbackResponse{Text: "Это твоя комната, ждём остальных"})
	}
	loc := loadLoc(b.chatTZ(game.ChatID))
	at := meetTime(time.Now(), minutes)
	set, err := b.st.SetMeet(game.ID, fmtDBTime(at), player.ID)
	if err != nil {
		return b.fail(c, "onMeet.set", err)
	}
	if !set {
		if cur, err := b.st.GameByID(game.ID); err == nil && cur != nil && cur.MeetAt.Valid {
			if t, err := parseDBTime(cur.MeetAt.String); err == nil {
				return c.Respond(&tele.CallbackResponse{Text: "Уже договорились на " + t.In(loc).Format("15:04")})
			}
		}
		return c.Respond(&tele.CallbackResponse{Text: "Игра уже закрыта"})
	}
	b.logEvent(storage.Event{ChatID: game.ChatID, GameID: game.ID, PlayerID: player.ID,
		Kind: evMeetSet, Detail: strconv.Itoa(minutes)})
	_ = c.Respond(&tele.CallbackResponse{Text: "Договорились: в " + at.In(loc).Format("15:04") + " позову обоих"})
	b.showMeet(game.ID, meetAgreedLine(player.Name, at, loc))
	return nil
}

// showMeet redraws the card with the agreed time right away, without waiting
// for the watcher's next poll.
func (b *Bot) showMeet(gameID int64, line string) {
	lp := b.anchor(gameID)
	if lp == nil {
		return
	}
	lp.mu.Lock()
	defer lp.mu.Unlock()
	if lp.base == "" {
		return
	}
	lp.agreeMeet(line)
	b.renderCard(gameID, lp)
}

// agreeMeet shows an agreed time on the card. The caller holds lp.mu.
func (lp *livePost) agreeMeet(line string) {
	lp.meetOffer, lp.meetLine, lp.meetSet = false, line, true
}

// setMeet applies the watcher's meet view. The watcher reads the game before
// it takes lp.mu, so it may still see no promise right after agreeMeet: an
// offer then leaves the agreement alone. The caller holds lp.mu.
func (lp *livePost) setMeet(offer bool, line string) {
	if offer && lp.meetSet {
		return
	}
	lp.meetOffer, lp.meetLine = offer, line
}

// remindMeets pings due meets, once each. A room that has already gathered or
// started is left alone; a mechanic switched off meanwhile stays silent.
func (b *Bot) remindMeets() {
	due, err := b.st.DueMeets(fmtDBTime(time.Now().UTC()))
	if err != nil {
		log.Printf("remindMeets: %v", err)
		return
	}
	for _, m := range due {
		if won, err := b.st.MarkMeetDone(m.GameID); err != nil || !won {
			continue
		}
		if !b.featureOn(m.ChatID, featMeet) || b.roomGathered(m) {
			continue
		}
		b.sendMeetPing(m)
	}
}

// roomGathered asks usdoku whether the round started or enough players are in.
// Unknown (no room, usdoku down) counts as not gathered: the ping still goes.
func (b *Bot) roomGathered(m storage.DueMeet) bool {
	if m.Code == "" || b.ud == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := b.ud.Info(ctx, m.Code)
	if err != nil {
		log.Printf("roomGathered %s: %v", m.Code, err)
		return false
	}
	return info.Info.Status != "pending" || len(info.Players) >= b.minPlayers(m.ChatID)
}

// sendMeetPing mentions the creator and whoever promised, as a reply to the card.
func (b *Bot) sendMeetPing(m storage.DueMeet) {
	if b.tb == nil {
		return
	}
	var who []string
	if m.CreatedBy != 0 {
		if p, err := b.st.PlayerByTg(m.ChatID, m.CreatedBy); err == nil && p != nil {
			who = append(who, mention(*p))
		}
	}
	if p, err := b.st.PlayerByID(m.MeetBy); err == nil && p != nil {
		who = append(who, mention(*p))
	}
	at, err := parseDBTime(m.MeetAt)
	if err != nil || len(who) == 0 {
		return
	}
	// A *SendOptions replaces telebot's defaults, so the parse mode is restated.
	opts := &tele.SendOptions{ParseMode: tele.ModeHTML, AllowWithoutReply: true}
	if m.PostMsgID != 0 {
		opts.ReplyTo = &tele.Message{ID: int(m.PostMsgID)}
	}
	text := meetPingText(who, at, loadLoc(b.chatTZ(m.ChatID)), m.Code)
	if _, err := b.tb.Send(tele.ChatID(m.ChatID), text, opts); err != nil {
		log.Printf("sendMeetPing %d: %v", m.GameID, err)
		return
	}
	b.logEvent(storage.Event{ChatID: m.ChatID, GameID: m.GameID, PlayerID: m.MeetBy, Kind: evMeetPing})
}
