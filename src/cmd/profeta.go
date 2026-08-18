package cmd

import (
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
	if len(args) == 0 {
		h.reactToCommand(msgTrigger, "❌")
		h.whatsappService.SendMessageReply(msgTrigger.Chat, msgTrigger.Sender, msgTrigger.ID, "❌ Número de mensagens não especificado!\n\nUso: !profeta <número de mensagens> [opções]\n\nOpções: --curto, --medio, --longo")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger, args[0], profetaCountMessages)
	if !ok {
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "profeta",
	}

	go h.performSummarization(opts, msgTrigger)
}
