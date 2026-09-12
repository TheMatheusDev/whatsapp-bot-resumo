package cmd

import (
	"fmt"
	"strconv"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

var profetaCountMessages = CountValidationMessages{
	TooFewJoke: "📜 Poucas são as palavras para uma revelação divina...",
	TooFew:     "📜 Apenas 10 mensagens? Buscai mais sabedoria antes de clamar pelo profeta!",
	TooMany:    "📜 Grande demais é este fardo de mensagens! Escolhei um número menor.",
}

// handleSummarizeProfetaCommand handles the !profeta command (shortcut for -r with --profeta flag)
func (h *Handler) handleSummarizeProfetaCommand(args []string, msgTrigger types.MessageInfo) {
	if wait := h.checkSummarizeRateLimit(msgTrigger); wait > 0 {
		h.reactToCommand(msgTrigger, "⏳")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID,
			fmt.Sprintf("⏳ Aguarde *%.0fs* antes de pedir outro resumo.", wait.Seconds()))
		return
	}

	count, style, _, question, hasExplicitCount := utils.ParseSummarizeArgs(args, DefaultSummarizeMessageCount)
	if hasExplicitCount {
		var ok bool
		count, ok = h.parseAndValidateCount(msgTrigger, strconv.Itoa(count), profetaCountMessages)
		if !ok {
			return
		}
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "profeta",
		Question:    question,
	}

	go h.performSummarization(opts, msgTrigger)
}
