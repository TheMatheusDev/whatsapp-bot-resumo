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
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens não especificado")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger.Chat, args[0], cltCountMessages)
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
