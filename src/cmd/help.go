package cmd

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// handleInfoCommand handles the --info/-i command
func (h *Handler) handleHelpCommand(info types.MessageInfo, client *whatsmeow.Client) {
	infoText := `
ℹ️ *ProfetaBOT:*
Resume mensagens via Google Gemini 2.5 Flash

*Comandos:*
- --resuma <número> → Resume mensagens do chat atual
- -r <número> → Forma abreviada
- -clt <número> → Atalho para resumo CLT
- -p <número> <pergunta> → Resume e responde uma pergunta
- --info → Informações do bot
- --version → Versão do bot

*Opções de Resumo:*
- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT (funciona com -r e -p)

*Exemplos:*
- -r 15 → Resumo curto de 15 mensagens
- -r 5000 --clt → Resumo com personalidade CLT de 5000 mensagens
- -p 50 Como está o humor do grupo? → Resume 50 msgs e responde a pergunta
- -p 100 --clt Teve alguma treta? → Resume com CLT e responde pergunta
- -p 200 --longo --clt Carlos surtou? → Resumo longo CLT + resposta
`

	h.sendMessage(client, info.Chat, infoText)
}

// handleVersionCommand handles the --version/-v command
func (h *Handler) handleVersionCommand(info types.MessageInfo, client *whatsmeow.Client) {
	versionText := `
ℹ️ ProfetaBOT v2.1

🔗 Código: https://github.com/TheMatheusDev/whatsapp-bot-resumo`
	h.sendMessage(client, info.Chat, versionText)
}
