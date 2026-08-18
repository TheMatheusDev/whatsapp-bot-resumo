package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

// handleRulesCommand replies with the group rules.
// It first looks up rules stored in the DB for this specific group via
// getGroupSettings; if none are found it falls back to GROUP_RULES from the
// global config (i.e. the .env value).
func (h *Handler) handleRulesCommand(msgTrigger types.MessageInfo) {
	rules := ""

	// Per-group rules take priority.
	if settings := h.getGroupSettings(msgTrigger.Chat.User); settings != nil && settings.Rules != "" {
		rules = settings.Rules
	} else {
		// Fallback: global config value (GROUP_RULES env var).
		rules = h.config.Bot.Rules
	}

	if rules == "" {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			"❌ Nenhuma regra foi definida para este grupo.")
		return
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, rules)
}

