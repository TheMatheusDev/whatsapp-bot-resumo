package cmd

import (
	"strings"

	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleHelpCommand(args []string, msgTrigger types.MessageInfo) {
	if len(args) > 0 && strings.ToLower(args[0]) == "admin" {
		adminHelpText := `⚙️ *Comandos de Administração (Admins)*

*Visão Geral:*
• *!config* ➔ Ver status de todas as configurações

📜 *Regras:*
• *!setregras <texto>* ➔ Define as regras do grupo

👋 *Boas-vindas e Despedidas:*
• *!addwelcome <texto>* ➔ Adiciona mensagem de boas-vindas
• *!delwelcome <id>* ➔ Remove boas-vindas pelo ID
• *!welcome* ➔ Lista mensagens cadastradas
• *!addfarewell <texto>* ➔ Adiciona mensagem de despedida
• *!delfarewell <id>* ➔ Remove despedida pelo ID
• *!farewell* ➔ Lista despedidas cadastradas

🤖 *Automações (Liga/Desliga):*
• *!resumodia* ➔ Alterna resumo diário automático
• *!ranking* ➔ Alterna ranking semanal automático
• *!chatbot* ➔ Alterna respostas por menção/reply

🎭 *Personalidade Padrão:*
• *!personalidade <nome>* ➔ Define a personalidade do grupo
• *!personalidades* ➔ Lista opções disponíveis

📝 *Variáveis de Texto (Boas-vindas / Despedidas):*
• *{numero}* ➔ Menciona o participante (@usuario)
• *{regras}* ➔ Insere as regras cadastradas do grupo
• *|* ➔ Utilizado para inserir múltiplas mensagens de boas-vindas/despedidas de uma só vez. Separe cada mensagem com o caractere |
• *Quebra de linha:* Use quebra de linha real ou \n

*Exemplo:*
!addwelcome Olá {numero}! Seja bem-vindo(a)!\nLeia as regras:\n{regras}`

		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, adminHelpText)
		return
	}

	infoText := `🤖 *Como usar o ResumoBOT*

Aqui estão os comandos mais usados:

📄 *1. Resumos*
• *!50* ou *!r 50* ➔ Resume as últimas 50 mensagens
• *!d* ➔ Resume tudo o que rolou hoje

❓ *2. Fazer Perguntas*
• *!p 50 Teve alguma novidade?* ➔ Pergunta sobre as últimas 50 mensagens

🎨 *3. Criar Figurinha*
• *!sticker* ➔ Responda a uma foto, vídeo ou GIF

🏆 *4. Rankings*
• *!semana* ou *!s* ➔ Ranking dos últimos 7 dias
• *!mes* ou *!m* ➔ Ranking dos últimos 30 dias

🎭 *Personalidades de Resumo:*
Experimente pedir um resumo com estilo diferente:
• *!clt 50* (trabalhador cansado)
• *!fl 50* (faria limer)
• *!z 50* (geração Z)
• *!profeta 50* (poético/bíblico)

⚙️ _É admin? Use *!help admin* para ver os comandos de gerenciamento do grupo._`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, infoText)
}
