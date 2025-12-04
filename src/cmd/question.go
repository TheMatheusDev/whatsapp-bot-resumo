package cmd

import (
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// handleAskQuestionCommand handles the --pergunte/-p command
func (h *Handler) handleAskQuestionCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	if len(args) < 2 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Uso: -p <número> [opções] <pergunta>\n\nOpções: --clt, --curto, --medio, --longo\n\nExemplos:\n• -p 50 Quem foi o usuário mais ativo?\n• -p 100 --clt Qual foi o assunto principal?\n• -p 200 --longo --clt Houve conflitos?")
		return
	}

	// Parse message count
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens inválido")
		return
	}

	// Validate count limits
	if count <= 3 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Se acha o engraçadinho, hein?")
		return
	}

	if count <= 10 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Não faz sentido resumir tão poucas mensagens...")
		return
	}

	if count > 9000 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Você só pode ta de brincadeira, né?! Escolha um número menor!")
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

	// Parse options - start with defaults
	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: personality,
		Question:    question,
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger, client)
}
