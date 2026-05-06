package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

// handleRulesCommand replies with the group rules defined in GROUP_RULES env variable.
func (h *Handler) handleRulesCommand(msgTrigger types.MessageInfo) {
	rules := h.config.Bot.Rules
	if rules == "" {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, "❌ Nenhuma regra foi definida para este grupo.")
		return
	}
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, rules)
}
