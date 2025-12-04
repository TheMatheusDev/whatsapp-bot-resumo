package cmd

import (
	"strconv"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	wstypes "whatsapp-summarizer/src/types"
	"whatsapp-summarizer/src/utils"
)

// handleSummarizeZoomerCommand handles the zoomer command (shortcut for -r with --zoomer flag)
func (h *Handler) handleSummarizeZoomerCommand(args []string, msgTrigger types.MessageInfo, client *whatsmeow.Client) {
	if len(args) == 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ número de mensagens não especificado")
		return
	}

	// Parse message count
	count, err := strconv.Atoi(args[0])
	if err != nil || count <= 0 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ número de mensagens inválido mano")
		return
	}

	// Validate count limits with zoomer personality
	if count <= 3 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ 3 mensagens? intankavel 💀 simplesmente não tankou")
		return
	}

	if count <= 10 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ mlk pediu 10 msgs kkkkkk muito cringe 🤡")
		return
	}

	if count > 9000 {
		h.whatsappService.SendMessage(msgTrigger.Chat, "❌ meteu 9000 msgs literalmente bugou tudo 💀 escolhe um numero menor ai")
		return
	}

	// Parse style options
	style, _, _ := utils.ParseSummarizeOptions(args[1:], false)

	opts := wstypes.SummarizeOptions{
		Count:       count,
		Style:       style,
		Personality: "zoomer",
	}

	// Start summarization in goroutine
	go h.performSummarization(opts, msgTrigger, client)
}
