package cmd

import (
	"strconv"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// handleSummarizeFariaLimerCommand handles the farialimer command (shortcut for -r with --farialimer flag)
func (h *Handler) handleSummarizeFariaLimerCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens não especificado")
		return
	}

	// Parse message count
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Número de mensagens inválido")
		return
	}

	// Validate count limits with Faria Lima personality
	if count <= 3 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Hahaha, 3 mensagens? Que bad investment! PRISCILA! Traz meu café!")
		return
	}

	if count <= 10 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ 10 mensagens? Isso nem dá pra fazer um due diligence! Aumenta esse número aí, vai...")
		return
	}

	if count > 9000 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Você acha que eu tenho tempo pra isso? Tenho um meeting em Dubai! Escolha um número menor!")
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "farialimer",
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger, client)
}
