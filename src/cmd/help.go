package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleHelpCommand(msgTrigger types.MessageInfo) {
	infoText := `
ℹ️ *ProfetaBOT:*
Resume mensagens via Google Gemini 2.5 Pro/Flash com várias personalidades e estilos!
Prefixos de comando: !, - ou /

*Comandos:*
- !resuma <número> <personalidade> <tamanho do resumo> → Resume mensagens do chat atual (atalho: !r)
- !p <número> <pergunta> → Responde a pergunta com base nas últimas X mensagens
- !d ou !dia → Resume todas as msgs do dia (desde 4h da manhã)	
- !help → Informações do bot
- !version → Versão do bot

*Comandos de Personalidade:*
- !clt <número> → Resumo com personalidade CLT (trabalhador cansado)
- !n <número> → Resumo com narrador esportivo emocionado
- !fl <número> → Resumo com executivo da Faria Lima
- !noir <número> → Resumo com detetive noir dos anos 40
- !z <número> → Resumo com zoomer da Geração Z

*Argumentos de Tamanho de Resumo:*
- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT
- --narrador ou --n → Personalidade narrador esportivo
- --farialimer ou --fl → Personalidade Faria Lima
- --noir ou --detetive → Personalidade detetive noir
- --zoomer ou --z → Personalidade zoomer

*Exemplos:*
- !r 15 → Resumo de 15 mensagens
- !r 5000 --clt → Resumo com personalidade CLT de 5000 mensagens
- !clt 100 → Resumo com personalidade de CLT de 100 mensagens (atalho)
- !n 200 --longo → Resumo com personalidade de narrador esportivo de tamanho longo
- !p 50 Como está o humor do grupo? → Responde a pergunta de acordo com as últimas 50 mensagens
- !p 100 --clt Teve alguma treta? → Responde a pergunta com personalidade CLT
- !p 200 --noir Carlos surtou? → Responde a pergunta com personalidade detetive noir
- !d → Resumo diário
- !d --clt → Resumo diário com personalidade CLT
- !d --farialimer --longo → Resumo longo do dia com personalidade Faria Limmer
`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, infoText)
}
