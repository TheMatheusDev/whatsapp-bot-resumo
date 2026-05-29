package cmd

import (
	"go.mau.fi/whatsmeow/types"
)

func (h *Handler) handleHelpCommand(msgTrigger types.MessageInfo) {
	infoText := `
ℹ️ *ResumoBOT:*
Resume mensagens via Google Gemini com personalidades e estilos!
Prefixos de comando: !, - ou /

*Comandos:*
- !resuma <número> <personalidade> <tamanho do resumo> → Resume mensagens do chat atual (atalho: !r)
- !p <número> <pergunta> → Responde a pergunta com base nas últimas X mensagens
- !d ou !dia → Resume todas as msgs do dia (desde 4h da manhã)	
- !help → Informações do bot
- !regras → Exibe as regras do grupo
- !version → Versão do bot
- !ping → Verifica se o bot está online

*Comandos de Personalidade:*
- !clt <número> → Resumo com personalidade CLT (trabalhador cansado)
- !fl <número> → Resumo com executivo da Faria Lima
- !z <número> → Resumo com zoomer da Geração Z

*Argumentos de Tamanho de Resumo:*
- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT
- --farialimer ou --fl → Personalidade Faria Lima
- --zoomer ou --z → Personalidade zoomer

*Exemplos:*
- !r 15 → Resumo de 15 mensagens
- !r 5000 --clt → Resumo com personalidade CLT de 5000 mensagens
- !clt 100 → Resumo com personalidade de CLT de 100 mensagens (atalho)
- !p 50 Como está o humor do grupo? → Responde a pergunta de acordo com as últimas 50 mensagens
- !p 100 --clt Teve alguma treta? → Responde a pergunta com personalidade CLT
- !d → Resumo diário
- !d --farialimer --longo → Resumo longo do dia com personalidade Faria Limmer

⚙️ *Configurações do Grupo (somente admins):*
- !setregras <texto> → Define as regras do grupo
- !addwelcome <msg> → Adiciona mensagem de boas-vindas ({numero} = menção)
- !delwelcome <n> → Remove boas-vindas pelo índice (sem índice lista as atuais)
- !welcome → Lista as mensagens de boas-vindas configuradas
- !addfarewell <msg> → Adiciona mensagem de despedida ({numero} = menção)
- !delfarewell <n> → Remove despedida pelo índice (sem índice lista as atuais)
- !farewell → Lista as mensagens de despedida configuradas
- !resumo on|off → Liga/desliga o resumo diário automático deste grupo
- !ranking on|off → Liga/desliga o ranking semanal deste grupo
`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.ID, infoText)
}
