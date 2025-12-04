package cmd

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleHelpCommand(msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	infoText := `
ℹ️ *ProfetaBOT:*
Resume mensagens via Google Gemini 2.5 Flash

*Comandos:*
- -resuma <número> → Resume mensagens do chat atual
- -r <número> → Forma abreviada
- -p <número> <pergunta> → Resume e responde uma pergunta
- -d ou -dia → Resume todas as msgs do dia (desde 4h da manhã)	
- --info → Informações do bot
- --version → Versão do bot

*Comandos de Personalidade:*
- -clt <número> → Resumo com personalidade CLT (trabalhador cansado)
- -n <número> → Resumo com narrador esportivo emocionado
- -fl <número> → Resumo com executivo da Faria Lima
- -noir <número> → Resumo com detetive noir dos anos 40
- -z <número> → Resumo com zoomer da Geração Z

*Opções de Resumo:*
- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT
- --narrador ou --n → Personalidade narrador esportivo
- --farialimer ou --fl → Personalidade Faria Lima
- --noir ou --detetive → Personalidade detetive noir
- --zoomer ou --z → Personalidade zoomer

*Exemplos:*
- -r 15 → Resumo curto de 15 mensagens
- -r 5000 --clt → Resumo com personalidade CLT de 5000 mensagens
- -clt 100 → Resumo CLT de 100 mensagens (atalho)
- -n 200 --longo → Narrador esportivo com resumo longo
- -fl 50 → Executivo Faria Lima resumindo 50 msgs
- -noir 150 → Detetive noir resumindo 150 msgs
- -z 80 → Zoomer resumindo 80 msgs
- -p 50 Como está o humor do grupo? → Resume 50 msgs e responde a pergunta
- -p 100 --clt Teve alguma treta? → Resume com CLT e responde pergunta
- -p 200 --longo --noir Carlos surtou? → Resumo longo noir + resposta
- -d → Resumo diário
- -d --clt → Resumo diário com personalidade CLT
- -d --longo --farialimer → Resumo longo do dia com Faria Lima
`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, infoText)
}
