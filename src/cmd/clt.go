package cmd

import (
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

var cltCountMessages = CountValidationMessages{
	TooFewJoke: "❌ Sem tempo para brincadeiras...",
	TooFew:     "❌ 10 mensagens? Sério? Resuma você mesmo!",
	TooMany:    "❌ Tá achando que eu sou seu escravo? Escolha um número menor!",
}

// handleSummarizeCltCommand handles the -clt command (shortcut for -r with --clt flag)
func (h *Handler) handleSummarizeCltCommand(args []string, msgTrigger types.MessageInfo) {
	if len(args) == 0 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Comando incompleto!\n\nUso: !CLT <número de mensagens> [opções optativas]\n\nOpções: --curto, --medio, --longo\n\nExemplos:\n - *!CLT 100*\n- !CLT 30 --longo")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger, args[0], cltCountMessages)
	if !ok {
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "clt",
	}

	go h.performSummarization(opts, msgTrigger)
}
