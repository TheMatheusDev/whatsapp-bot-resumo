package cmd

import (
	"strconv"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// handleSummarizeNarradorCommand handles the narrador command (shortcut for -r with --narrador flag)
func (h *Handler) handleSummarizeNarradorCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
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

	// Validate count limits with narrator personality
	if count <= 3 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ OLHA O QUE ELE FEZ! PEDIU PRA RESUMIR 3 MENSAGENS! NÃO DÁ PRA ACREDITAR!")
		return
	}

	if count <= 10 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ É TESTE PRA CARDÍACO! 10 mensagens é muito pouco pra uma narração épica!")
		return
	}

	if count > 9000 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ HAJA CORAÇÃO! Esse número é mais de 8 mil! Escolha um número menor!")
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "narrador",
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger, client)
}
