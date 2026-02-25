package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleVersionCommand(msgTrigger types.MessageInfo) {
	versionText := `
ℹ️ ResumoBOT v2.3

🔗 Código: https://github.com/TheMatheusDev/whatsapp-bot-resumo`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, versionText)
}
