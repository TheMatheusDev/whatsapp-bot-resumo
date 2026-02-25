package cmd

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

var fariaLimerCountMessages = CountValidationMessages{
	TooFewJoke: "❌ Hahaha, 3 mensagens? Que bad investment! PRISCILA! Traz meu café!",
	TooFew:     "❌ 10 mensagens? Isso nem dá pra fazer um due diligence! Aumenta esse número aí, vai...",
	TooMany:    "❌ Você acha que eu tenho tempo pra isso? Tenho um meeting em Dubai! Escolha um número menor!",
}

// handleSummarizeFariaLimerCommand handles the farialimer command (shortcut for -r with --farialimer flag)
func (h *Handler) handleSummarizeFariaLimerCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens não especificado")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger.Chat, args[0], fariaLimerCountMessages)
	if !ok {
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "farialimer",
	}

	go h.performSummarization(opts, msgTrigger, client)
}
