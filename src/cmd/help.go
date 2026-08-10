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
- !semana → Ranking de mensagens dos últimos 7 dias (atalho: !s)
- !figurinha → Cria um sticker a partir da foto, vídeo ou GIF que você respondeu (atalho: !sticker)
- !help → Informações do bot
- !regras → Exibe as regras do grupo
- !version → Versão do bot
- !ping → Verifica se o bot está online (inclui uptime)

*Comandos de Personalidade:*
- !clt <número> → Resumo com personalidade CLT (trabalhador cansado)
- !fl <número> → Resumo com executivo da Faria Lima
- !z <número> → Resumo com zoomer da Geração Z
- !profeta <número> → Resumo com linguagem poética/bíblica (atalho: !pft)

*Argumentos de Tamanho e Personalidade:*
- --curto ou -c → Resumo curto
- --medio ou -m → Resumo médio
- --longo ou -l → Resumo longo
- --clt → Personalidade CLT
- --farialimer ou --fl → Personalidade Faria Lima
- --zoomer ou --z → Personalidade Zoomer
- --profeta ou --pft → Personalidade Profeta
- --resumobot ou --rb → Personalidade ResumoBot (padrão quando nenhuma é especificada)

*Exemplos:*
- !r 15 → Resumo de 15 mensagens
- !r 5000 --clt → Resumo com personalidade CLT de 5000 mensagens
- !clt 100 → Resumo com personalidade de CLT de 100 mensagens (atalho)
- !p 50 Como está o humor do grupo? → Responde a pergunta de acordo com as últimas 50 mensagens
- !p 100 --clt Teve alguma treta? → Responde a pergunta com personalidade CLT
- !d → Resumo diário
- !d --farialimer --longo → Resumo longo do dia com personalidade Faria Limmer

⚙️ *Configurações do Grupo (somente admins):*
- !config → Exibe todas as configurações do grupo
- !setregras <texto> → Define as regras do grupo
- !addwelcome <msg> → Adiciona mensagem de boas-vindas ({numero} = menciona quem entrou, {regras} = para inserir regras), separe múltiplas com |
- !delwelcome <n> → Remove boas-vindas pelo índice
- !welcome → Lista as mensagens de boas-vindas configuradas
- !addfarewell <msg> → Adiciona mensagem de despedida ({numero} = menciona quem saiu), separe múltiplas com |
- !delfarewell <n> → Remove despedida pelo índice
- !farewell → Lista as mensagens de despedida configuradas
- !resumodia → Liga/desliga o resumo diário automático deste grupo (toggle)
- !ranking → Liga/desliga o ranking semanal deste grupo (toggle)
- !chatbot [on|off] → Liga/desliga respostas a menções e replies neste grupo (toggle)
- !personalidade <personalidade> → Define a personalidade padrão do grupo (use !personalidades para ver as disponíveis)
- !personalidades → Lista todas as personalidades disponíveis
- !cache → Força atualização do cache do grupo
`

	h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, infoText)
}
