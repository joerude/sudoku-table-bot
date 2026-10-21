package bot

import (
	"fmt"
	"log"
	"strings"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/storage"
)

// feature is an optional engagement mechanic a chat can switch off with
// /settings <key> off. Every mechanic starts enabled.
type feature struct {
	Key   string // /settings key and the chat_features row
	Label string // name in the /settings summary
}

// features lists the switchable mechanics in /settings order.
var features = []feature{
	{Key: featHook, Label: "Крючок после игры"},
	{Key: featPace, Label: "Личный темп"},
	{Key: featMeet, Label: "«Буду через…» в пустой комнате"},
	{Key: featNudge, Label: "Умное время напоминания"},
	{Key: featTitles, Label: "Титулы недели в дайджесте"},
}

// featureByKey finds a registered mechanic by its /settings key.
func featureByKey(key string) (feature, bool) {
	for _, f := range features {
		if f.Key == key {
			return f, true
		}
	}
	return feature{}, false
}

// featureOn reports whether a mechanic is on for a chat. A storage error
// counts as off: a hiccup should make the bot quieter, never louder.
func (b *Bot) featureOn(chatID int64, key string) bool {
	on, err := b.st.FeatureEnabled(chatID, key)
	if err != nil {
		log.Printf("featureOn %s: %v", key, err)
		return false
	}
	return on
}

// logEvent records an engagement event; failures are logged, never surfaced.
func (b *Bot) logEvent(e storage.Event) {
	if err := b.st.LogEvent(e); err != nil {
		log.Printf("logEvent %s: %v", e.Kind, err)
	}
}

// parseOnOff reads an on/off switch argument.
func parseOnOff(val string) (on, ok bool) {
	switch strings.ToLower(val) {
	case "on", "вкл", "1":
		return true, true
	case "off", "выкл", "0":
		return false, true
	}
	return false, false
}

// featureSummary renders one "Label: вкл|выкл" line per mechanic for /settings.
func featureSummary(states map[string]bool) string {
	var b strings.Builder
	for _, f := range features {
		state := "выкл"
		if states[f.Key] {
			state = "вкл"
		}
		fmt.Fprintf(&b, "%s: <b>%s</b>\n", f.Label, state)
	}
	return b.String()
}

// featureUsage is the /settings usage line for the switches, "" without any.
func featureUsage() string {
	if len(features) == 0 {
		return ""
	}
	keys := make([]string, len(features))
	for i, f := range features {
		keys[i] = f.Key
	}
	return "\n/settings " + strings.Join(keys, "|") + " on|off: включить или выключить механику"
}

// featureStates reads every mechanic's switch for a chat.
func (b *Bot) featureStates(chatID int64) map[string]bool {
	states := make(map[string]bool, len(features))
	for _, f := range features {
		states[f.Key] = b.featureOn(chatID, f.Key)
	}
	return states
}

// onFeatureSwitch handles /settings <feature> on|off (admin already checked).
func (b *Bot) onFeatureSwitch(c tele.Context, f feature, val string) error {
	on, ok := parseOnOff(val)
	if !ok {
		return b.ephemeral(c, fmt.Sprintf("Включить или выключить: /settings %s on|off", f.Key))
	}
	if err := b.st.SetFeature(c.Chat().ID, f.Key, on); err != nil {
		return b.fail(c, "onFeatureSwitch", err)
	}
	if on {
		return b.quiet(c, fmt.Sprintf("✅ %s: включено", f.Label))
	}
	return b.quiet(c, fmt.Sprintf("🔕 %s: выключено", f.Label))
}
