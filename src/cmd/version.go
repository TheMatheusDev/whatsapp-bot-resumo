package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleVersionCommand(msgTrigger types.MessageInfo) {
	versionText := `
ℹ️ ResumoBOT v2.5.0

📋 O que há de novo:

- Adicionado ranking semanal de mensagens para os usuários mais ativos
- Adicionado top 3 ativos do dia no rodapé do resumo do dia
- Adicionado comando !regras
- Adicionado pool de mensagens aleatórias de entrada/saída
- Adicionado autoria das citações nos resumos
- Pequenos refinamentos nas personalidades
- Corrigido cálculo incorreto de msgs/h no rodapé do resumo do fim do dia

🔗 Código: https://github.com/TheMatheusDev/whatsapp-bot-resumo`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, versionText)
}
