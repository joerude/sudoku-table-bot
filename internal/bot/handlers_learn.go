package bot

import (
	"math/rand"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/joerude/sudoku-bot-telegram/internal/domain"
)

// onLearn opens the technique dictionary. Everything below it is an edit of the
// same message: root → tier → technique → back. An ephemeral /learn opens it as
// an ephemeral message, which editCallbackMessage then edits the ephemeral way.
func (b *Bot) onLearn(c tele.Context) error {
	if b.privately(c, learnRootMsg(), learnRootKeyboard()) {
		return nil
	}
	return c.Send(learnRootMsg(), learnRootKeyboard())
}

// onLearnRoot returns to the tier chooser.
func (b *Bot) onLearnRoot(c tele.Context) error {
	_ = c.Respond()
	return b.editCallbackMessage(c, learnRootMsg(), learnRootKeyboard())
}

// onLearnTier lists the six techniques of one tier. Payload: "<tier>".
func (b *Bot) onLearnTier(c tele.Context) error {
	tier := domain.Tier(c.Data())
	list := domain.TechniquesByTier(tier)
	if len(list) == 0 {
		_ = c.Respond()
		return b.editCallbackMessage(c, learnRootMsg(), learnRootKeyboard())
	}
	_ = c.Respond()
	return b.editCallbackMessage(c, learnTierMsg(tier), learnTierKeyboard(tier))
}

// onLearnTech renders one technique. Payload: "<key>".
func (b *Bot) onLearnTech(c tele.Context) error {
	tech, ok := domain.TechniqueByKey(c.Data())
	if !ok {
		_ = c.Respond()
		return b.editCallbackMessage(c, learnRootMsg(), learnRootKeyboard())
	}
	_ = c.Respond()
	return b.editCallbackMessage(c, learnTechMsg(tech), learnTechKeyboard(tech))
}

// onLearnRandom jumps to a random technique — the "техника дня" without a cron.
func (b *Bot) onLearnRandom(c tele.Context) error {
	tech := domain.RandomTechnique(rand.New(rand.NewSource(time.Now().UnixNano())))
	_ = c.Respond()
	return b.editCallbackMessage(c, learnTechMsg(tech), learnTechKeyboard(tech))
}
