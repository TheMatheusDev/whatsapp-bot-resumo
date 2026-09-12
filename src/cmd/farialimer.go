package cmd

import (
	"fmt"
	"strconv"

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
func (h *Handler) handleSummarizeFariaLimerCommand(args []string, msgTrigger types.MessageInfo) {
	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return
	}

	count, style, _, question, hasExplicitCount := utils.ParseSummarizeArgs(args, DefaultSummarizeMessageCount)
	if hasExplicitCount {
		var ok bool
		count, ok = h.parseAndValidateCount(msgTrigger, strconv.Itoa(count), fariaLimerCountMessages)
		if !ok {
			return
		}
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "farialimer",
		Question:    question,
	}

	go h.performSummarization(opts, msgTrigger)
}
