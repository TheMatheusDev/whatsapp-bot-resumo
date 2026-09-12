package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

// handleAskQuestionCommand informs the user that the !p / !pergunta command has been deprecated
// in favor of passing questions directly to summarize commands (!<número> <pergunta> or !r [número] <pergunta>).
func (h *Handler) handleAskQuestionCommand(_ []string, msgTrigger types.MessageInfo) {
	h.reactToCommand(msgTrigger, "❌")
	deprecatedMsg := "❌ O comando *!p* foi descontinuado.\n\n" +
		"Agora você pode fazer perguntas diretamente no comando de resumo:\n" +
		"• *!<número> <pergunta>* (ex: *!50 Quem falou mais?*)\n" +
		"• *!r [número] <pergunta>* (ex: *!r 100 Teve novidades?* ou *!r O que rolou?*)\n\n" +
		"💡 _Dica: Se não especificar o número, o bot lê as últimas 300 mensagens por padrão._"
	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, deprecatedMsg)
}
