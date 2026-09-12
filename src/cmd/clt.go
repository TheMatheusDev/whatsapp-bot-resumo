package cmd

import (
	"fmt"
	"strconv"

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
	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return
	}

	count, style, _, question, hasExplicitCount := utils.ParseSummarizeArgs(args, DefaultSummarizeMessageCount)
	if hasExplicitCount {
		var ok bool
		count, ok = h.parseAndValidateCount(msgTrigger, strconv.Itoa(count), cltCountMessages)
		if !ok {
			return
		}
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "clt",
		Question:    question,
	}

	go h.performSummarization(opts, msgTrigger)
}
