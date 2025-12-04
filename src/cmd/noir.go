package cmd

import (
	"strconv"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// handleSummarizeNoirCommand handles the noir/detective command (shortcut for -r with --noir flag)
func (h *Handler) handleSummarizeNoirCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
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

	// Validate count limits with noir personality
	if count <= 3 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Três mensagens. Pistas insuficientes para decifrar este enigma...")
		return
	}

	if count <= 10 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Dez mensagens... A cidade esconde mais segredos que isso...")
		return
	}

	if count > 9000 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ Esse caso é grande demais até pra mim. Traga um número menor...")
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "noir",
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger, client)
}
