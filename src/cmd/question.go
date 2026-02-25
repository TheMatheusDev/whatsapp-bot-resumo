package cmd

import (
	"strings"

	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// handleAskQuestionCommand handles the --pergunte/-p command
func (h *Handler) handleAskQuestionCommand(args []string, msgTrigger types.MessageInfo) {
	if len(args) < 2 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Uso: -p <número> [opções] <pergunta>\n\nOpções: --clt, --curto, --medio, --longo\n\nExemplos:\n• -p 50 Quem foi o usuário mais ativo?\n• -p 100 --clt Qual foi o assunto principal?\n• -p 200 --longo --clt Houve conflitos?")
		return
	}

	count, ok := h.parseAndValidateCount(msgTrigger.Chat, args[0], DefaultCountMessages)
	if !ok {
		return
	}

	// Parse options and extract question using utility function
	style, personality, questionParts := utils.ParseSummarizeOptions(args[1:], true)

	// Join the remaining args as the question
	question := strings.Join(questionParts, " ")

	if question == "" {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Você precisa fazer uma pergunta!")
		return
	}

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: personality,
		Question:    question,
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger)
}
