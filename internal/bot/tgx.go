package bot

import (
	"encoding/json"

	tele "gopkg.in/telebot.v3"
)

// Ephemeral messages (Bot API 10.x) are seen only by their receiver. telebot
// has no methods for them, so they go through tb.Raw with hand-built params.

// ephemeralParams is EphemeralMessageParameters.
type ephemeralParams struct {
	ReceiverUserID  int64  `json:"receiver_user_id"`
	CallbackQueryID string `json:"callback_query_id,omitempty"`
}

// ephemeralReply is ReplyParameters pointing at an incoming ephemeral message.
type ephemeralReply struct {
	EphemeralMessageID int `json:"ephemeral_message_id"`
}

// noPreview is LinkPreviewOptions with previews off: service text needs none.
var noPreview = map[string]bool{"is_disabled": true}

// sendEphemeralParams builds the sendMessage params for an ephemeral message to
// receiverID. cbID (a callback just received) or replyEphemeralID (an incoming
// ephemeral message) makes it an answer to that action; with neither, only a
// chat admin bot may send it.
func sendEphemeralParams(chatID, receiverID int64, cbID string, replyEphemeralID int, text string, markup *tele.ReplyMarkup) map[string]any {
	p := map[string]any{
		"chat_id":                      chatID,
		"text":                         text,
		"parse_mode":                   tele.ModeHTML,
		"link_preview_options":         noPreview,
		"ephemeral_message_parameters": ephemeralParams{ReceiverUserID: receiverID, CallbackQueryID: cbID},
	}
	if replyEphemeralID != 0 {
		p["reply_parameters"] = ephemeralReply{EphemeralMessageID: replyEphemeralID}
	}
	if markup != nil {
		p["reply_markup"] = markup
	}
	return p
}

// editEphemeralTextParams builds the editEphemeralMessageText params.
func editEphemeralTextParams(chatID, receiverID int64, ephemeralID int, text string, markup *tele.ReplyMarkup) map[string]any {
	p := map[string]any{
		"chat_id":              chatID,
		"receiver_user_id":     receiverID,
		"ephemeral_message_id": ephemeralID,
		"text":                 text,
		"parse_mode":           tele.ModeHTML,
		"link_preview_options": noPreview,
	}
	if markup != nil {
		p["reply_markup"] = markup
	}
	return p
}

// deleteEphemeralParams builds the deleteEphemeralMessage params.
func deleteEphemeralParams(chatID, receiverID int64, ephemeralID int) map[string]any {
	return map[string]any{
		"chat_id":              chatID,
		"receiver_user_id":     receiverID,
		"ephemeral_message_id": ephemeralID,
	}
}

// sendEphemeral sends an ephemeral message and returns its ephemeral id (0 if
// the answer did not carry one).
func (b *Bot) sendEphemeral(chatID, receiverID int64, cbID string, replyEphemeralID int, text string, markup *tele.ReplyMarkup) (int, error) {
	data, err := b.tb.Raw("sendMessage", sendEphemeralParams(chatID, receiverID, cbID, replyEphemeralID, text, markup))
	if err != nil {
		return 0, err
	}
	var resp struct {
		Result tele.Message `json:"result"`
	}
	if json.Unmarshal(data, &resp) != nil {
		return 0, nil // sent; the id is only informational
	}
	return resp.Result.EphemeralMessageID, nil
}

// editEphemeralText replaces the text (and keyboard) of an ephemeral message.
func (b *Bot) editEphemeralText(chatID, receiverID int64, ephemeralID int, text string, markup *tele.ReplyMarkup) error {
	_, err := b.tb.Raw("editEphemeralMessageText", editEphemeralTextParams(chatID, receiverID, ephemeralID, text, markup))
	return err
}

// deleteEphemeral removes an ephemeral message.
func (b *Bot) deleteEphemeral(chatID, receiverID int64, ephemeralID int) error {
	_, err := b.tb.Raw("deleteEphemeralMessage", deleteEphemeralParams(chatID, receiverID, ephemeralID))
	return err
}
