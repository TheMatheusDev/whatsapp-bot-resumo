package cmd

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

var narradorCountMessages = CountValidationMessages{
	TooFewJoke: "❌ OLHA O QUE ELE FEZ! PEDIU PRA RESUMIR 3 MENSAGENS! NÃO DÁ PRA ACREDITAR!",
	TooFew:     "❌ É TESTE PRA CARDÍACO! 10 mensagens é muito pouco pra uma narração épica!",
	TooMany:    "❌ HAJA CORAÇÃO! Esse número é mais de 8 mil! Escolha um número menor!",
}

// handleSummarizeNarradorCommand handles the narrador command (shortcut for -r with --narrador flag)
func (h *Handler) handleSummarizeNarradorCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens não especificado")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger.Chat, args[0], narradorCountMessages)
	if !ok {
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "narrador",
	}

	go h.performSummarization(opts, msgTrigger, client)
}
