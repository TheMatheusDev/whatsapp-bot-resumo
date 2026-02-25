package cmd

import (
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

var noirCountMessages = CountValidationMessages{
	TooFewJoke: "❌ Três mensagens. Pistas insuficientes para decifrar este enigma...",
	TooFew:     "❌ Dez mensagens... A cidade esconde mais segredos que isso...",
	TooMany:    "❌ Esse caso é grande demais até pra mim. Traga um número menor...",
}

// handleSummarizeNoirCommand handles the noir/detective command (shortcut for -r with --noir flag)
func (h *Handler) handleSummarizeNoirCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens não especificado")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger.Chat, args[0], noirCountMessages)
	if !ok {
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "noir",
	}

	go h.performSummarization(opts, msgTrigger, client)
}
