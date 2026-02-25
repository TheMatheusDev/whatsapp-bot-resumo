package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleVersionCommand(msgTrigger types.MessageInfo) {
	versionText := `
ℹ️ ResumoBOT v2.4

📋 *O que há de novo:*

🏷️ Bot renomeado para ResumoBOT
🤖 Personalidade CLT como padrão — narrador esportivo removido.
⏰ Resumo diário automático às 00:00.
🔁 IA com fallback duplo — modelos Gemini atualizados.
🐛 Correções e melhorias internas de performance.

🔗 Código: https://github.com/TheMatheusDev/whatsapp-bot-resumo`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, versionText)
}
