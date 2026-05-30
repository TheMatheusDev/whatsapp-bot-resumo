package cmd

import (
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

var zoomerCountMessages = CountValidationMessages{
	TooFewJoke: "❌ 3 mensagens? intankavel 💀 simplesmente não tankou",
	TooFew:     "❌ mlk pediu 10 msgs kkkkkk muito cringe 🤡",
	TooMany:    "❌ meteu 9000 msgs literalmente bugou tudo 💀 escolhe um numero menor ai",
}

// handleSummarizeZoomerCommand handles the zoomer command (shortcut for -r with --zoomer flag)
func (h *Handler) handleSummarizeZoomerCommand(args []string, msgTrigger types.MessageInfo) {
	if len(args) == 0 {
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ número de mensagens não especificado")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger, args[0], zoomerCountMessages)
	if !ok {
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "zoomer",
	}

	go h.performSummarization(opts, msgTrigger)
}
