package cmd

import (
	"fmt"
	"strconv"

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
	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return
	}

	count, style, _, question, hasExplicitCount := utils.ParseSummarizeArgs(args, DefaultSummarizeMessageCount)
	if hasExplicitCount {
		var ok bool
		count, ok = h.parseAndValidateCount(msgTrigger, strconv.Itoa(count), zoomerCountMessages)
		if !ok {
			return
		}
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "zoomer",
		Question:    question,
	}

	go h.performSummarization(opts, msgTrigger)
}
