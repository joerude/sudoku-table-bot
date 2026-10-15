package bot

import (
	"encoding/json"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v3"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestSendEphemeralParamsCallback(t *testing.T) {
	got := mustJSON(t, sendEphemeralParams(-100, 42, "cb1", 0, "hi", nil))
	for _, want := range []string{
		`"chat_id":-100`, `"text":"hi"`, `"parse_mode":"HTML"`,
		`"ephemeral_message_parameters":{"receiver_user_id":42,"callback_query_id":"cb1"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("params %s lack %s", got, want)
		}
	}
	for _, absent := range []string{"reply_parameters", "reply_markup", "replace_callback_query_message"} {
		if strings.Contains(got, absent) {
			t.Errorf("params %s must not carry %s", got, absent)
		}
	}
}

func TestSendEphemeralParamsReply(t *testing.T) {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("x", "u")))
	got := mustJSON(t, sendEphemeralParams(-100, 42, "", 7, "hi", m))
	for _, want := range []string{
		`"ephemeral_message_parameters":{"receiver_user_id":42}`,
		`"reply_parameters":{"ephemeral_message_id":7}`,
		`"inline_keyboard":[[`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("params %s lack %s", got, want)
		}
	}
}

func TestSendEphemeralParamsAdmin(t *testing.T) {
	got := mustJSON(t, sendEphemeralParams(-100, 42, "", 0, "hi", nil))
	if !strings.Contains(got, `"ephemeral_message_parameters":{"receiver_user_id":42}`) {
		t.Errorf("params %s lack the bare receiver", got)
	}
	if strings.Contains(got, "reply_parameters") {
		t.Errorf("params %s must not reply to anything", got)
	}
}

func TestEditAndDeleteEphemeralParams(t *testing.T) {
	got := mustJSON(t, editEphemeralTextParams(-100, 42, 7, "new", learnRootKeyboard()))
	for _, want := range []string{
		`"chat_id":-100`, `"receiver_user_id":42`, `"ephemeral_message_id":7`,
		`"text":"new"`, `"parse_mode":"HTML"`, `"inline_keyboard"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("edit params %s lack %s", got, want)
		}
	}
	got = mustJSON(t, deleteEphemeralParams(-100, 42, 7))
	if got != `{"chat_id":-100,"ephemeral_message_id":7,"receiver_user_id":42}` {
		t.Errorf("delete params = %s", got)
	}
}

// The Raw path must encode callback data the way telebot's router reads it,
// without touching the caller's markup.
func TestRawMarkupEncodesCallbackData(t *testing.T) {
	m := &tele.ReplyMarkup{}
	m.Inline(m.Row(m.Data("a", "uniq", "7"), m.Data("b", "bare"), m.URL("c", "https://x")))
	raw := rawMarkup(m)
	row := raw.InlineKeyboard[0]
	if row[0].Data != "\funiq|7" || row[1].Data != "\fbare" || row[2].Data != "" {
		t.Errorf("encoded data = %q %q %q", row[0].Data, row[1].Data, row[2].Data)
	}
	if m.InlineKeyboard[0][0].Data != "7" {
		t.Errorf("caller's markup changed: %q", m.InlineKeyboard[0][0].Data)
	}
	if got := mustJSON(t, sendEphemeralParams(1, 2, "", 0, "x", m)); !strings.Contains(got, `"callback_data":"\funiq|7"`) {
		t.Errorf("params %s lack the encoded callback data", got)
	}
}

// An ephemeral command arrives with message_id 0; the fork must keep its id.
func TestMessageUnmarshalsEphemeralFields(t *testing.T) {
	var m tele.Message
	raw := `{"message_id":0,"ephemeral_message_id":55,"receiver_user":{"id":9},"text":"/help","chat":{"id":-100}}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != 0 || m.EphemeralMessageID != 55 || m.ReceiverUser == nil || m.ReceiverUser.ID != 9 {
		t.Errorf("got id=%d eph=%d receiver=%v", m.ID, m.EphemeralMessageID, m.ReceiverUser)
	}
}
