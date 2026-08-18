package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleHelpCommand(msgTrigger types.MessageInfo) {
	infoText := `🤖 *Como usar o ResumoBOT*

Aqui estão os comandos mais usados:

📄 *1. Resumos*
• *!50* ou *!r 50* ➔ Resume as últimas 50 mensagens
• *!d* ➔ Resume tudo o que rolou hoje

❓ *2. Fazer Perguntas*
• *!p 50 Teve alguma novidade?* ➔ Pergunta sobre as últimas 50 mensagens

🎨 *3. Criar Figurinha*
• *!sticker* ➔ Responda a uma foto, vídeo ou GIF

🎭 *Personalidades de Resumo:*
Experimente pedir um resumo com estilo diferente:
• *!clt 50* (trabalhador cansado)
• *!fl 50* (faria limer)
• *!z 50* (geração Z)
• *!profeta 50* (poético/bíblico)

⚙️ _É admin? Use *!help admin* para ver comandos de configuração._`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, infoText)
}
