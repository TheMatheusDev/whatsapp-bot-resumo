package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleVersionCommand(msgTrigger types.MessageInfo) {
	versionText := `
ℹ️ ResumoBOT v2.5.0

📋 O que há de novo:

📈 Top 3 Ativos: Adicionado ranking dos usuários mais ativos no rodapé do resumo diário.
⚖️ Comando !regras: Agora você pode consultar as normas do grupo rapidamente.
👋 Pool de Mensagens: Novas saudações e despedidas aleatórias ao entrar ou sair do chat.
🎭 Refinamento de Persona: Ajustes finos para personalidades mais autênticas e consistentes.
💬 Citações Inteligentes: Atribuição de falas garantida, independente da extensão do resumo.
🐛 Correção de Métricas: Ajustado o cálculo de msgs/h que apresentava erro no fechamento do dia.

🔗 Código: https://github.com/TheMatheusDev/whatsapp-bot-resumo`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, versionText)
}
